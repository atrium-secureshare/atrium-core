package tos

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/atrium-secureshare/atrium-core/internal/audit"
)

// newTestManager builds an enabled Manager over the given content without
// touching the filesystem. Its logger writes audit lines to the returned buffer
// (with pseudonymization on, as in production) so handler tests can inspect them.
func newTestManager(t *testing.T, content string) (*Manager, *bytes.Buffer) {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	auditBuf := &bytes.Buffer{}
	m := &Manager{
		content:       []byte(content),
		contentHash:   hex.EncodeToString(sum[:]),
		version:       "test-1",
		signingKey:    []byte("0123456789abcdef0123456789abcdef"),
		secureCookies: true,
		logger:        audit.New(auditBuf, slog.LevelInfo, nil, true),
	}
	return m, auditBuf
}

func acceptCookie(t *testing.T, m *Manager, email string) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := m.setAcceptanceCookie(rec, email, time.Now()); err != nil {
		t.Fatalf("setAcceptanceCookie: %v", err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	return cookies[0]
}

func requestWithCookie(c *http.Cookie) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if c != nil {
		r.AddCookie(c)
	}
	return r
}

// TestCheckAcceptance drives CheckAcceptance through the acceptance-cookie
// lifecycle. Each case builds the manager to check against, the request and the
// email to verify. The tampered case represents the whole "verifyCookie returns
// an error" branch (missing/malformed/bad-signature/wrong-key); the key-binding
// property itself is covered in signedcookie's own tests.
func TestCheckAcceptance(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) (m *Manager, r *http.Request, email string)
		want  bool
	}{
		{
			name: "accepting user round-trips",
			setup: func(t *testing.T) (*Manager, *http.Request, string) {
				m, _ := newTestManager(t, "terms")
				return m, requestWithCookie(acceptCookie(t, m, "user@example.com")), "user@example.com"
			},
			want: true,
		},
		{
			name: "email match is case-insensitive",
			setup: func(t *testing.T) (*Manager, *http.Request, string) {
				m, _ := newTestManager(t, "terms")
				return m, requestWithCookie(acceptCookie(t, m, "User@Example.com")), "user@example.com"
			},
			want: true,
		},
		{
			name: "different email is rejected",
			setup: func(t *testing.T) (*Manager, *http.Request, string) {
				m, _ := newTestManager(t, "terms")
				return m, requestWithCookie(acceptCookie(t, m, "user@example.com")), "other@example.com"
			},
			want: false,
		},
		{
			name: "changed ToS content invalidates consent",
			setup: func(t *testing.T) (*Manager, *http.Request, string) {
				m, _ := newTestManager(t, "terms v1")
				c := acceptCookie(t, m, "user@example.com")
				// Simulate an edited ToS: the content hash changes, so old consent is stale.
				updated, _ := newTestManager(t, "terms v2")
				updated.signingKey = m.signingKey
				return updated, requestWithCookie(c), "user@example.com"
			},
			want: false,
		},
		{
			name: "tampered cookie is rejected",
			setup: func(t *testing.T) (*Manager, *http.Request, string) {
				m, _ := newTestManager(t, "terms")
				c := acceptCookie(t, m, "user@example.com")
				c.Value = "x" + c.Value[1:] // corrupt the payload
				return m, requestWithCookie(c), "user@example.com"
			},
			want: false,
		},
		{
			name: "missing cookie is rejected",
			setup: func(t *testing.T) (*Manager, *http.Request, string) {
				m, _ := newTestManager(t, "terms")
				return m, requestWithCookie(nil), "user@example.com"
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, r, email := tt.setup(t)
			if got := m.CheckAcceptance(r, email); got != tt.want {
				t.Errorf("CheckAcceptance = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAcceptanceCookieAttributes(t *testing.T) {
	m, _ := newTestManager(t, "terms")
	c := acceptCookie(t, m, "user@example.com")

	if c.Name != acceptanceCookieName {
		t.Errorf("Name = %q, want %q", c.Name, acceptanceCookieName)
	}
	if !c.HttpOnly {
		t.Error("HttpOnly = false, want true")
	}
	if !c.Secure {
		t.Error("Secure = false, want true")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want /", c.Path)
	}
	if c.MaxAge != int(acceptanceTTL.Seconds()) {
		t.Errorf("MaxAge = %d, want %d", c.MaxAge, int(acceptanceTTL.Seconds()))
	}
}
