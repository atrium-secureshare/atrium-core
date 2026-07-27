package tos

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/atrium-secureshare/atrium-core/internal/audit"
	"github.com/atrium-secureshare/atrium-core/internal/auth"
)

func authed(r *http.Request, email string) *http.Request {
	return r.WithContext(auth.NewContext(r.Context(), auth.SessionData{Email: email}))
}

func newHandlerRequest(method, path, email string) (*httptest.ResponseRecorder, *http.Request) {
	return httptest.NewRecorder(), authed(httptest.NewRequest(method, path, nil), email)
}

func acceptanceCookieFrom(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == acceptanceCookieName {
			return c
		}
	}
	return nil
}

func probe() (http.Handler, *bool) {
	called := false
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}), &called
}

func TestRequireTOSAllowsAccepted(t *testing.T) {
	m, _ := newTestManager(t, "terms")
	next, called := probe()

	req := authed(requestWithCookie(acceptCookie(t, m, "user@example.com")), "user@example.com")
	rec := httptest.NewRecorder()
	m.RequireTOS(next).ServeHTTP(rec, req)

	if !*called {
		t.Error("next was not called for an accepted user")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireTOSBlocksWithoutAcceptance(t *testing.T) {
	m, _ := newTestManager(t, "terms")
	next, called := probe()

	req := authed(httptest.NewRequest(http.MethodGet, "/api/me", nil), "user@example.com")
	rec := httptest.NewRecorder()
	m.RequireTOS(next).ServeHTTP(rec, req)

	if *called {
		t.Fatal("next was called although the ToS was not accepted")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (%q)", err, rec.Body.String())
	}
	if body["error"] != "tos_required" {
		t.Errorf("error = %q, want tos_required", body["error"])
	}
}

func TestRequireTOSDisabledPassthrough(t *testing.T) {
	var m *Manager // disabled
	next, called := probe()

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()
	m.RequireTOS(next).ServeHTTP(rec, req)

	if !*called {
		t.Error("next was not called with the feature disabled")
	}
}

// TestHandlersWithoutSession covers the two pre-consent handler paths. Both run
// after the auth middleware, so a missing session in context is an internal
// error (500) rather than a request they should act on.
func TestHandlersWithoutSession(t *testing.T) {
	tests := []struct {
		name    string
		handler func(m *Manager) http.Handler
	}{
		{
			name: "RequireTOS",
			handler: func(m *Manager) http.Handler {
				next, _ := probe()
				return m.RequireTOS(next)
			},
		},
		{
			name: "AcceptHandler",
			handler: func(m *Manager) http.Handler {
				return http.HandlerFunc(m.AcceptHandler)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newTestManager(t, "terms")
			req := httptest.NewRequest(http.MethodGet, "/api/me", nil) // no session in context
			rec := httptest.NewRecorder()
			tt.handler(m).ServeHTTP(rec, req)

			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want %d without a session", rec.Code, http.StatusInternalServerError)
			}
		})
	}
}

func TestContentHandlerReturnsDocument(t *testing.T) {
	m, _ := newTestManager(t, "# Terms\n\nBe nice.")

	rec, req := newHandlerRequest(http.MethodGet, ContentPath, "user@example.com")
	m.ContentHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body["version"] != m.version {
		t.Errorf("version = %q, want %q", body["version"], m.version)
	}
	if body["content"] != "# Terms\n\nBe nice." {
		t.Errorf("content = %q, want the ToS document", body["content"])
	}
}

func TestAcceptHandlerSetsValidCookie(t *testing.T) {
	m, _ := newTestManager(t, "terms")
	const email = "user@example.com"

	rec, req := newHandlerRequest(http.MethodPost, AcceptPath, email)
	m.AcceptHandler(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	set := acceptanceCookieFrom(rec)
	if set == nil {
		t.Fatal("no acceptance cookie set")
	}
	if !m.CheckAcceptance(requestWithCookie(set), email) {
		t.Error("cookie set by AcceptHandler does not validate")
	}
}

func TestAcceptHandlerAuditsAcceptance(t *testing.T) {
	m, auditBuf := newTestManager(t, "terms")

	rec, req := newHandlerRequest(http.MethodPost, AcceptPath, "user@example.com")
	m.AcceptHandler(rec, req)

	// Email pseudonymization is enforced centrally (see internal/audit); here we
	// only assert that AcceptHandler emits the acceptance audit event with the
	// ToS version attached.
	var ev map[string]any
	if err := json.Unmarshal(auditBuf.Bytes(), &ev); err != nil {
		t.Fatalf("audit line is not valid JSON: %v (%q)", err, auditBuf.String())
	}
	if ev["level"] != "AUDIT" || ev["msg"] != audit.EventAcceptTOS {
		t.Errorf("level/msg = %v/%v, want AUDIT/%s", ev["level"], ev["msg"], audit.EventAcceptTOS)
	}
	if ev["tos_version"] != m.version {
		t.Errorf("tos_version = %v, want %q", ev["tos_version"], m.version)
	}
}
