package tenantflow

import (
	"context"
	"testing"
)

type recordingInfrai struct {
	zoneID, name, content, tenantID          string
	bucket, key, contentType, idempotencyKey string
	maxBytes                                 int64
}

func (f *recordingInfrai) UpsertCNAME(_ context.Context, zoneID, name, content, tenantID string) error {
	f.zoneID, f.name, f.content, f.tenantID = zoneID, name, content, tenantID
	return nil
}

func (f *recordingInfrai) PresignUpload(_ context.Context, bucket, key, contentType, idempotencyKey string, maxBytes int64) (PresignResult, error) {
	f.bucket, f.key, f.contentType, f.idempotencyKey, f.maxBytes = bucket, key, contentType, idempotencyKey, maxBytes
	return PresignResult{URL: "https://uploads.example/signed"}, nil
}

func TestProvisionTenantDecision(t *testing.T) {
	tests := []struct {
		name      string
		input     TenantRequest
		wantHost  string
		wantKey   string
		wantError bool
	}{
		{name: "routes generated asset by tenant", input: TenantRequest{TenantID: "ember-studio", AssetName: "banner.webp", ContentType: "image/webp", MaxBytes: 2 << 20}, wantHost: "ember-studio.games.example.com", wantKey: "ember-studio/generated/banner.webp"},
		{name: "rejects uppercase DNS label", input: TenantRequest{TenantID: "Ember", AssetName: "banner.webp", ContentType: "image/webp", MaxBytes: 2 << 20}, wantError: true},
		{name: "rejects nested asset name", input: TenantRequest{TenantID: "ember", AssetName: "draft/banner.webp", ContentType: "image/webp", MaxBytes: 2 << 20}, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &recordingInfrai{}
			p := Provisioner{Infrai: fake, ZoneID: "zone-42", RootDomain: "games.example.com", CNAMETarget: "edge.example.net", Bucket: "game-assets"}
			got, err := p.Provision(context.Background(), tt.input)
			if tt.wantError {
				if err == nil {
					t.Fatal("expected validation error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Provision() error = %v", err)
			}
			if got.Hostname != tt.wantHost {
				t.Fatalf("hostname = %q, want %q", got.Hostname, tt.wantHost)
			}
			if fake.zoneID != "zone-42" || fake.name != tt.input.TenantID || fake.content != "edge.example.net" {
				t.Fatalf("CNAME request = zone %q name %q content %q", fake.zoneID, fake.name, fake.content)
			}
			if fake.key != tt.wantKey || got.Asset.ObjectKey != tt.wantKey {
				t.Fatalf("asset key = %q, want %q", fake.key, tt.wantKey)
			}
			if fake.idempotencyKey != "asset:"+tt.input.TenantID+":"+tt.input.AssetName {
				t.Fatalf("idempotency key = %q", fake.idempotencyKey)
			}
		})
	}
}
