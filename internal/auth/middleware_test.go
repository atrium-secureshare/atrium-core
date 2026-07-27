package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
	if rec.Header().Get("Location") != LoginPath {
		t.Fatalf("Location = %q, want %q", rec.Header().Get("Location"), LoginPath)
	}
	if !clearedSessionCookie(rec) {
		t.Fatal("expected session cookie to be cleared")
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
