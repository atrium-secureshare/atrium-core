// Package config loads the gateway configuration from the environment.
package config

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the runtime configuration for the Atrium core gateway.
type Config struct {
	// Addr is the TCP address the HTTP server listens on (host:port).
	Addr string

	// OIDC configures the OpenID Connect provider.
	OIDC OIDCConfig

	// SessionKey signs stateless session cookies (HMAC-SHA256); at least 32 bytes.
	SessionKey []byte

	// SessionTTL is the absolute lifetime of a session cookie.
	SessionTTL time.Duration

	// SecureCookies sets the Secure cookie attribute; derived from the redirect
	// URI scheme (https) so plain http still works.
	SecureCookies bool

	// TOS holds the optional consent configuration; when disabled, consent is
	// delegated to the identity provider.
	TOS TOSConfig

	// Provider configures the trusted client to the storage provider.
	Provider ProviderConfig

	// Audit configures how recipient emails are pseudonymized in audit events.
	Audit AuditConfig

	// LogLevel filters the technical (non-audit) log output.
	LogLevel slog.Level

	// BrandingDir is an optional directory whose files override the embedded
	// white-label defaults served under /branding/.
	BrandingDir string

	// Brand holds the optional white-label text/theme values injected into
	// index.html as window.__ATRIUM__.
	Brand BrandConfig
}

// BrandConfig holds the optional white-label values surfaced to the frontend
// through window.__ATRIUM__. An empty field is omitted so the frontend keeps its
// default.
type BrandConfig struct {
	// Name and Sub are the brand label and sub-label shown in the header.
	Name string
	Sub  string
	// AccentColor is a CSS colour (e.g. #2563eb) mapped onto the accent tokens.
	AccentColor string
	// DefaultTheme is the initial theme for a first-time visitor: light or dark.
	DefaultTheme string
}

// AuditConfig configures email pseudonymization for the audit trail. Salt is
// optional and strengthens the hash against enumeration, at the cost of losing
// lookups for already-logged addresses if it is ever lost.
type AuditConfig struct {
	Pseudonymize bool
	Salt         []byte
}

// ProviderConfig configures the trusted client to the storage provider. BaseURL
// and PrivateKeyPEM are required; the transfer limits have defaults.
type ProviderConfig struct {
	// Type selects the storage backend (e.g. nextcloud); main.go maps it to a
	// constructor. Defaults to nextcloud, so deployments predating this field keep
	// working. An unknown value fails startup.
	Type string
	// BaseURL is the absolute base URL of the provider app.
	BaseURL string
	// PrivateKeyPEM is the PEM-encoded ECDSA P-256 key signing per-request tokens.
	PrivateKeyPEM string
	// MaxUploadSize is the maximum accepted upload body size in bytes; larger
	// uploads are rejected with 413 before any bytes reach the provider.
	MaxUploadSize int64
	// StreamTimeout bounds a single download or upload transfer to the provider.
	StreamTimeout time.Duration
}

// TOSConfig configures the optional Terms-of-Service consent gate.
type TOSConfig struct {
	Enabled bool
	// Path is the ToS Markdown file, loaded once at startup; required when Enabled.
	Path string
	// Version is an explicit label; empty derives it from the content hash, so
	// editing the file forces re-consent.
	Version string
}

// OIDCConfig holds the OpenID Connect client configuration. Atrium is a
// confidential client (client secret) and additionally uses PKCE.
type OIDCConfig struct {
	// Issuer is the OIDC issuer URL used for discovery.
	Issuer string
	// ClientID identifies the Atrium client at the provider.
	ClientID string
	// ClientSecret authenticates the confidential client at the token endpoint.
	ClientSecret string
	// RedirectURI is the absolute callback URL registered at the provider.
	RedirectURI string
	// RequireEmailVerified rejects logins without email_verified=true; set false
	// only for an IdP that does not verify email.
	RequireEmailVerified bool
	// MFAACRValues lists the acr values counting as satisfied MFA; empty disables
	// the check. acr values are provider-defined levels, not method names, so a
	// new MFA method needs no change here as long as it maps to a listed level.
	MFAACRValues []string
}

// Load reads the configuration from environment variables, applying defaults and
// validating required values so the process fails fast on a bad one.
func Load() (Config, error) {
	cfg := Config{
		Addr: getenv("ATRIUM_ADDR", ":8080"),
		OIDC: OIDCConfig{
			Issuer:       os.Getenv("OIDC_ISSUER"),
			ClientID:     os.Getenv("OIDC_CLIENT_ID"),
			ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
			RedirectURI:  os.Getenv("OIDC_REDIRECT_URI"),
		},
		Provider: ProviderConfig{
			Type:          os.Getenv("PROVIDER_TYPE"),
			BaseURL:       os.Getenv("PROVIDER_BASE_URL"),
			PrivateKeyPEM: os.Getenv("PROVIDER_JWT_PRIVATE_KEY"),
		},
	}

	for _, req := range []struct{ name, value string }{
		{"OIDC_ISSUER", cfg.OIDC.Issuer},
		{"OIDC_CLIENT_ID", cfg.OIDC.ClientID},
		{"OIDC_CLIENT_SECRET", cfg.OIDC.ClientSecret},
		{"OIDC_REDIRECT_URI", cfg.OIDC.RedirectURI},
		{"PROVIDER_TYPE", cfg.Provider.Type},
		{"PROVIDER_BASE_URL", cfg.Provider.BaseURL},
		{"PROVIDER_JWT_PRIVATE_KEY", cfg.Provider.PrivateKeyPEM},
	} {
		if req.value == "" {
			return Config{}, fmt.Errorf("missing required environment variable %s", req.name)
		}
	}

	redirect, err := url.Parse(cfg.OIDC.RedirectURI)
	if err != nil || !redirect.IsAbs() {
		return Config{}, fmt.Errorf("OIDC_REDIRECT_URI must be an absolute URL: %q", cfg.OIDC.RedirectURI)
	}
	cfg.SecureCookies = redirect.Scheme == "https"

	if cfg.OIDC.RequireEmailVerified, err = parseBoolDefault("OIDC_REQUIRE_EMAIL_VERIFIED", true); err != nil {
		return Config{}, err
	}
	cfg.OIDC.MFAACRValues = parseCSV(os.Getenv("OIDC_MFA_ACR_VALUES"))

	if cfg.SessionKey, err = decodeSessionKey(os.Getenv("SESSION_KEY")); err != nil {
		return Config{}, err
	}
	if cfg.SessionTTL, err = parseSessionTTL(os.Getenv("SESSION_TTL")); err != nil {
		return Config{}, err
	}
	if cfg.TOS, err = loadTOS(); err != nil {
		return Config{}, err
	}
	if cfg.Provider.MaxUploadSize, err = parseMaxUploadSize(os.Getenv("MAX_UPLOAD_SIZE")); err != nil {
		return Config{}, err
	}
	if cfg.Provider.StreamTimeout, err = parseProviderTimeout(os.Getenv("PROVIDER_TIMEOUT")); err != nil {
		return Config{}, err
	}
	if cfg.Audit.Pseudonymize, err = parseBoolDefault("AUDIT_PSEUDONYMIZE", true); err != nil {
		return Config{}, err
	}
	cfg.Audit.Salt = []byte(os.Getenv("AUDIT_SALT"))
	if cfg.LogLevel, err = parseLogLevel(os.Getenv("LOG_LEVEL")); err != nil {
		return Config{}, err
	}
	cfg.BrandingDir = os.Getenv("BRANDING_DIR")
	if cfg.Brand, err = loadBrand(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// defaultMaxUploadSize caps uploads at 100 MiB unless MAX_UPLOAD_SIZE overrides it.
const defaultMaxUploadSize = 100 << 20

// parseMaxUploadSize reads MAX_UPLOAD_SIZE as a positive byte count, defaulting
// to defaultMaxUploadSize when unset.
func parseMaxUploadSize(raw string) (int64, error) {
	if raw == "" {
		return defaultMaxUploadSize, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("MAX_UPLOAD_SIZE must be an integer number of bytes: %w", err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("MAX_UPLOAD_SIZE must be positive, got %d", n)
	}
	return n, nil
}

// parseProviderTimeout reads PROVIDER_TIMEOUT as a positive Go duration bounding a
// single file transfer, defaulting to 30m for large downloads when unset.
func parseProviderTimeout(raw string) (time.Duration, error) {
	if raw == "" {
		return 30 * time.Minute, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("PROVIDER_TIMEOUT must be a valid Go duration: %w", err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("PROVIDER_TIMEOUT must be positive, got %s", d)
	}
	return d, nil
}

// loadTOS reads the optional consent configuration: off unless TOS_ENABLED is
// truthy, and then TOS_PATH is required.
func loadTOS() (TOSConfig, error) {
	raw := os.Getenv("TOS_ENABLED")
	if raw == "" {
		return TOSConfig{}, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return TOSConfig{}, fmt.Errorf("TOS_ENABLED must be a boolean: %w", err)
	}
	if !enabled {
		return TOSConfig{}, nil
	}
	path := os.Getenv("TOS_PATH")
	if path == "" {
		return TOSConfig{}, fmt.Errorf("TOS_ENABLED is true but TOS_PATH is not set")
	}
	return TOSConfig{Enabled: true, Path: path, Version: os.Getenv("TOS_VERSION")}, nil
}

// loadBrand reads the optional white-label brand configuration, validating
// BRAND_DEFAULT_THEME and BRAND_ACCENT_COLOR at startup so a typo fails fast.
func loadBrand() (BrandConfig, error) {
	theme := os.Getenv("BRAND_DEFAULT_THEME")
	if theme != "" && theme != "light" && theme != "dark" {
		return BrandConfig{}, fmt.Errorf("BRAND_DEFAULT_THEME must be light or dark, got %q", theme)
	}
	accent := os.Getenv("BRAND_ACCENT_COLOR")
	if accent != "" && !isHexColor(accent) {
		return BrandConfig{}, fmt.Errorf("BRAND_ACCENT_COLOR must be a hex colour like #2563eb, got %q", accent)
	}
	return BrandConfig{
		Name:         os.Getenv("BRAND_NAME"),
		Sub:          os.Getenv("BRAND_SUB"),
		AccentColor:  accent,
		DefaultTheme: theme,
	}, nil
}

// isHexColor reports whether s is a #-prefixed hex colour (3, 4, 6 or 8 digits).
// Restricting the accent to hex keeps it safe to interpolate into the injected CSS.
func isHexColor(s string) bool {
	n := len(s) - 1
	if s == "" || s[0] != '#' || (n != 3 && n != 4 && n != 6 && n != 8) {
		return false
	}
	for _, c := range s[1:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// decodeSessionKey decodes the base64 session key and enforces a 32-byte minimum
// so the HMAC-SHA256 signature has adequate entropy. All base64 variants are accepted.
func decodeSessionKey(raw string) ([]byte, error) {
	if raw == "" {
		return nil, fmt.Errorf("missing required environment variable SESSION_KEY")
	}
	var key []byte
	var err error
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.URLEncoding,
		base64.RawStdEncoding, base64.RawURLEncoding,
	} {
		if key, err = enc.DecodeString(raw); err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("SESSION_KEY must be valid base64: %w", err)
	}
	if len(key) < 32 {
		return nil, fmt.Errorf("SESSION_KEY must decode to at least 32 bytes, got %d", len(key))
	}
	return key, nil
}

// parseSessionTTL parses the session lifetime, defaulting to 12h when unset.
func parseSessionTTL(raw string) (time.Duration, error) {
	if raw == "" {
		return 12 * time.Hour, nil
	}
	ttl, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("SESSION_TTL must be a valid Go duration: %w", err)
	}
	if ttl <= 0 {
		return 0, fmt.Errorf("SESSION_TTL must be positive, got %s", ttl)
	}
	return ttl, nil
}

// parseBoolDefault reads key as a boolean, defaulting to def when unset.
func parseBoolDefault(key string, def bool) (bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", key, err)
	}
	return v, nil
}

// parseLogLevel reads LOG_LEVEL (debug|info|warn|error, case-insensitive),
// defaulting to info when unset.
func parseLogLevel(raw string) (slog.Level, error) {
	if raw == "" {
		return slog.LevelInfo, nil
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(raw)); err != nil {
		return 0, fmt.Errorf("LOG_LEVEL must be one of debug, info, warn, error: %w", err)
	}
	return level, nil
}

// parseCSV splits a comma-separated env value into a trimmed, non-empty slice.
// It returns nil when raw is empty, so callers treat "unset" as "no values".
func parseCSV(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
