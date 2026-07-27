// Package auth implements the OpenID Connect authorization-code flow (with PKCE)
// and stateless, HMAC-signed session cookies. The core is a confidential client
// (client secret plus PKCE as defense in depth); no server-side session state is
// kept, the identity travels in a signed cookie (see session.go).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/atrium-secureshare/atrium-core/internal/audit"
	"github.com/atrium-secureshare/atrium-core/internal/config"
)

// Email-validation errors.
var (
	// ErrEmailNotVerified indicates the OIDC identity has no verified email.
	ErrEmailNotVerified = errors.New("email not verified")
	// ErrEmailMismatch indicates the OIDC email does not match the expected one.
	ErrEmailMismatch = errors.New("email mismatch")
)

// Auth endpoint paths, all under /auth. CallbackPath must match the path
// component of OIDC_REDIRECT_URI configured at the provider.
const (
	LoginPath    = "/auth/login"
	CallbackPath = "/auth/callback"
	LogoutPath   = "/auth/logout"
)

// errorPath is the client route the callback redirects to on a failed sign-in.
// The reason is logged, never carried in the URL, so it cannot be reflected into
// the page or read from the address bar.
const errorPath = "/auth/error"

// flowCookiePath scopes the short-lived flow cookies to the auth endpoints, so
// they are never sent with requests to the rest of the application.
const flowCookiePath = "/auth"

// flowTTL is the lifetime of the cookies carrying CSRF state, nonce and PKCE
// verifier across the redirect.
const flowTTL = 5 * time.Minute

// Flow cookie names used between the login redirect and the callback.
const (
	cookieState    = "oauth_state"
	cookieNonce    = "oauth_nonce"
	cookiePKCE     = "oauth_pkce"
	cookieNextPath = "oauth_next"
)

// OIDCAuth wires the OpenID Connect provider, the OAuth2 client configuration
// and the session-cookie secret. It is safe for concurrent use.
type OIDCAuth struct {
	provider     *oidc.Provider
	oauth2Config *oauth2.Config
	verifier     *oidc.IDTokenVerifier
	logger       *slog.Logger

	sessionKey    []byte
	sessionTTL    time.Duration
	secureCookies bool

	requireEmailVerified bool
	// mfaACR holds the acr values that count as a satisfied second factor; empty
	// disables the check.
	mfaACR map[string]struct{}

	// endSessionEndpoint is the provider's RP-initiated logout URL; empty when the
	// provider advertises none, so logout then only clears the local session.
	endSessionEndpoint string
	postLogoutRedirect string
}

// idTokenClaims holds the ID-token claims Atrium relies on. acr (authentication
// context class reference) attests the authentication strength for the MFA check.
type idTokenClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Nonce         string `json:"nonce"`
	ACR           string `json:"acr"`
}

// NewOIDCAuth performs OIDC discovery and builds the OAuth2 client, returning
// discovery failures so the caller can fail fast at startup.
func NewOIDCAuth(ctx context.Context, cfg config.Config, logger *slog.Logger) (*OIDCAuth, error) {
	provider, err := oidc.NewProvider(ctx, cfg.OIDC.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery for %q: %w", cfg.OIDC.Issuer, err)
	}

	// end_session_endpoint is not exposed by the base discovery struct; read it
	// from the raw metadata so logout can end the provider session too.
	var meta struct {
		EndSessionEndpoint string `json:"end_session_endpoint"`
	}
	if err := provider.Claims(&meta); err != nil {
		return nil, fmt.Errorf("parse oidc discovery metadata: %w", err)
	}

	postLogout, err := postLogoutRedirectURI(cfg.OIDC.RedirectURI)
	if err != nil {
		return nil, err
	}

	mfaACR := make(map[string]struct{}, len(cfg.OIDC.MFAACRValues))
	for _, v := range cfg.OIDC.MFAACRValues {
		mfaACR[v] = struct{}{}
	}
	// Make a disabled opt-out loud at boot so it is never silently in effect.
	if len(mfaACR) == 0 {
		logger.Warn("OIDC_MFA_ACR_VALUES is empty: MFA is not enforced at the gateway; the identity provider is trusted for authentication strength")
	}
	if !cfg.OIDC.RequireEmailVerified {
		logger.Warn("OIDC_REQUIRE_EMAIL_VERIFIED is false: logins with an unverified email are accepted")
	}

	return &OIDCAuth{
		provider: provider,
		oauth2Config: &oauth2.Config{
			ClientID:     cfg.OIDC.ClientID,
			ClientSecret: cfg.OIDC.ClientSecret,
			RedirectURL:  cfg.OIDC.RedirectURI,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "email"},
		},
		verifier:             provider.Verifier(&oidc.Config{ClientID: cfg.OIDC.ClientID}),
		logger:               logger,
		sessionKey:           cfg.SessionKey,
		sessionTTL:           cfg.SessionTTL,
		secureCookies:        cfg.SecureCookies,
		requireEmailVerified: cfg.OIDC.RequireEmailVerified,
		mfaACR:               mfaACR,
		endSessionEndpoint:   meta.EndSessionEndpoint,
		postLogoutRedirect:   postLogout,
	}, nil
}

// postLogoutRedirectURI derives the post-logout URL from the redirect URI's
// origin. It must be registered as a valid post-logout redirect URI at the provider.
func postLogoutRedirectURI(redirectURI string) (string, error) {
	u, err := url.Parse(redirectURI)
	if err != nil || !u.IsAbs() {
		return "", fmt.Errorf("derive post-logout redirect from %q: %w", redirectURI, err)
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}).String(), nil
}

// LoginHandler starts the authorization-code flow: it generates CSRF state, a
// nonce and a PKCE verifier, stores them in short-lived cookies and redirects to
// the provider.
func (a *OIDCAuth) LoginHandler(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken()
	if err != nil {
		a.serverError(w, "generate state", err)
		return
	}
	nonce, err := randomToken()
	if err != nil {
		a.serverError(w, "generate nonce", err)
		return
	}
	verifier := oauth2.GenerateVerifier()

	a.setFlowCookie(w, cookieState, state)
	a.setFlowCookie(w, cookieNonce, nonce)
	a.setFlowCookie(w, cookiePKCE, verifier)
	if next := safeNextPath(r.URL.Query().Get("next")); next != "" {
		a.setFlowCookie(w, cookieNextPath, next)
	}

	authURL := a.oauth2Config.AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// CallbackHandler completes the flow: it validates state, exchanges the code,
// verifies the ID token and nonce, enforces email_verified and the optional MFA
// acr, and establishes the session cookie.
func (a *OIDCAuth) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	next := "/"
	if c, err := r.Cookie(cookieNextPath); err == nil {
		if p := safeNextPath(c.Value); p != "" {
			next = p
		}
	}
	// Flow cookies are single-use: clear them regardless of the outcome.
	defer a.clearFlowCookies(w)

	stateCookie, err := r.Cookie(cookieState)
	if err != nil || subtle.ConstantTimeCompare([]byte(stateCookie.Value), []byte(r.URL.Query().Get("state"))) != 1 {
		a.logger.Warn("oauth state missing or mismatched")
		a.authError(w, r)
		return
	}

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		a.logger.Warn("oidc provider returned error", "error", errParam, "description", r.URL.Query().Get("error_description"))
		a.authError(w, r)
		return
	}

	pkceCookie, err := r.Cookie(cookiePKCE)
	if err != nil {
		a.logger.Warn("pkce verifier cookie missing")
		a.authError(w, r)
		return
	}

	oauth2Token, err := a.oauth2Config.Exchange(r.Context(),
		r.URL.Query().Get("code"),
		oauth2.VerifierOption(pkceCookie.Value),
	)
	if err != nil {
		a.logger.Warn("code exchange failed", "error", err)
		a.authError(w, r)
		return
	}

	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		a.logger.Warn("token response missing id_token")
		a.authError(w, r)
		return
	}
	idToken, err := a.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		a.logger.Warn("id_token verification failed", "error", err)
		a.authError(w, r)
		return
	}

	nonceCookie, err := r.Cookie(cookieNonce)
	if err != nil || subtle.ConstantTimeCompare([]byte(nonceCookie.Value), []byte(idToken.Nonce)) != 1 {
		a.logger.Warn("oidc nonce missing or mismatched")
		a.authError(w, r)
		return
	}

	var claims idTokenClaims
	if err := idToken.Claims(&claims); err != nil {
		a.logger.Warn("id_token claims decode failed", "error", err)
		a.authError(w, r)
		return
	}
	if a.requireEmailVerified && !claims.EmailVerified {
		audit.Log(a.logger, r, audit.LevelAudit, audit.EventLoginFailed, slog.String("email", claims.Email), slog.String("reason", "email_not_verified"))
		a.authError(w, r)
		return
	}
	if claims.Email == "" {
		audit.Log(a.logger, r, audit.LevelAudit, audit.EventLoginFailed, slog.String("reason", "email_missing"))
		a.authError(w, r)
		return
	}
	// Defense in depth: when MFA acr values are configured, fail closed unless the
	// token attests one of them.
	if len(a.mfaACR) > 0 {
		if _, ok := a.mfaACR[claims.ACR]; !ok {
			audit.Log(a.logger, r, audit.LevelAudit, audit.EventLoginFailed, slog.String("email", claims.Email), slog.String("reason", "mfa_missing"), slog.String("acr", claims.ACR))
			a.authError(w, r)
			return
		}
	}

	cookie, err := a.createSessionCookie(claims.Email, rawIDToken)
	if err != nil {
		a.serverError(w, "create session", err)
		return
	}
	http.SetCookie(w, cookie)
	// Record the received acr so operators can discover the real values before
	// turning enforcement on.
	audit.Log(a.logger, r, audit.LevelAudit, audit.EventLogin, slog.String("email", claims.Email), slog.String("acr", claims.ACR))
	http.Redirect(w, r, next, http.StatusFound)
}

// randomToken returns a base64url-encoded 32-byte random token for CSRF state
// and the nonce.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// safeNextPath returns raw if it is a safe same-site relative path, else "",
// rejecting absolute and protocol-relative URLs to prevent open redirects.
func safeNextPath(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" {
		return ""
	}
	return u.EscapedPath() + optionalRawQuery(u)
}

func optionalRawQuery(u *url.URL) string {
	if u.RawQuery == "" {
		return ""
	}
	return "?" + u.RawQuery
}

func (a *OIDCAuth) setFlowCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     flowCookiePath,
		MaxAge:   int(flowTTL.Seconds()),
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *OIDCAuth) clearFlowCookies(w http.ResponseWriter) {
	for _, name := range []string{cookieState, cookieNonce, cookiePKCE, cookieNextPath} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     flowCookiePath,
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   a.secureCookies,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

// serverError logs an internal error without leaking details to the client.
func (a *OIDCAuth) serverError(w http.ResponseWriter, msg string, err error) {
	a.logger.Error(msg, "error", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

// authError redirects a failed sign-in to the client error route. The reason is
// kept out of the URL (callers log it) so it is never reflected into the page,
// and the redirect strips the OAuth code/state from the address bar.
func (a *OIDCAuth) authError(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, errorPath, http.StatusSeeOther)
}
