package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atrium-secureshare/atrium-core/internal/api"
	"github.com/atrium-secureshare/atrium-core/internal/audit"
	"github.com/atrium-secureshare/atrium-core/internal/auth"
	"github.com/atrium-secureshare/atrium-core/internal/authtest"
	"github.com/atrium-secureshare/atrium-core/internal/provider"
	"github.com/atrium-secureshare/atrium-core/internal/proxy"
	"github.com/atrium-secureshare/atrium-core/internal/tos"
	"github.com/atrium-secureshare/atrium-core/internal/webui"
)

func testLogger() *slog.Logger {
	return audit.New(io.Discard, slog.LevelInfo, nil, true)
}

type stubTrust struct {
	err       error
	shares    []provider.Share
	listErr   error
	listCalls *int
	folder    *provider.FolderListing
	folderErr error
	gotPath   *string
}

func (s stubTrust) HealthCheck(context.Context) error { return s.err }

func (s stubTrust) ListShares(context.Context, string) ([]provider.Share, error) {
	if s.listCalls != nil {
		*s.listCalls++
	}
	return s.shares, s.listErr
}

func (s stubTrust) ListFolder(_ context.Context, _, _, path string) (*provider.FolderListing, error) {
	if s.gotPath != nil {
		*s.gotPath = path
	}
	return s.folder, s.folderErr
}

var errStub = errors.New("provider unreachable")

type stubProvider struct{}

func (stubProvider) Download(context.Context, string, string) (*provider.Content, error) {
	return nil, provider.ErrShareNotFound
}

func (stubProvider) DownloadFolderFile(context.Context, string, string, string) (*provider.Content, error) {
	return nil, provider.ErrShareNotFound
}

func (stubProvider) Upload(context.Context, string, string, string, string, string, io.Reader) error {
	return provider.ErrShareNotFound
}

func testProxy() *proxy.Proxy {
	return proxy.New(stubProvider{}, 1<<20, testLogger())
}

func newHandlerWithProvider(t *testing.T, svc api.ProviderService) (http.Handler, *authtest.Provider, *auth.OIDCAuth) {
	t.Helper()
	p := authtest.NewProvider(t)
	a := p.Auth(t)
	return api.Handler(a, nil, svc, testProxy(), "", webui.Brand{}, false, testLogger()), p, a
}

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	h, _, _ := newHandlerWithProvider(t, stubTrust{})
	return h
}

func authedGET(t *testing.T, h http.Handler, p *authtest.Provider, a *auth.OIDCAuth, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.AddCookie(p.SessionCookie(t, a, "user@example.com"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
}

const tosDocument = "# Terms\n\nPLEASE-READ-THESE-TERMS"

func newTOSHandler(t *testing.T) (http.Handler, *authtest.Provider, *auth.OIDCAuth) {
	t.Helper()
	return newTOSHandlerWithProvider(t, stubTrust{})
}

func newTOSHandlerWithProvider(t *testing.T, svc api.ProviderService) (http.Handler, *authtest.Provider, *auth.OIDCAuth) {
	t.Helper()
	p := authtest.NewProvider(t)
	a := p.Auth(t)
	path := filepath.Join(t.TempDir(), "tos.md")
	if err := os.WriteFile(path, []byte(tosDocument), 0o600); err != nil {
		t.Fatalf("write tos: %v", err)
	}
	mgr, err := tos.NewManager(path, "v1", []byte("0123456789abcdef0123456789abcdef"), false, testLogger())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return api.Handler(a, mgr, svc, testProxy(), "", webui.Brand{}, false, testLogger()), p, a
}

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	newHandler(t).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "ok\n" {
		t.Fatalf("body = %q, want %q", got, "ok\n")
	}
}

func TestReadyzReflectsProviderTrust(t *testing.T) {
	p := authtest.NewProvider(t)
	for name, tc := range map[string]struct {
		trust      stubTrust
		wantStatus int
	}{
		"trust ok":        {stubTrust{}, http.StatusOK},
		"trust unhealthy": {stubTrust{err: errStub}, http.StatusServiceUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			h := api.Handler(p.Auth(t), nil, tc.trust, testProxy(), "", webui.Brand{}, false, testLogger())
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

func TestAPIGateRejectsUnauthenticated(t *testing.T) {
	// The /api/ subtree is gated deny-by-default: any path under it - a registered
	// route or an unknown one - answers 401 JSON, which the SPA turns into a login
	// redirect. The unregistered case proves a route added under /api/ cannot be
	// reached unauthenticated by accident.
	h := newHandler(t)
	for name, target := range map[string]string{
		"registered route":   "/api/me",
		"unregistered route": "/api/does-not-exist",
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Fatalf("content-type = %q, want JSON", ct)
			}
		})
	}
}

func TestTOSAcceptRequiresAuth(t *testing.T) {
	h, _, _ := newTOSHandler(t)
	req := httptest.NewRequest(http.MethodPost, tos.AcceptPath, nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestGateBlocksFileListingBeforeAcceptance(t *testing.T) {
	// The requested guarantee: after a successful login but before ToS
	// acceptance, no API request is possible - listing files included. A valid
	// session hits the consent gate first, which answers 403 tos_required, so the
	// share listing never reaches the provider (ListShares is never called). This
	// also covers the plain "valid session is not sufficient" case, since the gate
	// is one middleware around the whole apiMux, not per-route behaviour.
	calls := 0
	svc := stubTrust{
		shares:    []provider.Share{{ID: "tok1", RecipientEmail: "user@example.com", DisplayName: "report.pdf"}},
		listCalls: &calls,
	}
	h, p, a := newTOSHandlerWithProvider(t, svc)

	rec := authedGET(t, h, p, a, "/api/shares")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	var body map[string]string
	decodeJSON(t, rec, &body)
	if body["error"] != "tos_required" {
		t.Errorf("error = %q, want tos_required", body["error"])
	}
	// The crux of "no API request possible": the backend was never touched.
	if calls != 0 {
		t.Errorf("ListShares called %d times before ToS acceptance, want 0", calls)
	}
}

func TestTOSContentAvailableBeforeAcceptance(t *testing.T) {
	// The consent overlay needs the document, so GET /api/tos is reachable with
	// a session but without prior acceptance.
	h, p, a := newTOSHandler(t)

	rec := authedGET(t, h, p, a, tos.ContentPath)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body map[string]string
	decodeJSON(t, rec, &body)
	if body["content"] != tosDocument {
		t.Errorf("content = %q, want the ToS document", body["content"])
	}
	if body["version"] != "v1" {
		t.Errorf("version = %q, want v1", body["version"])
	}
}

func TestGatePassesAfterAcceptance(t *testing.T) {
	h, p, a := newTOSHandler(t)
	sess := p.SessionCookie(t, a, "user@example.com")

	accept := httptest.NewRequest(http.MethodPost, tos.AcceptPath, nil)
	accept.AddCookie(sess)
	accRec := httptest.NewRecorder()
	h.ServeHTTP(accRec, accept)
	if accRec.Code != http.StatusNoContent {
		t.Fatalf("accept status = %d, want %d", accRec.Code, http.StatusNoContent)
	}
	var tosCookie *http.Cookie
	for _, c := range accRec.Result().Cookies() {
		if c.Name == "tos_accepted" {
			tosCookie = c
		}
	}
	if tosCookie == nil {
		t.Fatal("no acceptance cookie set")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(sess)
	req.AddCookie(tosCookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"email"`) {
		t.Errorf("body = %q, want identity JSON", rec.Body.String())
	}
}

func TestListSharesProjectsRecipientView(t *testing.T) {
	// The listing endpoint returns the recipient's shares as the frontend
	// contract: a stable subset that never echoes the recipient's own email.
	max := 10
	svc := stubTrust{shares: []provider.Share{
		{ID: "tok1", RecipientEmail: "user@example.com", DisplayName: "report.pdf", DownloadCount: 3, MaxDownloads: &max},
		{ID: "tok2", RecipientEmail: "user@example.com", FileName: "Inbox", IsFolder: true, Mode: 2},
	}}
	h, p, a := newHandlerWithProvider(t, svc)

	rec := authedGET(t, h, p, a, "/api/shares")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if strings.Contains(rec.Body.String(), "recipient") || strings.Contains(rec.Body.String(), "example.com") {
		t.Fatalf("response leaks recipient identity: %s", rec.Body.String())
	}
	var views []struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		IsFolder     bool   `json:"isFolder"`
		Mode         int    `json:"mode"`
		MaxDownloads *int   `json:"maxDownloads"`
	}
	decodeJSON(t, rec, &views)
	if len(views) != 2 {
		t.Fatalf("got %d shares, want 2", len(views))
	}
	if views[0].Name != "report.pdf" || views[0].MaxDownloads == nil || *views[0].MaxDownloads != 10 {
		t.Errorf("file view = %+v, want name report.pdf and max 10", views[0])
	}
	// The mode (0-3) reaches the frontend as `mode`, not a permission bitmask.
	if views[1].Name != "Inbox" || !views[1].IsFolder || views[1].Mode != 2 {
		t.Errorf("folder view = %+v, want folder Inbox mode 2", views[1])
	}
}

func TestListFolderProjectsEntries(t *testing.T) {
	// The folder endpoint forwards the provider's already mode-filtered listing as
	// the frontend's camelCase contract, and forwards the sub-path query verbatim.
	var size int64 = 512
	var gotPath string
	svc := stubTrust{gotPath: &gotPath, folder: &provider.FolderListing{Entries: []provider.FolderEntry{
		{ID: "10", Name: "owner.txt", Size: &size, MimeType: "text/plain", IsFolder: false, IsOwn: false},
		{ID: "11", Name: "mine.pdf", IsOwn: true},
	}}}
	h, p, a := newHandlerWithProvider(t, svc)

	rec := authedGET(t, h, p, a, "/api/shares/tok1/folder?path=docs%2Fsub")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotPath != "docs/sub" {
		t.Errorf("forwarded path = %q, want docs/sub", gotPath)
	}
	var listing struct {
		IsFile  bool `json:"isFile"`
		Entries []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			IsOwn bool   `json:"isOwn"`
		} `json:"entries"`
	}
	decodeJSON(t, rec, &listing)
	if listing.IsFile {
		t.Errorf("isFile = true, want a folder listing")
	}
	if len(listing.Entries) != 2 || listing.Entries[0].Name != "owner.txt" || !listing.Entries[1].IsOwn {
		t.Fatalf("entries = %+v, want owner.txt then own mine.pdf", listing.Entries)
	}
}

func TestListFolderReturnsFileEntryForFilePath(t *testing.T) {
	// A path that resolves to a file comes back as {isFile:true, entry:{…}} so the
	// frontend can open a file deep-link without a second lookup.
	var size int64 = 42
	svc := stubTrust{folder: &provider.FolderListing{
		IsFile: true,
		Entry:  &provider.FolderEntry{ID: "99", Name: "deep.pdf", Size: &size, MimeType: "application/pdf"},
	}}
	h, p, a := newHandlerWithProvider(t, svc)

	rec := authedGET(t, h, p, a, "/api/shares/tok1/folder?path=a%2Fdeep.pdf")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var listing struct {
		IsFile bool `json:"isFile"`
		Entry  *struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"entry"`
	}
	decodeJSON(t, rec, &listing)
	if !listing.IsFile || listing.Entry == nil || listing.Entry.Name != "deep.pdf" || listing.Entry.ID != "99" {
		t.Fatalf("listing = %+v, want isFile with entry deep.pdf/99", listing)
	}
}

func TestShareEndpointErrorsMapToStatus(t *testing.T) {
	// Each provider error maps to a recipient-facing status without leaking the
	// backend. A folder is share-scoped, so writeShareError distinguishes missing
	// (404), forbidden (403) and gone (410) on top of the flat listing's 502/503,
	// letting the recipient UI tell the causes apart. The flat listing maps only
	// an unexpected status (502) and everything else (503). An error not in the
	// switch falls through to the 503 default (here errStub).
	for name, tc := range map[string]struct {
		target     string
		svc        stubTrust
		wantStatus int
	}{
		"folder not found":    {"/api/shares/tok1/folder", stubTrust{folderErr: provider.ErrShareNotFound}, http.StatusNotFound},
		"folder forbidden":    {"/api/shares/tok1/folder", stubTrust{folderErr: provider.ErrForbidden}, http.StatusForbidden},
		"folder gone":         {"/api/shares/tok1/folder", stubTrust{folderErr: provider.ErrShareGone}, http.StatusGone},
		"folder bad gateway":  {"/api/shares/tok1/folder", stubTrust{folderErr: provider.ErrUnexpectedStatus}, http.StatusBadGateway},
		"folder unavailable":  {"/api/shares/tok1/folder", stubTrust{folderErr: errStub}, http.StatusServiceUnavailable},
		"listing bad gateway": {"/api/shares", stubTrust{listErr: provider.ErrUnexpectedStatus}, http.StatusBadGateway},
		"listing unavailable": {"/api/shares", stubTrust{listErr: errStub}, http.StatusServiceUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			h, p, a := newHandlerWithProvider(t, tc.svc)
			rec := authedGET(t, h, p, a, tc.target)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

// TestLogout covers both logout paths. Both answer 302 and clear the session
// cookie; they differ only in the redirect target. Without a session there is
// nothing to end at the provider, so it falls back to the login path. With one,
// logout is RP-initiated against the provider's end_session_endpoint, carrying
// id_token_hint and post_logout_redirect_uri so the SSO session ends too, not
// just the local cookie.
func TestLogout(t *testing.T) {
	for name, tc := range map[string]struct {
		withSession  bool
		wantLocation func(t *testing.T, p *authtest.Provider, loc string)
	}{
		"without session falls back to login": {
			withSession: false,
			wantLocation: func(t *testing.T, _ *authtest.Provider, loc string) {
				if loc != auth.LoginPath {
					t.Fatalf("Location = %q, want %q", loc, auth.LoginPath)
				}
			},
		},
		"with session is rp-initiated": {
			withSession: true,
			wantLocation: func(t *testing.T, p *authtest.Provider, loc string) {
				u, err := url.Parse(loc)
				if err != nil {
					t.Fatalf("parse Location: %v", err)
				}
				if want := p.Issuer + "/logout"; u.Scheme+"://"+u.Host+u.Path != want {
					t.Fatalf("logout endpoint = %q, want %q", u.Scheme+"://"+u.Host+u.Path, want)
				}
				q := u.Query()
				if q.Get("id_token_hint") == "" {
					t.Error("id_token_hint missing from logout redirect")
				}
				if got := q.Get("post_logout_redirect_uri"); got != "http://localhost:8080/" {
					t.Errorf("post_logout_redirect_uri = %q, want %q", got, "http://localhost:8080/")
				}
				if got := q.Get("client_id"); got != authtest.ClientID {
					t.Errorf("client_id = %q, want %q", got, authtest.ClientID)
				}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			h, p, a := newHandlerWithProvider(t, stubTrust{})
			req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
			if tc.withSession {
				req.AddCookie(p.SessionCookie(t, a, "user@example.com"))
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			// Shared contract on both paths: redirect + the stale cookie cleared.
			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
			}
			if !hasClearedSessionCookie(rec.Result().Cookies()) {
				t.Fatal("logout did not clear the session cookie")
			}
			tc.wantLocation(t, p, rec.Header().Get("Location"))
		})
	}
}

func hasClearedSessionCookie(cookies []*http.Cookie) bool {
	for _, c := range cookies {
		if c.Name == "atrium_session" && c.MaxAge < 0 {
			return true
		}
	}
	return false
}

// TestSecurityHeaders verifies the security-header middleware wraps every
// response: the static hardening headers and a locked-down CSP are present.
// /healthz is a cheap public route that still passes through the same wrapper.
// The transport-dependent HSTS header has its own test.
func TestSecurityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	newHandler(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	got := rec.Result().Header

	for _, c := range []struct{ key, want string }{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
		{"Referrer-Policy", "no-referrer"},
	} {
		if v := got.Get(c.key); v != c.want {
			t.Errorf("%s = %q, want %q", c.key, v, c.want)
		}
	}

	csp := got.Get("Content-Security-Policy")
	for _, want := range []string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		"object-src 'none'",
		"base-uri 'none'",
		"frame-ancestors 'none'",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q missing %q", csp, want)
		}
	}
}

// TestSecurityHeadersHSTS verifies Strict-Transport-Security is emitted exactly
// when the deployment is served over TLS: absent on plain http (a browser must
// never be told to force HTTPS for a local run) and, over TLS, a one-year
// includeSubDomains policy.
func TestSecurityHeadersHSTS(t *testing.T) {
	for name, tc := range map[string]struct {
		secureTransport bool
		wantHSTS        bool
	}{
		"plain http omits hsts": {secureTransport: false, wantHSTS: false},
		"tls emits hsts":        {secureTransport: true, wantHSTS: true},
	} {
		t.Run(name, func(t *testing.T) {
			p := authtest.NewProvider(t)
			h := api.Handler(p.Auth(t), nil, stubTrust{}, testProxy(), "", webui.Brand{}, tc.secureTransport, testLogger())
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

			hsts := rec.Result().Header.Get("Strict-Transport-Security")
			switch {
			case tc.wantHSTS && (!strings.Contains(hsts, "max-age=31536000") || !strings.Contains(hsts, "includeSubDomains")):
				t.Fatalf("Strict-Transport-Security = %q, want long max-age with includeSubDomains", hsts)
			case !tc.wantHSTS && hsts != "":
				t.Errorf("HSTS must be absent without TLS, got %q", hsts)
			}
		})
	}
}
