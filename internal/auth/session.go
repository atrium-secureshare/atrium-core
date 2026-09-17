package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/atrium-secureshare/atrium-core/internal/audit"
	"github.com/atrium-secureshare/atrium-core/internal/signedcookie"
)

const sessionCookieName = "atrium_session"

// sessionRefreshInterval throttles re-issuing the session cookie on activity. The
// cookie carries the ID token, so a Set-Cookie per request would be kilobytes of
// overhead for sub-second precision nobody needs.
const sessionRefreshInterval = time.Minute

// Session errors returned by session validation.
var (
	// ErrNoSession indicates that no session cookie was present.
	ErrNoSession = errors.New("no session cookie")
	// ErrInvalidSession indicates a malformed or tampered session cookie.
	ErrInvalidSession = errors.New("invalid session")
	// ErrSessionExpired indicates a session whose absolute lifetime elapsed.
	ErrSessionExpired = errors.New("session expired")
	// ErrSessionIdle indicates a session that went unused for SESSION_IDLE_TTL.
	ErrSessionIdle = errors.New("session idle")
)

// SessionData is the payload in the signed session cookie. The core is stateless:
// everything needed to authorize a request lives in the cookie, HMAC-signed.
type SessionData struct {
	// Email is the verified OIDC email of the authenticated recipient.
	Email string `json:"email"`
	// IssuedAt is when the session was created.
	IssuedAt time.Time `json:"iat"`
	// ExpiresAt is the absolute session expiry, never moved by activity.
	ExpiresAt time.Time `json:"exp"`
	// LastSeen is the last request that counted as activity, the anchor of the
	// idle timeout.
	LastSeen time.Time `json:"seen"`
	// IDToken is kept solely to pass as id_token_hint on RP-initiated logout, so
	// the provider ends its own session without a prompt.
	IDToken string `json:"idt,omitempty"`
}

// lastActivity falls back to IssuedAt for cookies signed before LastSeen existed,
// so an upgrade does not sign everyone out.
func (d SessionData) lastActivity() time.Time {
	if d.LastSeen.IsZero() {
		return d.IssuedAt
	}
	return d.LastSeen
}

// signAndEncode serializes data to JSON and seals it into a signed token.
func signAndEncode(data SessionData, key []byte) (string, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal session: %w", err)
	}
	return signedcookie.Seal(key, payload), nil
}

// decodeAndVerify reverses signAndEncode: verify the signature, then check the
// absolute expiry.
func decodeAndVerify(token string, key []byte) (SessionData, error) {
	payload, err := signedcookie.Open(key, token)
	if err != nil {
		return SessionData{}, ErrInvalidSession
	}
	var data SessionData
	if err := json.Unmarshal(payload, &data); err != nil {
		return SessionData{}, ErrInvalidSession
	}
	if time.Now().After(data.ExpiresAt) {
		return SessionData{}, ErrSessionExpired
	}
	return data, nil
}

// createSessionCookie builds a signed session cookie for the given verified
// email and raw ID token.
func (a *OIDCAuth) createSessionCookie(email, idToken string) (*http.Cookie, error) {
	now := time.Now()
	return a.sessionCookie(SessionData{
		Email:     email,
		IssuedAt:  now,
		ExpiresAt: now.Add(a.sessionAbsoluteTTL),
		LastSeen:  now,
		IDToken:   idToken,
	})
}

// sessionCookie seals data into the session cookie. MaxAge tracks the session
// deadline so the browser drops an idle cookie on its own.
func (a *OIDCAuth) sessionCookie(data SessionData) (*http.Cookie, error) {
	token, err := signAndEncode(data, a.sessionKey)
	if err != nil {
		return nil, err
	}
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   secondsUntil(a.sessionDeadline(data)),
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
	}, nil
}

// secondsUntil rounds rather than truncates, so a value taken right after issuing
// the cookie is the exact lifetime instead of one second short.
func secondsUntil(t time.Time) int {
	return int(time.Until(t).Round(time.Second).Seconds())
}

// sessionDeadline is when the session ends: the absolute expiry, or the idle
// deadline when that comes first.
func (a *OIDCAuth) sessionDeadline(data SessionData) time.Time {
	if a.sessionIdleTTL <= 0 {
		return data.ExpiresAt
	}
	if idle := data.lastActivity().Add(a.sessionIdleTTL); idle.Before(data.ExpiresAt) {
		return idle
	}
	return data.ExpiresAt
}

// validateSessionCookie reads and verifies the session cookie from the request.
// The idle check lives here rather than in the caller so no call site can skip it.
func (a *OIDCAuth) validateSessionCookie(r *http.Request) (SessionData, error) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return SessionData{}, ErrNoSession
	}
	data, err := decodeAndVerify(c.Value, a.sessionKey)
	if err != nil {
		return SessionData{}, err
	}
	if a.sessionIdleTTL > 0 && time.Now().After(data.lastActivity().Add(a.sessionIdleTTL)) {
		audit.Log(a.logger, r, slog.LevelInfo, audit.EventSessionIdle, slog.String("email", data.Email))
		return SessionData{}, ErrSessionIdle
	}
	return data, nil
}

// clearSessionCookie returns a cookie that immediately expires the session cookie.
func (a *OIDCAuth) clearSessionCookie() *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
	}
}
