package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/atrium-secureshare/atrium-core/internal/audit"
)

// contextKey is an unexported context-key type, avoiding collisions with other
// packages.
type contextKey struct{ name string }

var sessionContextKey = contextKey{"session"}

// SessionExpiresHeader advertises the seconds left before the session ends, so
// the frontend can warn ahead of an idle logout without polling for it. Named
// after the application rather than X-prefixed (RFC 6648) and specific enough
// that a future standard field cannot collide with it.
const SessionExpiresHeader = "Atrium-Session-Expires-In"

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
		session = a.touchSession(w, session)
		ctx := context.WithValue(r.Context(), sessionContextKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// touchSession counts the request as activity and advertises the resulting
// deadline. Re-issuing the cookie is throttled, so the header is derived from the
// LastSeen actually stored rather than from now, which would overstate it.
func (a *OIDCAuth) touchSession(w http.ResponseWriter, session SessionData) SessionData {
	if a.sessionIdleTTL > 0 && time.Since(session.lastActivity()) >= sessionRefreshInterval {
		refreshed := session
		refreshed.LastSeen = time.Now()
		cookie, err := a.sessionCookie(refreshed)
		if err != nil {
			a.logger.Error("refresh session cookie", "error", err)
		} else {
			http.SetCookie(w, cookie)
			session = refreshed
		}
	}
	w.Header().Set(SessionExpiresHeader, strconv.Itoa(secondsUntil(a.sessionDeadline(session))))
	return session
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
// follow-up request would silently sign the recipient back in. Both paths end on
// loggedOutPath.
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
	http.Redirect(w, r, loggedOutPath, http.StatusFound)
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
