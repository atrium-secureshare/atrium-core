package provider

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestE2EHealthCheck exercises the real trust boundary: the Go client mints an
// ES256 token that the live Nextcloud provider must verify. It is skipped unless
// both env vars are set, so it never runs in normal unit-test or CI passes.
//
// Run against the test instance with:
//
//	PROVIDER_E2E_URL=https://nextcloud.example/apps/atrium_secureshare \
//	PROVIDER_E2E_KEY=/path/to/provider-signing.key \
//	go test ./internal/provider -run TestE2EHealthCheck -v
func TestE2EHealthCheck(t *testing.T) {
	base := os.Getenv("PROVIDER_E2E_URL")
	keyPath := os.Getenv("PROVIDER_E2E_KEY")
	if base == "" || keyPath == "" {
		t.Skip("set PROVIDER_E2E_URL and PROVIDER_E2E_KEY to run the live trust-boundary smoke test")
	}

	pemBytes, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	svc, err := NewNextcloud(base, string(pemBytes), 30*time.Second, discardLogger())
	if err != nil {
		t.Fatalf("NewNextcloud: %v", err)
	}
	if err := svc.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck against %s failed: %v", base, err)
	}
}
