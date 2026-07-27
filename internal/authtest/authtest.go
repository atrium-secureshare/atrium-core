// Package authtest provides an in-process mock OpenID Connect provider for
// exercising the auth package end to end in unit tests. It serves discovery,
// JWKS and a token endpoint, and lets tests dictate the next ID token's claims.
package authtest

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/atrium-secureshare/atrium-core/internal/audit"
	"github.com/atrium-secureshare/atrium-core/internal/auth"
	"github.com/atrium-secureshare/atrium-core/internal/config"
)

// ClientID is the OAuth2 client id the mock provider expects and audiences.
const ClientID = "atrium-core-test"

// Provider is a running mock OIDC provider backed by an httptest.Server.
type Provider struct {
	Server *httptest.Server
	Issuer string

	// AuditBuf collects the audit lines from the OIDCAuth built by Auth,
	// unpseudonymized so tests can assert on plaintext emails/reasons.
	AuditBuf *bytes.Buffer

	signer jose.Signer
	jwks   jose.JSONWebKeySet

	mu         sync.Mutex
	nextClaims map[string]any
}

// NewProvider starts a mock OIDC provider and registers cleanup with t.
func NewProvider(t *testing.T) *Provider {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const kid = "test-key"
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid),
	)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}

	p := &Provider{
		signer: signer,
		jwks: jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key:       key.Public(),
			KeyID:     kid,
			Algorithm: string(jose.RS256),
			Use:       "sig",
		}}},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.handleDiscovery)
	mux.HandleFunc("GET /jwks", p.handleJWKS)
	mux.HandleFunc("POST /token", p.handleToken)
	mux.HandleFunc("GET /auth", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	p.Server = httptest.NewServer(mux)
	p.Issuer = p.Server.URL
	t.Cleanup(p.Server.Close)
	return p
}

// SetIDTokenClaims sets the extra claims merged into the next minted ID token.
func (p *Provider) SetIDTokenClaims(claims map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextClaims = claims
}

// Auth builds an *auth.OIDCAuth wired to this provider. Optional mutators tweak
// the config before the client is built; the defaults mirror production.
func (p *Provider) Auth(t *testing.T, opts ...func(*config.Config)) *auth.OIDCAuth {
	t.Helper()
	cfg := config.Config{
		OIDC: config.OIDCConfig{
			Issuer:               p.Issuer,
			ClientID:             ClientID,
			ClientSecret:         "test-secret",
			RedirectURI:          "http://localhost:8080/auth/callback",
			RequireEmailVerified: true,
		},
		SessionKey:    []byte("0123456789abcdef0123456789abcdef"),
		SessionTTL:    time.Hour,
		SecureCookies: false,
	}
	for _, o := range opts {
		o(&cfg)
	}
	// Pseudonymization is off so tests can assert on plaintext emails/reasons.
	p.AuditBuf = &bytes.Buffer{}
	logger := audit.New(p.AuditBuf, slog.LevelInfo, nil, false)
	a, err := auth.NewOIDCAuth(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("NewOIDCAuth: %v", err)
	}
	// Drop startup log lines so tests only observe records their own request triggers.
	p.AuditBuf.Reset()
	return a
}

// SessionCookie completes a full login for the given verified email and returns
// the resulting session cookie, so tests can issue authenticated requests.
func (p *Provider) SessionCookie(t *testing.T, a *auth.OIDCAuth, email string) *http.Cookie {
	t.Helper()

	loginRec := httptest.NewRecorder()
	a.LoginHandler(loginRec, httptest.NewRequest(http.MethodGet, auth.LoginPath, nil))
	var flow []*http.Cookie
	var state, nonce string
	for _, c := range loginRec.Result().Cookies() {
		flow = append(flow, c)
		switch c.Name {
		case "oauth_state":
			state = c.Value
		case "oauth_nonce":
			nonce = c.Value
		}
	}

	p.SetIDTokenClaims(map[string]any{"email": email, "email_verified": true, "nonce": nonce})

	req := httptest.NewRequest(http.MethodGet, auth.CallbackPath+"?code=abc&state="+url.QueryEscape(state), nil)
	for _, c := range flow {
		req.AddCookie(c)
	}
	cbRec := httptest.NewRecorder()
	a.CallbackHandler(cbRec, req)
	for _, c := range cbRec.Result().Cookies() {
		if c.Name == "atrium_session" {
			return c
		}
	}
	t.Fatalf("login did not set a session cookie: status=%d body=%s", cbRec.Code, cbRec.Body.String())
	return nil
}

func (p *Provider) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                p.Issuer,
		"authorization_endpoint":                p.Issuer + "/auth",
		"token_endpoint":                        p.Issuer + "/token",
		"end_session_endpoint":                  p.Issuer + "/logout",
		"jwks_uri":                              p.Issuer + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (p *Provider) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, p.jwks)
}

func (p *Provider) handleToken(w http.ResponseWriter, _ *http.Request) {
	p.mu.Lock()
	extra := p.nextClaims
	p.mu.Unlock()

	claims := map[string]any{
		"iss": p.Issuer,
		"aud": ClientID,
		"sub": "test-subject",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	for k, v := range extra {
		claims[k] = v
	}

	idToken, err := jwt.Signed(p.signer).Claims(claims).Serialize()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"access_token": "test-access-token",
		"token_type":   "Bearer",
		"id_token":     idToken,
		"expires_in":   3600,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
