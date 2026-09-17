package auth

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func protectedProbe() (http.Handler, *string) {
	var gotEmail string
	h := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if s, ok := SessionFromContext(r.Context()); ok {
			gotEmail = s.Email
		}
	})
	return h, &gotEmail
}

func TestRequireAuthValidSession(t *testing.T) {
	a := newTestAuth()
	probe, gotEmail := protectedProbe()

	cookie, _ := a.createSessionCookie("user@example.com", "")
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	a.RequireAuth(probe).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if *gotEmail != "user@example.com" {
		t.Fatalf("context email = %q, want %q", *gotEmail, "user@example.com")
	}
}

func TestRequireAuthNoSessionRedirects(t *testing.T) {
	a := newTestAuth()
	probe, _ := protectedProbe()

	req := httptest.NewRequest(http.MethodGet, "/me?x=1", nil)
	rec := httptest.NewRecorder()

	a.RequireAuth(probe).ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	loc := rec.Header().Get("Location")
	if want := "/auth/login?next=%2Fme%3Fx%3D1"; loc != want {
		t.Fatalf("Location = %q, want %q", loc, want)
	}
}

func TestRequireAuthAPIPathReturnsJSON(t *testing.T) {
	a := newTestAuth()
	probe, _ := protectedProbe()

	req := httptest.NewRequest(http.MethodGet, "/api/shares", nil)
	rec := httptest.NewRecorder()

	a.RequireAuth(probe).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestRequireAuthClearsInvalidCookie(t *testing.T) {
	a := newTestAuth()
	probe, _ := protectedProbe()

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "garbage.value"})
	rec := httptest.NewRecorder()

	a.RequireAuth(probe).ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if !clearedSessionCookie(rec) {
		t.Fatal("expected session cookie to be cleared")
	}
}

func TestLogoutClearsSession(t *testing.T) {
	a := newTestAuth()
	req := httptest.NewRequest(http.MethodPost, LogoutPath, nil)
	rec := httptest.NewRecorder()

	a.LogoutHandler(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if rec.Header().Get("Location") != loggedOutPath {
		t.Fatalf("Location = %q, want %q", rec.Header().Get("Location"), loggedOutPath)
	}
	if !clearedSessionCookie(rec) {
		t.Fatal("expected session cookie to be cleared")
	}
}

func TestPostLogoutRedirectURI(t *testing.T) {
	got, err := postLogoutRedirectURI("https://atrium.example.test/auth/callback")
	if err != nil {
		t.Fatalf("postLogoutRedirectURI: %v", err)
	}
	if want := "https://atrium.example.test" + loggedOutPath; got != want {
		t.Fatalf("uri = %q, want %q", got, want)
	}
	if _, err := postLogoutRedirectURI("/auth/callback"); err == nil {
		t.Fatal("expected an error for a non-absolute redirect URI")
	}
}

// clearedSessionCookie reports whether the response expires the session cookie.
func clearedSessionCookie(rec *httptest.ResponseRecorder) bool {
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.MaxAge < 0 {
			return true
		}
	}
	return false
}

// issuedSession returns the session the response re-issued, if any.
func issuedSession(t *testing.T, a *OIDCAuth, rec *httptest.ResponseRecorder) (SessionData, bool) {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name != sessionCookieName || c.MaxAge < 0 {
			continue
		}
		data, err := decodeAndVerify(c.Value, a.sessionKey)
		if err != nil {
			t.Fatalf("decodeAndVerify re-issued cookie: %v", err)
		}
		return data, true
	}
	return SessionData{}, false
}

func TestRequireAuthRefreshesStaleSession(t *testing.T) {
	a := newTestAuth()
	a.sessionIdleTTL = 30 * time.Minute
	probe, _ := protectedProbe()

	rec := httptest.NewRecorder()
	a.RequireAuth(probe).ServeHTTP(rec, requestWithSession(t, a, liveSession(5*time.Minute)))

	data, ok := issuedSession(t, a, rec)
	if !ok {
		t.Fatal("expected the session cookie to be re-issued")
	}
	if time.Since(data.LastSeen) > time.Minute {
		t.Errorf("LastSeen = %s, want about now", data.LastSeen)
	}
}

func TestRequireAuthThrottlesRefresh(t *testing.T) {
	a := newTestAuth()
	a.sessionIdleTTL = 30 * time.Minute
	probe, _ := protectedProbe()

	rec := httptest.NewRecorder()
	a.RequireAuth(probe).ServeHTTP(rec, requestWithSession(t, a, liveSession(time.Second)))

	if _, ok := issuedSession(t, a, rec); ok {
		t.Error("session cookie re-issued inside the refresh interval")
	}
}

func TestRequireAuthAdvertisesDeadline(t *testing.T) {
	for _, tc := range []struct {
		name string
		idle time.Duration
		want int
	}{
		{"idle deadline wins", 30 * time.Minute, 1800},
		{"absolute expiry when idle is off", 0, 36000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestAuth()
			a.sessionIdleTTL = tc.idle
			probe, _ := protectedProbe()

			rec := httptest.NewRecorder()
			a.RequireAuth(probe).ServeHTTP(rec, requestWithSession(t, a, liveSession(5*time.Minute)))

			got, err := strconv.Atoi(rec.Header().Get(SessionExpiresHeader))
			if err != nil {
				t.Fatalf("%s = %q: %v", SessionExpiresHeader, rec.Header().Get(SessionExpiresHeader), err)
			}
			if got > tc.want || got < tc.want-5 {
				t.Errorf("%s = %d, want about %d", SessionExpiresHeader, got, tc.want)
			}
		})
	}
}

func TestRequireAuthDeniesIdleSession(t *testing.T) {
	a := newTestAuth()
	a.sessionIdleTTL = 30 * time.Minute
	probe, _ := protectedProbe()

	rec := httptest.NewRecorder()
	a.RequireAuth(probe).ServeHTTP(rec, requestWithSession(t, a, liveSession(time.Hour)))

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if !clearedSessionCookie(rec) {
		t.Fatal("expected session cookie to be cleared")
	}
}
