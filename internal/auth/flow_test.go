package auth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/atrium-secureshare/atrium-core/internal/audit"
	"github.com/atrium-secureshare/atrium-core/internal/auth"
	"github.com/atrium-secureshare/atrium-core/internal/authtest"
	"github.com/atrium-secureshare/atrium-core/internal/config"
)

func cookieMap(rec *httptest.ResponseRecorder) map[string]*http.Cookie {
	m := map[string]*http.Cookie{}
	for _, c := range rec.Result().Cookies() {
		m[c.Name] = c
	}
	return m
}

func decodeAudit(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var ev map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &ev); err != nil {
		t.Fatalf("audit line is not valid JSON: %v (%q)", err, buf.String())
	}
	return ev
}

type callbackOpt func(*callbackParams)

type callbackParams struct {
	state string
	next  string
}

// withState overrides the state parameter the callback receives (to exercise
// the state-mismatch rejection). The empty string is a no-op.
func withState(state string) callbackOpt {
	return func(p *callbackParams) { p.state = state }
}

// withNext makes the login leg request ?next=<path>, so the round-trip through
// the oauth_next cookie can be exercised. The empty string is a no-op.
func withNext(next string) callbackOpt {
	return func(p *callbackParams) { p.next = next }
}

// runCallback drives a full authorization-code login against the mock provider
// and returns the callback recorder. It runs the login leg to obtain the flow
// cookies, sets the given ID-token claims (injecting the real flow nonce unless
// the claims already carry one, so a case can force a nonce mismatch), then
// invokes CallbackHandler with those cookies. Nil claims skip claim setup, for
// rejections decided before the token exchange (e.g. a state mismatch).
func runCallback(t *testing.T, p *authtest.Provider, a *auth.OIDCAuth, claims map[string]any, opts ...callbackOpt) *httptest.ResponseRecorder {
	t.Helper()

	var params callbackParams
	for _, opt := range opts {
		opt(&params)
	}

	loginURL := auth.LoginPath
	if params.next != "" {
		loginURL += "?next=" + url.QueryEscape(params.next)
	}
	loginRec := httptest.NewRecorder()
	a.LoginHandler(loginRec, httptest.NewRequest(http.MethodGet, loginURL, nil))
	var cookies []*http.Cookie
	var state, nonce string
	for _, c := range loginRec.Result().Cookies() {
		cookies = append(cookies, c)
		switch c.Name {
		case "oauth_state":
			state = c.Value
		case "oauth_nonce":
			nonce = c.Value
		}
	}

	if claims != nil {
		// Copy so a caller's table literal is never mutated, then bind the flow
		// nonce unless the case supplied its own (to force a nonce mismatch).
		merged := make(map[string]any, len(claims)+1)
		for k, v := range claims {
			merged[k] = v
		}
		if _, ok := merged["nonce"]; !ok {
			merged["nonce"] = nonce
		}
		p.SetIDTokenClaims(merged)
	}

	sentState := state
	if params.state != "" {
		sentState = params.state
	}
	req := httptest.NewRequest(http.MethodGet, auth.CallbackPath+"?code=abc&state="+url.QueryEscape(sentState), nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	a.CallbackHandler(rec, req)
	return rec
}

func TestCallbackRedirectsToNextAfterLogin(t *testing.T) {
	// The next-path round-trip: LoginHandler stores ?next in the oauth_next
	// cookie, and after a successful callback CallbackHandler redirects there
	// instead of the default "/".
	p := authtest.NewProvider(t)
	a := p.Auth(t)
	rec := runCallback(t, p, a, map[string]any{
		"email":          "user@example.com",
		"email_verified": true,
	}, withNext("/files/report.pdf"))

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if loc := rec.Header().Get("Location"); loc != "/files/report.pdf" {
		t.Errorf("Location = %q, want /files/report.pdf", loc)
	}
}

func TestLoginHandlerRedirectsToProvider(t *testing.T) {
	p := authtest.NewProvider(t)
	a := p.Auth(t)

	req := httptest.NewRequest(http.MethodGet, auth.LoginPath, nil)
	rec := httptest.NewRecorder()
	a.LoginHandler(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	q := loc.Query()
	if q.Get("code_challenge") == "" {
		t.Error("missing code_challenge")
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
	if q.Get("state") == "" {
		t.Error("missing state in redirect")
	}
	if q.Get("nonce") == "" {
		t.Error("missing nonce in redirect")
	}

	cookies := cookieMap(rec)
	for _, name := range []string{"oauth_state", "oauth_nonce", "oauth_pkce"} {
		if cookies[name] == nil || cookies[name].Value == "" {
			t.Errorf("missing flow cookie %q", name)
		}
	}
	if q.Get("state") != cookies["oauth_state"].Value {
		t.Error("state param does not match state cookie")
	}
}

func TestCallbackSuccessSetsSession(t *testing.T) {
	p := authtest.NewProvider(t)
	a := p.Auth(t)

	rec := runCallback(t, p, a, map[string]any{
		"email":          "recipient@example.com",
		"email_verified": true,
	})

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want /", loc)
	}
	if cookieMap(rec)["atrium_session"] == nil {
		t.Fatal("expected atrium_session cookie to be set")
	}

	ev := decodeAudit(t, p.AuditBuf)
	if ev["level"] != "AUDIT" || ev["msg"] != audit.EventLogin {
		t.Errorf("level/msg = %v/%v, want AUDIT/%s", ev["level"], ev["msg"], audit.EventLogin)
	}
	if ev["email"] != "recipient@example.com" {
		t.Errorf("email = %v, want the plaintext login email", ev["email"])
	}
}

func TestCallbackAcceptsConfiguredACR(t *testing.T) {
	p := authtest.NewProvider(t)
	a := p.Auth(t, func(c *config.Config) {
		c.OIDC.MFAACRValues = []string{"gold", "2"}
	})

	rec := runCallback(t, p, a, map[string]any{
		"email":          "recipient@example.com",
		"email_verified": true,
		"acr":            "gold",
	})

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if cookieMap(rec)["atrium_session"] == nil {
		t.Fatal("expected session cookie for a token with an accepted acr")
	}

	ev := decodeAudit(t, p.AuditBuf)
	if ev["msg"] != audit.EventLogin || ev["acr"] != "gold" {
		t.Errorf("msg/acr = %v/%v, want %s/gold", ev["msg"], ev["acr"], audit.EventLogin)
	}
}

func TestCallbackMFADisabledIgnoresACR(t *testing.T) {
	p := authtest.NewProvider(t)
	a := p.Auth(t) // MFAACRValues empty: enforcement off.

	// A single-factor (weak) acr is present in the token. With MFA enforcement
	// disabled it must be ignored - the login succeeds - while the acr is still
	// recorded so operators can audit the authentication strength.
	rec := runCallback(t, p, a, map[string]any{
		"email":          "recipient@example.com",
		"email_verified": true,
		"acr":            "1",
	})

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if cookieMap(rec)["atrium_session"] == nil {
		t.Fatal("expected session cookie when MFA enforcement is disabled")
	}

	ev := decodeAudit(t, p.AuditBuf)
	if ev["msg"] != audit.EventLogin || ev["acr"] != "1" {
		t.Errorf("msg/acr = %v/%v, want %s/1 (weak acr accepted but logged)", ev["msg"], ev["acr"], audit.EventLogin)
	}
}

func TestCallbackEmailVerifiedOptOut(t *testing.T) {
	p := authtest.NewProvider(t)
	a := p.Auth(t, func(c *config.Config) {
		c.OIDC.RequireEmailVerified = false
	})

	rec := runCallback(t, p, a, map[string]any{
		"email":          "recipient@example.com",
		"email_verified": false,
	})

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if cookieMap(rec)["atrium_session"] == nil {
		t.Fatal("expected session cookie when email_verified enforcement is disabled")
	}
}

// TestCallbackRejectsEmptyOrMissingEmail covers the single claims.Email == ""
// branch, reached whether the email claim is present but empty or absent
// entirely. Both are rejected with the email_missing audit reason.
func TestCallbackRejectsEmptyOrMissingEmail(t *testing.T) {
	tests := []struct {
		name   string
		claims map[string]any
	}{
		{"empty", map[string]any{"email": "", "email_verified": true}},
		{"missing", map[string]any{"email_verified": true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := authtest.NewProvider(t)
			a := p.Auth(t)

			rec := runCallback(t, p, a, tc.claims)

			if rec.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusSeeOther, rec.Body.String())
			}
			if loc := rec.Header().Get("Location"); loc != "/auth/error" {
				t.Fatalf("Location = %q, want /auth/error", loc)
			}
			if cookieMap(rec)["atrium_session"] != nil {
				t.Fatal("session cookie must not be set without a usable email")
			}

			ev := decodeAudit(t, p.AuditBuf)
			if ev["msg"] != audit.EventLoginFailed || ev["reason"] != "email_missing" {
				t.Errorf("msg/reason = %v/%v, want %s/email_missing", ev["msg"], ev["reason"], audit.EventLoginFailed)
			}
		})
	}
}

// TestCallbackRejects covers the remaining sign-in rejections. They share the
// same outcome - a 303 bounce to /auth/error and no session cookie - and, where
// the failure is a policy denial, an audit login-failed reason. Each reject
// trigger is one case: unverified email, a missing MFA factor, a nonce mismatch
// and a CSRF state mismatch. (Empty/missing email is covered by
// TestCallbackRejectsEmptyOrMissingEmail.)
func TestCallbackRejects(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*config.Config)
		claims     map[string]any
		state      string // state-param override; empty uses the real flow state
		wantStatus int
		wantReason string // audit login-failed reason; empty means a technical (non-audit) reject
		wantACR    string // audit acr on the failure; empty skips the check
	}{
		{
			name:       "unverified email",
			claims:     map[string]any{"email": "recipient@example.com", "email_verified": false},
			wantStatus: http.StatusSeeOther,
			wantReason: "email_not_verified",
		},
		{
			name:       "mfa factor missing",
			mutate:     func(c *config.Config) { c.OIDC.MFAACRValues = []string{"gold", "2"} },
			claims:     map[string]any{"email": "recipient@example.com", "email_verified": true, "acr": "1"},
			wantStatus: http.StatusSeeOther,
			wantReason: "mfa_missing",
			wantACR:    "1",
		},
		{
			name:       "nonce mismatch",
			claims:     map[string]any{"email": "recipient@example.com", "email_verified": true, "nonce": "attacker-controlled-nonce"},
			wantStatus: http.StatusSeeOther,
		},
		{
			name:       "state mismatch",
			state:      "wrong-state",
			wantStatus: http.StatusSeeOther,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := authtest.NewProvider(t)
			var opts []func(*config.Config)
			if tc.mutate != nil {
				opts = append(opts, tc.mutate)
			}
			a := p.Auth(t, opts...)

			rec := runCallback(t, p, a, tc.claims, withState(tc.state))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			// Every rejected sign-in bounces to the SPA error route; the reason
			// stays in the logs, never in the URL.
			if loc := rec.Header().Get("Location"); loc != "/auth/error" {
				t.Fatalf("Location = %q, want /auth/error", loc)
			}
			if cookieMap(rec)["atrium_session"] != nil {
				t.Fatal("session cookie must not be set on a rejected sign-in")
			}

			if tc.wantReason == "" {
				return // technical reject (nonce/state): no audit event to assert
			}
			ev := decodeAudit(t, p.AuditBuf)
			if ev["msg"] != audit.EventLoginFailed || ev["reason"] != tc.wantReason {
				t.Errorf("msg/reason = %v/%v, want %s/%s", ev["msg"], ev["reason"], audit.EventLoginFailed, tc.wantReason)
			}
			if tc.wantACR != "" && ev["acr"] != tc.wantACR {
				t.Errorf("acr = %v, want %s (the rejected acr value)", ev["acr"], tc.wantACR)
			}
		})
	}
}
