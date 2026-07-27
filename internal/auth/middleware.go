package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/atrium-secureshare/atrium-core/internal/audit"
)

// contextKey is an unexported context-key type, avoiding collisions with other
// packages.
type contextKey struct{ name string }

var sessionContextKey = contextKey{"session"}

// SessionFromContext returns the authenticated session stored by RequireAuth,
// and whether one was present.
func SessionFromContext(ctx context.Context) (SessionData, bool) {
	s, ok := ctx.Value(sessionContextKey).(SessionData)
	return s, ok
}

// NewContext returns a copy of ctx carrying session, as RequireAuth stores it, so
// downstream middleware and tests can build authenticated contexts.
func NewContext(ctx context.Context, session SessionData) context.Context {
	return context.WithValue(ctx, sessionContextKey, session)
}

// RequireAuth admits only requests with a valid session cookie. On failure it
// clears the stale cookie and redirects browser navigations to login or returns
// 401 JSON for API requests.
func (a *OIDCAuth) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := a.validateSessionCookie(r)
		if err != nil {
			http.SetCookie(w, a.clearSessionCookie())
			a.denyUnauthenticated(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), sessionContextKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// denyUnauthenticated answers 401 JSON for API calls, else redirects to login.
func (a *OIDCAuth) denyUnauthenticated(w http.ResponseWriter, r *http.Request) {
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthenticated"})
		return
	}
	loginURL := LoginPath
	if next := safeNextPath(r.URL.RequestURI()); next != "" {
		loginURL += "?next=" + url.QueryEscape(next)
	}
	http.Redirect(w, r, loginURL, http.StatusFound)
}

// LogoutHandler clears the local session and, when the provider supports it,
// redirects to end_session_endpoint to end the SSO session too — otherwise a
// follow-up request would silently sign the recipient back in.
func (a *OIDCAuth) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	session, err := a.validateSessionCookie(r)
	http.SetCookie(w, a.clearSessionCookie())
	if err == nil {
		audit.Log(a.logger, r, slog.LevelInfo, audit.EventLogout, slog.String("email", session.Email))
	}

	if a.endSessionEndpoint != "" && err == nil && session.IDToken != "" {
		params := url.Values{
			"id_token_hint":            {session.IDToken},
			"post_logout_redirect_uri": {a.postLogoutRedirect},
			"client_id":                {a.oauth2Config.ClientID},
		}
		http.Redirect(w, r, a.endSessionEndpoint+"?"+params.Encode(), http.StatusFound)
		return
	}
	http.Redirect(w, r, LoginPath, http.StatusFound)
}

// wantsJSON reports whether the request should get a JSON error rather than a
// redirect: API paths, or clients accepting JSON but not HTML.
func wantsJSON(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		return true
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html")
}
