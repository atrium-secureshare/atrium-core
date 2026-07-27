package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/atrium-secureshare/atrium-core/internal/config"
)

// testSigningKeyPEM returns a fresh SEC1-PEM P-256 key so newProvider's backend
// constructor can validate a real signing key.
func testSigningKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
}

// TestNewProviderSelectsBackend covers the PROVIDER_TYPE seam: a known type
// builds a backend, and an unknown type fails startup with a clear error.
func TestNewProviderSelectsBackend(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	base := config.ProviderConfig{
		BaseURL:       "https://provider.example",
		PrivateKeyPEM: testSigningKeyPEM(t),
		StreamTimeout: time.Minute,
	}

	t.Run("nextcloud", func(t *testing.T) {
		cfg := base
		cfg.Type = "nextcloud"
		svc, err := newProvider(cfg, logger)
		if err != nil {
			t.Fatalf("newProvider: %v", err)
		}
		if svc == nil {
			t.Fatal("newProvider returned a nil Service")
		}
	})

	t.Run("unknown type fails startup", func(t *testing.T) {
		cfg := base
		cfg.Type = "bogus"
		if _, err := newProvider(cfg, logger); err == nil {
			t.Error("expected an error for unknown PROVIDER_TYPE")
		}
	})
}
