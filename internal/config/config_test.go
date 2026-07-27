package config

import (
	"log/slog"
	"testing"
	"time"
)

func validEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OIDC_ISSUER", "https://idp.example.com/realms/atrium")
	t.Setenv("OIDC_CLIENT_ID", "atrium-core")
	t.Setenv("OIDC_CLIENT_SECRET", "s3cr3t")
	t.Setenv("OIDC_REDIRECT_URI", "https://atrium.example.com/callback")
	// 32 bytes, base64 standard encoded.
	t.Setenv("SESSION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	t.Setenv("PROVIDER_TYPE", "nextcloud")
	t.Setenv("PROVIDER_BASE_URL", "https://nextcloud.example.com/apps/atrium_secureshare")
	// Load only checks presence; the key is parsed by the provider client.
	t.Setenv("PROVIDER_JWT_PRIVATE_KEY", "-----BEGIN EC PRIVATE KEY-----\nplaceholder\n-----END EC PRIVATE KEY-----")
}

func mustLoad(t *testing.T) Config {
	t.Helper()
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestLoadValid(t *testing.T) {
	validEnv(t)
	cfg := mustLoad(t)
	if !cfg.SecureCookies {
		t.Error("SecureCookies should be true for https redirect")
	}
	if cfg.SessionTTL != 12*time.Hour {
		t.Errorf("SessionTTL = %s, want default 12h", cfg.SessionTTL)
	}
	if len(cfg.SessionKey) < 32 {
		t.Errorf("SessionKey len = %d, want >= 32", len(cfg.SessionKey))
	}
	if cfg.Provider.MaxUploadSize != defaultMaxUploadSize {
		t.Errorf("MaxUploadSize = %d, want default %d", cfg.Provider.MaxUploadSize, defaultMaxUploadSize)
	}
	if cfg.Provider.StreamTimeout != 30*time.Minute {
		t.Errorf("StreamTimeout = %s, want default 30m", cfg.Provider.StreamTimeout)
	}
	if cfg.Provider.Type != "nextcloud" {
		t.Errorf("Provider.Type = %q, want nextcloud", cfg.Provider.Type)
	}
}

func TestLoadProviderTypeOverride(t *testing.T) {
	validEnv(t)
	t.Setenv("PROVIDER_TYPE", "opencloud")
	cfg := mustLoad(t)
	if cfg.Provider.Type != "opencloud" {
		t.Errorf("Provider.Type = %q, want opencloud", cfg.Provider.Type)
	}
}

func TestLoadOIDCAuthChecksDefaults(t *testing.T) {
	validEnv(t)
	cfg := mustLoad(t)
	if !cfg.OIDC.RequireEmailVerified {
		t.Error("RequireEmailVerified should default to true")
	}
	if len(cfg.OIDC.MFAACRValues) != 0 {
		t.Errorf("MFAACRValues = %v, want empty (MFA off) by default", cfg.OIDC.MFAACRValues)
	}
}

func TestLoadOIDCAuthChecksOverrides(t *testing.T) {
	validEnv(t)
	t.Setenv("OIDC_REQUIRE_EMAIL_VERIFIED", "false")
	t.Setenv("OIDC_MFA_ACR_VALUES", " gold , 2 ,,") // trims and drops empties
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OIDC.RequireEmailVerified {
		t.Error("RequireEmailVerified should be false when opted out")
	}
	if got := cfg.OIDC.MFAACRValues; len(got) != 2 || got[0] != "gold" || got[1] != "2" {
		t.Errorf("MFAACRValues = %#v, want [gold 2]", got)
	}
}

// TestLoadRejectsInvalidValues covers the malformed-value paths: each case sets
// one otherwise-valid variable to a value Load must reject. validEnv runs inside
// t.Run so each subtest's t.Setenv isolation is torn down independently.
func TestLoadRejectsInvalidValues(t *testing.T) {
	for _, tc := range []struct{ name, key, val string }{
		{"non-boolean OIDC_REQUIRE_EMAIL_VERIFIED", "OIDC_REQUIRE_EMAIL_VERIFIED", "maybe"},
		{"session key not valid base64", "SESSION_KEY", "not base64!!"},
		{"session key under 32 bytes", "SESSION_KEY", "c2hvcnQ="}, // "short" -> 5 bytes
		{"non-duration SESSION_TTL", "SESSION_TTL", "not-a-duration"},
		{"non-boolean AUDIT_PSEUDONYMIZE", "AUDIT_PSEUDONYMIZE", "nope"},
		{"unknown LOG_LEVEL", "LOG_LEVEL", "verbose"},
		{"non-boolean TOS_ENABLED", "TOS_ENABLED", "yesplease"},
		{"non-numeric MAX_UPLOAD_SIZE", "MAX_UPLOAD_SIZE", "not-a-number"},
		{"zero MAX_UPLOAD_SIZE", "MAX_UPLOAD_SIZE", "0"},
		{"negative MAX_UPLOAD_SIZE", "MAX_UPLOAD_SIZE", "-1"},
		{"non-duration PROVIDER_TIMEOUT", "PROVIDER_TIMEOUT", "nope"},
		{"negative PROVIDER_TIMEOUT", "PROVIDER_TIMEOUT", "-5m"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validEnv(t)
			t.Setenv(tc.key, tc.val)
			if _, err := Load(); err == nil {
				t.Fatalf("expected error for %s=%q", tc.key, tc.val)
			}
		})
	}
}

func TestLoadProviderTransferOverrides(t *testing.T) {
	validEnv(t)
	t.Setenv("MAX_UPLOAD_SIZE", "1048576")
	t.Setenv("PROVIDER_TIMEOUT", "5m")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider.MaxUploadSize != 1<<20 {
		t.Errorf("MaxUploadSize = %d, want 1048576", cfg.Provider.MaxUploadSize)
	}
	if cfg.Provider.StreamTimeout != 5*time.Minute {
		t.Errorf("StreamTimeout = %s, want 5m", cfg.Provider.StreamTimeout)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	for _, missing := range []string{"OIDC_ISSUER", "OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET", "OIDC_REDIRECT_URI", "SESSION_KEY", "PROVIDER_TYPE", "PROVIDER_BASE_URL", "PROVIDER_JWT_PRIVATE_KEY"} {
		t.Run(missing, func(t *testing.T) {
			validEnv(t)
			t.Setenv(missing, "")
			if _, err := Load(); err == nil {
				t.Fatalf("expected error when %s is missing", missing)
			}
		})
	}
}

func TestLoadHTTPRedirectDisablesSecureCookies(t *testing.T) {
	validEnv(t)
	t.Setenv("OIDC_REDIRECT_URI", "http://localhost:8080/callback")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SecureCookies {
		t.Error("SecureCookies should be false for http redirect")
	}
}

func TestLoadCustomTTL(t *testing.T) {
	validEnv(t)
	t.Setenv("SESSION_TTL", "2h30m")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SessionTTL != 2*time.Hour+30*time.Minute {
		t.Errorf("SessionTTL = %s, want 2h30m", cfg.SessionTTL)
	}
}

// TestLoadTOS covers the consent-gate configuration. The two disabled cases hit
// different branches: "disabled by default" is the raw=="" early return, while
// "TOS_PATH ignored when explicitly disabled" is the !enabled return and asserts
// that a set TOS_PATH is ignored when the gate is off.
func TestLoadTOS(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setup   func(t *testing.T)
		wantErr bool
		check   func(t *testing.T, cfg Config)
	}{
		{
			name: "disabled by default",
			check: func(t *testing.T, cfg Config) {
				if cfg.TOS.Enabled {
					t.Error("TOS.Enabled = true, want false by default")
				}
			},
		},
		{
			name: "TOS_PATH ignored when explicitly disabled",
			setup: func(t *testing.T) {
				t.Setenv("TOS_ENABLED", "false")
				t.Setenv("TOS_PATH", "/etc/atrium/tos.md")
			},
			check: func(t *testing.T, cfg Config) {
				if cfg.TOS.Enabled {
					t.Error("TOS.Enabled = true, want false when TOS_ENABLED=false")
				}
			},
		},
		{
			name: "enabled with path and version",
			setup: func(t *testing.T) {
				t.Setenv("TOS_ENABLED", "true")
				t.Setenv("TOS_PATH", "/etc/atrium/tos.md")
				t.Setenv("TOS_VERSION", "2026-01")
			},
			check: func(t *testing.T, cfg Config) {
				if !cfg.TOS.Enabled {
					t.Fatal("TOS.Enabled = false, want true")
				}
				if cfg.TOS.Path != "/etc/atrium/tos.md" {
					t.Errorf("TOS.Path = %q", cfg.TOS.Path)
				}
				if cfg.TOS.Version != "2026-01" {
					t.Errorf("TOS.Version = %q", cfg.TOS.Version)
				}
			},
		},
		{
			name: "enabled without path is an error",
			setup: func(t *testing.T) {
				t.Setenv("TOS_ENABLED", "true")
			},
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validEnv(t)
			if tc.setup != nil {
				tc.setup(t)
			}
			cfg, err := Load()
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
		})
	}
}

func TestLoadAuditDefaults(t *testing.T) {
	validEnv(t)
	cfg := mustLoad(t)
	if !cfg.Audit.Pseudonymize {
		t.Error("Audit.Pseudonymize should default to true")
	}
	if len(cfg.Audit.Salt) != 0 {
		t.Errorf("Audit.Salt = %q, want empty by default", cfg.Audit.Salt)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want default %v", cfg.LogLevel, slog.LevelInfo)
	}
}

func TestLoadAuditOverrides(t *testing.T) {
	validEnv(t)
	t.Setenv("AUDIT_PSEUDONYMIZE", "false")
	t.Setenv("AUDIT_SALT", "pepper")
	t.Setenv("LOG_LEVEL", "debug")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Audit.Pseudonymize {
		t.Error("Audit.Pseudonymize should be false")
	}
	if string(cfg.Audit.Salt) != "pepper" {
		t.Errorf("Audit.Salt = %q, want %q", cfg.Audit.Salt, "pepper")
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, slog.LevelDebug)
	}
}
