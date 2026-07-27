package auth

import (
	"net/http"
	"testing"
	"time"
)

func newTestAuth() *OIDCAuth {
	return &OIDCAuth{
		sessionKey:    []byte("0123456789abcdef0123456789abcdef"),
		sessionTTL:    time.Hour,
		secureCookies: true,
	}
}

func TestSessionRoundTrip(t *testing.T) {
	a := newTestAuth()
	cookie, err := a.createSessionCookie("User@Example.com", "raw-id-token")
	if err != nil {
		t.Fatalf("createSessionCookie: %v", err)
	}

	data, err := decodeAndVerify(cookie.Value, a.sessionKey)
	if err != nil {
		t.Fatalf("decodeAndVerify: %v", err)
	}
	if data.Email != "User@Example.com" {
		t.Fatalf("email = %q, want %q", data.Email, "User@Example.com")
	}
	if data.IDToken != "raw-id-token" {
		t.Fatalf("idToken = %q, want %q", data.IDToken, "raw-id-token")
	}
	if data.ExpiresAt.Before(time.Now()) {
		t.Fatal("session already expired")
	}
}

func TestSessionTamperedPayload(t *testing.T) {
	a := newTestAuth()
	cookie, _ := a.createSessionCookie("user@example.com", "")

	tampered := []byte(cookie.Value)
	tampered[0] ^= 0xFF
	if _, err := decodeAndVerify(string(tampered), a.sessionKey); err != ErrInvalidSession {
		t.Fatalf("err = %v, want ErrInvalidSession", err)
	}
}

func TestSessionWrongKey(t *testing.T) {
	a := newTestAuth()
	cookie, _ := a.createSessionCookie("user@example.com", "")

	other := []byte("ffffffffffffffffffffffffffffffff")
	if _, err := decodeAndVerify(cookie.Value, other); err != ErrInvalidSession {
		t.Fatalf("err = %v, want ErrInvalidSession", err)
	}
}

func TestSessionExpired(t *testing.T) {
	a := newTestAuth()
	token, err := signAndEncode(SessionData{
		Email:     "user@example.com",
		IssuedAt:  time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-time.Hour),
	}, a.sessionKey)
	if err != nil {
		t.Fatalf("signAndEncode: %v", err)
	}
	if _, err := decodeAndVerify(token, a.sessionKey); err != ErrSessionExpired {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
}

func TestSessionMalformed(t *testing.T) {
	a := newTestAuth()
	for _, tok := range []string{"", "no-separator", "not-base64.also-not", "."} {
		if _, err := decodeAndVerify(tok, a.sessionKey); err != ErrInvalidSession {
			t.Errorf("token %q: err = %v, want ErrInvalidSession", tok, err)
		}
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	a := newTestAuth()
	cookie, _ := a.createSessionCookie("user@example.com", "")

	if cookie.Name != sessionCookieName {
		t.Errorf("name = %q, want %q", cookie.Name, sessionCookieName)
	}
	if !cookie.HttpOnly {
		t.Error("cookie must be HttpOnly")
	}
	if !cookie.Secure {
		t.Error("cookie must be Secure when secureCookies is set")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Errorf("path = %q, want /", cookie.Path)
	}
	if cookie.MaxAge != int(a.sessionTTL.Seconds()) {
		t.Errorf("MaxAge = %d, want %d", cookie.MaxAge, int(a.sessionTTL.Seconds()))
	}
}

func TestClearSessionCookie(t *testing.T) {
	a := newTestAuth()
	c := a.clearSessionCookie()
	if c.MaxAge != -1 {
		t.Errorf("MaxAge = %d, want -1", c.MaxAge)
	}
	if c.Value != "" {
		t.Errorf("value = %q, want empty", c.Value)
	}
}
