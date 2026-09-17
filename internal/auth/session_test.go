package auth

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestAuth() *OIDCAuth {
	return &OIDCAuth{
		sessionKey:         []byte("0123456789abcdef0123456789abcdef"),
		sessionAbsoluteTTL: time.Hour,
		secureCookies:      true,
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// requestWithSession builds a request carrying data as its session cookie, so a
// test can place LastSeen wherever it needs it.
func requestWithSession(t *testing.T, a *OIDCAuth, data SessionData) *http.Request {
	t.Helper()
	cookie, err := a.sessionCookie(data)
	if err != nil {
		t.Fatalf("sessionCookie: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(cookie)
	return r
}

// liveSession is a session well inside its absolute lifetime, with LastSeen ago.
func liveSession(ago time.Duration) SessionData {
	now := time.Now()
	return SessionData{
		Email:     "user@example.com",
		IssuedAt:  now.Add(-2 * time.Hour),
		ExpiresAt: now.Add(10 * time.Hour),
		LastSeen:  now.Add(-ago),
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
	if cookie.MaxAge != int(a.sessionAbsoluteTTL.Seconds()) {
		t.Errorf("MaxAge = %d, want %d", cookie.MaxAge, int(a.sessionAbsoluteTTL.Seconds()))
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

func TestSessionIdleExpiry(t *testing.T) {
	a := newTestAuth()
	a.sessionIdleTTL = 30 * time.Minute

	if _, err := a.validateSessionCookie(requestWithSession(t, a, liveSession(time.Minute))); err != nil {
		t.Fatalf("recent session rejected: %v", err)
	}
	if _, err := a.validateSessionCookie(requestWithSession(t, a, liveSession(time.Hour))); err != ErrSessionIdle {
		t.Fatalf("err = %v, want ErrSessionIdle", err)
	}
}

func TestSessionIdleDisabled(t *testing.T) {
	a := newTestAuth()
	if _, err := a.validateSessionCookie(requestWithSession(t, a, liveSession(48*time.Hour))); err != nil {
		t.Fatalf("idle expiry must not apply when SESSION_IDLE_TTL is 0: %v", err)
	}
}

// A cookie signed before LastSeen existed must not be treated as idle forever.
func TestSessionIdleFallsBackToIssuedAt(t *testing.T) {
	a := newTestAuth()
	a.sessionIdleTTL = 30 * time.Minute

	fresh := liveSession(0)
	fresh.IssuedAt = time.Now().Add(-time.Minute)
	fresh.LastSeen = time.Time{}
	if _, err := a.validateSessionCookie(requestWithSession(t, a, fresh)); err != nil {
		t.Fatalf("legacy cookie rejected: %v", err)
	}

	stale := fresh
	stale.IssuedAt = time.Now().Add(-time.Hour)
	if _, err := a.validateSessionCookie(requestWithSession(t, a, stale)); err != ErrSessionIdle {
		t.Fatalf("err = %v, want ErrSessionIdle", err)
	}
}

func TestSessionDeadlineTakesTheEarlier(t *testing.T) {
	a := newTestAuth()
	data := liveSession(0)

	if got := a.sessionDeadline(data); !got.Equal(data.ExpiresAt) {
		t.Errorf("deadline = %s, want the absolute expiry when idle is off", got)
	}

	a.sessionIdleTTL = 30 * time.Minute
	if got, want := a.sessionDeadline(data), data.LastSeen.Add(30*time.Minute); !got.Equal(want) {
		t.Errorf("deadline = %s, want the idle deadline %s", got, want)
	}

	// Near the absolute expiry the idle window reaches past it and must not win.
	data.ExpiresAt = time.Now().Add(time.Minute)
	if got := a.sessionDeadline(data); !got.Equal(data.ExpiresAt) {
		t.Errorf("deadline = %s, want the absolute expiry %s", got, data.ExpiresAt)
	}
}

func TestSessionCookieMaxAgeTracksIdle(t *testing.T) {
	a := newTestAuth()
	a.sessionIdleTTL = 15 * time.Minute

	cookie, err := a.createSessionCookie("user@example.com", "")
	if err != nil {
		t.Fatalf("createSessionCookie: %v", err)
	}
	if want := int(a.sessionIdleTTL.Seconds()); cookie.MaxAge > want || cookie.MaxAge < want-5 {
		t.Errorf("MaxAge = %d, want about %d", cookie.MaxAge, want)
	}
}
