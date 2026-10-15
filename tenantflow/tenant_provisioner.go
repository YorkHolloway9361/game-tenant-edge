package tenantflow

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var tenantSlugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type GeneratedAsset struct {
	ObjectKey   string `json:"object_key"`
	UploadURL   string `json:"upload_url"`
	ContentType string `json:"content_type"`
}

type LiveEvent struct {
	TenantID string         `json:"tenant_id"`
	Name     string         `json:"name"`
	Payload  map[string]any `json:"payload"`
}

type ModerationItem struct {
	TenantID string `json:"tenant_id"`
	AssetKey string `json:"asset_key"`
	State    string `json:"state"`
}

type TenantRequest struct {
	TenantID    string `json:"tenant_id"`
	AssetName   string `json:"asset_name"`
	ContentType string `json:"content_type"`
	MaxBytes    int64  `json:"max_bytes"`
}

type TenantRuntime struct {
	TenantID       string         `json:"tenant_id"`
	Hostname       string         `json:"hostname"`
	Asset          GeneratedAsset `json:"asset"`
	LiveEventsPath string         `json:"live_events_path"`
	ModerationPath string         `json:"moderation_path"`
}

type InfraProvisioner interface {
	UpsertCNAME(context.Context, string, string, string, string) error
	PresignUpload(context.Context, string, string, string, string, int64) (PresignResult, error)
}

type Provisioner struct {
	Infrai      InfraProvisioner
	ZoneID      string
	RootDomain  string
	CNAMETarget string
	Bucket      string
}

func (p Provisioner) Provision(ctx context.Context, input TenantRequest) (TenantRuntime, error) {
	if !tenantSlugPattern.MatchString(input.TenantID) {
		return TenantRuntime{}, fmt.Errorf("tenant_id must be a DNS-safe slug")
	}
	if strings.TrimSpace(input.AssetName) == "" || strings.Contains(input.AssetName, "/") {
		return TenantRuntime{}, fmt.Errorf("asset_name must be one path segment")
	}
	if input.ContentType == "" || input.MaxBytes <= 0 {
		return TenantRuntime{}, fmt.Errorf("content_type and a positive max_bytes are required")
	}

	hostname := input.TenantID + "." + p.RootDomain
	if err := p.Infrai.UpsertCNAME(ctx, p.ZoneID, input.TenantID, p.CNAMETarget, input.TenantID); err != nil {
		return TenantRuntime{}, fmt.Errorf("provision tenant hostname: %w", err)
	}
	objectKey := input.TenantID + "/generated/" + input.AssetName
	presigned, err := p.Infrai.PresignUpload(ctx, p.Bucket, objectKey, input.ContentType, "asset:"+input.TenantID+":"+input.AssetName, input.MaxBytes)
	if err != nil {
		return TenantRuntime{}, fmt.Errorf("presign generated asset: %w", err)
	}
	return TenantRuntime{
		TenantID:       input.TenantID,
		Hostname:       hostname,
		Asset:          GeneratedAsset{ObjectKey: objectKey, UploadURL: presigned.URL, ContentType: input.ContentType},
		LiveEventsPath: "https://" + hostname + "/events/live",
		ModerationPath: "https://" + hostname + "/moderation/queue",
	}, nil
}
