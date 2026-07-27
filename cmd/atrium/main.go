// Command atrium starts the Atrium core gateway HTTP server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stdout, nil)).Error("server terminated", "error", err)
		os.Exit(1)
	}
	// One logger for both technical and audit records; the handler pseudonymizes
	// emails and filters technical logs by LOG_LEVEL.
	logger := audit.New(os.Stdout, cfg.LogLevel, cfg.Audit.Salt, cfg.Audit.Pseudonymize)

	if err := run(cfg, logger); err != nil {
		logger.Error("server terminated", "error", err)
		os.Exit(1)
	}
}

func run(cfg config.Config, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	oidc, err := auth.NewOIDCAuth(ctx, cfg, logger)
	if err != nil {
		return err
	}

	// Select and build the storage backend by PROVIDER_TYPE. The constructor
	// validates the signing key, so a bad key fails startup; the public key is
	// logged for the trust-setup flow.
	providerSvc, err := newProvider(cfg.Provider, logger)
	if err != nil {
		return err
	}
	streamProxy := proxy.New(providerSvc, cfg.Provider.MaxUploadSize, logger)
	logger.Info("provider client configured", "type", cfg.Provider.Type, "base_url", cfg.Provider.BaseURL, "public_key", providerSvc.PublicKeyPEM())
	// Verify trust without blocking startup: a missing key leaves the core up but
	// degraded, with readiness reflecting provider reachability.
	checkProviderTrust(ctx, providerSvc, logger)

	// The ToS gate is optional; tosMgr stays nil (a pass-through) when disabled.
	// When enabled, the document is loaded here so startup fails fast on a bad path.
	var tosMgr *tos.Manager
	if cfg.TOS.Enabled {
		if tosMgr, err = tos.NewManager(cfg.TOS.Path, cfg.TOS.Version, cfg.SessionKey, cfg.SecureCookies, logger); err != nil {
			return err
		}
		logger.Info("tos consent gate enabled", "version", tosMgr.Version())
	}

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: api.Handler(oidc, tosMgr, providerSvc, streamProxy, cfg.BrandingDir, webui.Brand{
			Name:         cfg.Brand.Name,
			Sub:          cfg.Brand.Sub,
			AccentColor:  cfg.Brand.AccentColor,
			DefaultTheme: cfg.Brand.DefaultTheme,
		}, cfg.SecureCookies, logger),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// newProvider selects the storage backend by PROVIDER_TYPE. A new backend is one
// case here plus one file in internal/provider; the switch is deliberately
// explicit (no init-time registry) so the wiring stays greppable. An unknown type
// fails startup.
func newProvider(cfg config.ProviderConfig, logger *slog.Logger) (provider.Service, error) {
	switch cfg.Type {
	case "nextcloud":
		return provider.NewNextcloud(cfg.BaseURL, cfg.PrivateKeyPEM, cfg.StreamTimeout, logger)
	default:
		return nil, fmt.Errorf("unknown PROVIDER_TYPE %q", cfg.Type)
	}
}

// checkProviderTrust probes the provider's signed healthcheck at startup. It never
// aborts startup: a missing trust relationship is logged as an actionable warning
// rather than treated as fatal.
func checkProviderTrust(ctx context.Context, svc provider.Service, logger *slog.Logger) {
	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := svc.HealthCheck(healthCtx); err != nil {
		logger.Warn("provider trust not established; core running degraded", "error", err)
		return
	}
	logger.Info("provider trust established")
}
