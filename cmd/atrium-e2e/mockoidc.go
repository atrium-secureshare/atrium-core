package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// mockOIDC is a browser-drivable OIDC provider: unlike internal/authtest it
// serves a real authorization endpoint with a login form, so Playwright drives
// the actual authorization-code + PKCE flow.
type mockOIDC struct {
	issuer   string
	clientID string
	signer   jose.Signer
	jwks     jose.JSONWebKeySet

	mu    sync.Mutex
	codes map[string]codeData
}

// codeData carries the login-form state to the token endpoint. The nonce is
// echoed from the auth request so the core's nonce check passes.
type codeData struct {
	email    string
	verified bool
	nonce    string
}

// startMockOIDC serves discovery, JWKS and the auth/token endpoints on an
// ephemeral local port reachable by both the browser and the core.
func startMockOIDC(clientID string) (*mockOIDC, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate oidc key: %w", err)
	}
	const kid = "e2e-key"
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid),
	)
	if err != nil {
		return nil, fmt.Errorf("new oidc signer: %w", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen mock oidc: %w", err)
	}
	p := &mockOIDC{
		issuer:   "http://" + ln.Addr().String(),
		clientID: clientID,
		signer:   signer,
		jwks: jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: key.Public(), KeyID: kid, Algorithm: string(jose.RS256), Use: "sig",
		}}},
		codes: map[string]codeData{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.handleDiscovery)
	mux.HandleFunc("GET /jwks", p.handleJWKS)
	mux.HandleFunc("GET /auth", p.handleAuthForm)
	mux.HandleFunc("POST /auth", p.handleAuthSubmit)
	mux.HandleFunc("POST /token", p.handleToken)
	mux.HandleFunc("GET /logout", p.handleLogout)

	go func() { _ = http.Serve(ln, mux) }()
	return p, nil
}

func (p *mockOIDC) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                p.issuer,
		"authorization_endpoint":                p.issuer + "/auth",
		"token_endpoint":                        p.issuer + "/token",
		"jwks_uri":                              p.issuer + "/jwks",
		"end_session_endpoint":                  p.issuer + "/logout",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "email"},
	})
}

func (p *mockOIDC) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, p.jwks)
}

// handleAuthForm renders the mock login page, preserving the OAuth2 params as
// hidden fields so the submit can complete the redirect.
func (p *mockOIDC) handleAuthForm(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html lang="de"><head><meta charset="utf-8"><title>Mock Login</title></head>
<body>
<h1>Mock Identity Provider</h1>
<form method="POST" action="/auth">
  <label>E-Mail <input name="email" aria-label="E-Mail" autofocus></label>
  <label><input type="checkbox" name="verified" checked> Verifiziert</label>
  <input type="hidden" name="state" value="%s">
  <input type="hidden" name="nonce" value="%s">
  <input type="hidden" name="redirect_uri" value="%s">
  <button type="submit">Anmelden</button>
</form>
</body></html>`,
		html.EscapeString(q.Get("state")),
		html.EscapeString(q.Get("nonce")),
		html.EscapeString(q.Get("redirect_uri")),
	)
}

// handleAuthSubmit mints an authorization code bound to the chosen identity and
// redirects back to the core's callback.
func (p *mockOIDC) handleAuthSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	code := randomHex()
	p.mu.Lock()
	p.codes[code] = codeData{
		email:    r.FormValue("email"),
		verified: r.FormValue("verified") != "",
		nonce:    r.FormValue("nonce"),
	}
	p.mu.Unlock()

	redirect := r.FormValue("redirect_uri")
	sep := "?"
	if u, err := url.Parse(redirect); err == nil && u.RawQuery != "" {
		sep = "&"
	}
	http.Redirect(w, r, redirect+sep+"code="+url.QueryEscape(code)+"&state="+url.QueryEscape(r.FormValue("state")), http.StatusFound)
}

// handleToken exchanges a code for an ID token. PKCE and the client secret are
// not verified: this is a mock, and flow security is covered by unit tests.
func (p *mockOIDC) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	data, ok := p.codes[r.FormValue("code")]
	delete(p.codes, r.FormValue("code"))
	p.mu.Unlock()
	if !ok {
		http.Error(w, "invalid_grant", http.StatusBadRequest)
		return
	}

	now := time.Now()
	idToken, err := jwt.Signed(p.signer).Claims(map[string]any{
		"iss":            p.issuer,
		"aud":            p.clientID,
		"sub":            "e2e|" + data.email,
		"iat":            now.Unix(),
		"exp":            now.Add(time.Hour).Unix(),
		"email":          data.email,
		"email_verified": data.verified,
		"nonce":          data.nonce,
	}).Serialize()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"access_token": "e2e-access-token",
		"token_type":   "Bearer",
		"id_token":     idToken,
		"expires_in":   3600,
	})
}

// handleLogout bounces back to the post-logout redirect the core supplies.
func (p *mockOIDC) handleLogout(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("post_logout_redirect_uri")
	if target == "" {
		target = "/"
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func randomHex() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
