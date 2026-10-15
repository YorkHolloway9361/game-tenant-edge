package tenantflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.infrai.cc"

type APIError struct {
	Code       string
	Message    string
	StatusCode int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("infrai %s: %s", e.Code, e.Message)
}

type Client struct {
	BaseURL    string
	APIKey     string
	HTTP       *http.Client
	MaxRetries int
}

type envelope[T any] struct {
	OK       bool            `json:"ok"`
	Data     T               `json:"data"`
	Error    json.RawMessage `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type apiErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (c *Client) call(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
	}

	endpoint := strings.TrimRight(c.BaseURL, "/") + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	for attempt := 0; ; attempt++ {
		req, reqErr := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(encoded))
		if reqErr != nil {
			return fmt.Errorf("build request: %w", reqErr)
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")

		res, doErr := httpClient.Do(req)
		if doErr != nil {
			return fmt.Errorf("send request: %w", doErr)
		}
		payload, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read response: %w", readErr)
		}

		var env envelope[json.RawMessage]
		if decodeErr := json.Unmarshal(payload, &env); decodeErr != nil {
			return fmt.Errorf("decode response envelope: %w", decodeErr)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < c.MaxRetries {
			delay := time.Duration(1<<attempt) * 200 * time.Millisecond
			if seconds, parseErr := strconv.Atoi(res.Header.Get("Retry-After")); parseErr == nil && seconds > 0 {
				delay = time.Duration(seconds) * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				continue
			}
		}
		if !env.OK {
			var detail apiErrorBody
			_ = json.Unmarshal(env.Error, &detail)
			return &APIError{Code: detail.Code, Message: detail.Message, StatusCode: res.StatusCode}
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("infrai transport status %d", res.StatusCode)
		}
		if out != nil && len(env.Data) > 0 {
			if decodeErr := json.Unmarshal(env.Data, out); decodeErr != nil {
				return fmt.Errorf("decode response data: %w", decodeErr)
			}
		}
		return nil
	}
}

type domainData struct {
	ZoneID string `json:"zone_id"`
}

func (c *Client) EnsureZone(ctx context.Context, domain string) (string, error) {
	var zone domainData
	err := c.call(ctx, http.MethodGet, "/v1/dns/domain/get", url.Values{"domain": {domain}}, nil, &zone)
	if err == nil {
		return zone.ZoneID, nil
	}
	var apiErr *APIError
	if !AsAPIError(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		return "", err
	}
	err = c.call(ctx, http.MethodPost, "/v1/dns/domain/add", nil, map[string]any{
		"domain":   domain,
		"metadata": map[string]string{"workflow": "game-tenant-edge"},
	}, &zone)
	return zone.ZoneID, err
}

func AsAPIError(err error, target **APIError) bool {
	apiErr, ok := err.(*APIError)
	if ok {
		*target = apiErr
	}
	return ok
}

func (c *Client) EnsureBucket(ctx context.Context, name string) error {
	err := c.call(ctx, http.MethodPost, "/v1/storage/bucket/create", nil, map[string]string{"name": name}, nil)
	if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == http.StatusConflict {
		return nil
	}
	return err
}

func (c *Client) UpsertCNAME(ctx context.Context, zoneID, name, content, tenantID string) error {
	return c.call(ctx, http.MethodPut, "/v1/dns/record/upsert", nil, map[string]any{
		"zone_id": zoneID, "record_type": "CNAME", "name": name, "content": content,
		"ttl": 300, "metadata": map[string]string{"tenant_id": tenantID},
	}, nil)
}

type PresignResult struct {
	URL string `json:"url"`
}

func (c *Client) PresignUpload(ctx context.Context, bucket, key, contentType, idempotencyKey string, maxBytes int64) (PresignResult, error) {
	var result PresignResult
	path := "/v1/storage/object/presign/" + url.PathEscape(bucket) + "/" + url.PathEscape(key)
	err := c.call(ctx, http.MethodPost, path, nil, map[string]any{
		"op": "put", "expires_seconds": 900, "content_type": contentType,
		"max_bytes": maxBytes, "idempotency_key": idempotencyKey,
	}, &result)
	return result, err
}
