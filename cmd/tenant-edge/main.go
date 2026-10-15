package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"example.com/game-tenant-edge/tenantflow"
)

func main() {
	apiKey := requiredEnv("INFRAI_API_KEY")
	rootDomain := requiredEnv("ROOT_DOMAIN")
	bucket := envOr("ASSET_BUCKET", "game-generated-assets")
	client := &tenantflow.Client{BaseURL: envOr("INFRAI_BASE_URL", tenantflow.DefaultBaseURL), APIKey: apiKey, MaxRetries: 4}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := client.EnsureBucket(ctx, bucket); err != nil {
		log.Fatalf("prepare asset bucket: %v", err)
	}
	zoneID, err := client.EnsureZone(ctx, rootDomain)
	if err != nil {
		log.Fatalf("prepare DNS zone: %v", err)
	}
	provisioner := tenantflow.Provisioner{
		Infrai: client, ZoneID: zoneID, RootDomain: rootDomain,
		CNAMETarget: requiredEnv("TENANT_CNAME_TARGET"), Bucket: bucket,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /tenants", func(w http.ResponseWriter, r *http.Request) {
		var input tenantflow.TenantRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		result, err := provisioner.Provision(r.Context(), input)
		if err != nil {
			status := http.StatusBadGateway
			var apiErr *tenantflow.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
				status = apiErr.StatusCode
			} else if apiErr == nil {
				status = http.StatusBadRequest
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, result)
	})

	address := envOr("LISTEN_ADDR", ":8080")
	log.Printf("tenant edge listening on %s", address)
	log.Fatal(http.ListenAndServe(address, mux))
}

func requiredEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s is required", name)
	}
	return value
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		fmt.Fprintf(os.Stderr, "encode response: %v\n", err)
	}
}
