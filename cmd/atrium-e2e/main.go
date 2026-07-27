// Command atrium-e2e runs the real api.Handler against in-process mock OIDC and
// stub storage providers, so the Tier-1 recipient flow runs offline. It is a
// test harness, never deployed; the production entrypoint is cmd/atrium.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/atrium-secureshare/atrium-core/internal/api"
	"github.com/atrium-secureshare/atrium-core/internal/audit"
	"github.com/atrium-secureshare/atrium-core/internal/auth"
	"github.com/atrium-secureshare/atrium-core/internal/config"
	"github.com/atrium-secureshare/atrium-core/internal/provider"
	"github.com/atrium-secureshare/atrium-core/internal/proxy"
	"github.com/atrium-secureshare/atrium-core/internal/tos"
	"github.com/atrium-secureshare/atrium-core/internal/webui"
)

const clientID = "atrium-core-e2e"

func main() {
	logger := audit.New(os.Stdout, slog.LevelInfo, nil, false)
	if err := run(logger); err != nil {
		logger.Error("e2e harness terminated", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	addr := getenv("ATRIUM_ADDR", "127.0.0.1:8080")
	base := getenv("E2E_BASE_URL", "http://127.0.0.1:8080")

	// The mocks must be listening before the core runs OIDC discovery, so start
	// them first.
	oidcProvider, err := startMockOIDC(clientID)
	if err != nil {
		return err
	}
	dataset := newStore()
	stub, err := startStubProvider(dataset)
	if err != nil {
		return err
	}

	key, err := genSigningKeyPEM()
	if err != nil {
		return err
	}

	cfg := config.Config{
		Addr: addr,
		OIDC: config.OIDCConfig{
			Issuer:               oidcProvider.issuer,
			ClientID:             clientID,
			ClientSecret:         "e2e-secret",
			RedirectURI:          base + auth.CallbackPath,
			RequireEmailVerified: true,
		},
		SessionKey:    []byte("e2e-session-key-0123456789abcdef"),
		SessionTTL:    12 * time.Hour,
		SecureCookies: false,
		Provider: config.ProviderConfig{
			BaseURL:       stub.url,
			PrivateKeyPEM: key,
			MaxUploadSize: 100 << 20,
			StreamTimeout: 30 * time.Minute,
		},
		Audit:    config.AuditConfig{Pseudonymize: false},
		LogLevel: slog.LevelInfo,
	}
	// The consent gate is enabled whenever a ToS document is provided, so the
	// ToS specs run against the same server.
	if path := os.Getenv("TOS_PATH"); path != "" {
		cfg.TOS = config.TOSConfig{Enabled: true, Path: path, Version: os.Getenv("TOS_VERSION")}
	}

	oidcAuth, err := auth.NewOIDCAuth(context.Background(), cfg, logger)
	if err != nil {
		return err
	}
	providerSvc, err := provider.NewNextcloud(cfg.Provider.BaseURL, cfg.Provider.PrivateKeyPEM, cfg.Provider.StreamTimeout, logger)
	if err != nil {
		return err
	}
	streamProxy := proxy.New(providerSvc, cfg.Provider.MaxUploadSize, logger)

	var tosMgr *tos.Manager
	if cfg.TOS.Enabled {
		if tosMgr, err = tos.NewManager(cfg.TOS.Path, cfg.TOS.Version, cfg.SessionKey, cfg.SecureCookies, logger); err != nil {
			return err
		}
	}

	handler := api.Handler(oidcAuth, tosMgr, providerSvc, streamProxy, "", webui.Brand{}, false, logger)

	// One test-only control route so Playwright can reset the fixture between
	// tests over the same origin (the stub provider's port is unreachable from
	// the browser).
	root := http.NewServeMux()
	root.HandleFunc("POST /_e2e/seed", func(w http.ResponseWriter, _ *http.Request) {
		dataset.seed()
		w.WriteHeader(http.StatusNoContent)
	})
	root.Handle("/", handler)

	logger.Info("e2e harness listening", "addr", addr, "issuer", oidcProvider.issuer, "provider", stub.url, "tos", cfg.TOS.Enabled)
	return http.ListenAndServe(addr, root)
}

// genSigningKeyPEM returns a fresh SEC1-PEM P-256 key; the stub does not verify
// the signature, so the core only needs a usable signing key.
func genSigningKeyPEM() (string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})), nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
