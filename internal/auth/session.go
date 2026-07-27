package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/atrium-secureshare/atrium-core/internal/signedcookie"
)

const sessionCookieName = "atrium_session"

// Session errors returned by session validation.
var (
	// ErrNoSession indicates that no session cookie was present.
	ErrNoSession = errors.New("no session cookie")
	// ErrInvalidSession indicates a malformed or tampered session cookie.
	ErrInvalidSession = errors.New("invalid session")
	// ErrSessionExpired indicates a session whose absolute lifetime elapsed.
	ErrSessionExpired = errors.New("session expired")
)

// SessionData is the payload in the signed session cookie. The core is stateless:
// everything needed to authorize a request lives in the cookie, HMAC-signed.
type SessionData struct {
	// Email is the verified OIDC email of the authenticated recipient.
	Email string `json:"email"`
	// IssuedAt is when the session was created.
	IssuedAt time.Time `json:"iat"`
	// ExpiresAt is the absolute session expiry.
	ExpiresAt time.Time `json:"exp"`
	// IDToken is kept solely to pass as id_token_hint on RP-initiated logout, so
	// the provider ends its own session without a prompt.
	IDToken string `json:"idt,omitempty"`
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
// email and raw ID token, valid for the configured session TTL.
func (a *OIDCAuth) createSessionCookie(email, idToken string) (*http.Cookie, error) {
	now := time.Now()
	token, err := signAndEncode(SessionData{
		Email:     email,
		IssuedAt:  now,
		ExpiresAt: now.Add(a.sessionTTL),
		IDToken:   idToken,
	}, a.sessionKey)
	if err != nil {
		return nil, err
	}
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(a.sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
	}, nil
}

// validateSessionCookie reads and verifies the session cookie from the request.
func (a *OIDCAuth) validateSessionCookie(r *http.Request) (SessionData, error) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return SessionData{}, ErrNoSession
	}
	return decodeAndVerify(c.Value, a.sessionKey)
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
