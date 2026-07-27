package provider

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func newTestClient(t *testing.T, baseURL string) *client {
	t.Helper()
	key := newP256(t)
	c, err := newClient(baseURL, sec1PEM(t, key), nextcloudAudience, 30*time.Second, discardLogger())
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	return c
}

func (c *client) parseToken(t *testing.T, token string) (jose.Header, map[string]any) {
	t.Helper()
	tok, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	pub := parsePublicKeyPEM(t, c.publicKeyPEM)
	var claims map[string]any
	if err := tok.Claims(pub, &claims); err != nil {
		t.Fatalf("verify claims: %v", err)
	}
	return tok.Headers[0], claims
}

func parsePublicKeyPEM(t *testing.T, pemStr string) *ecdsa.PublicKey {
	t.Helper()
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		t.Fatal("public key PEM does not decode")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse public key: %v", err)
	}
	ec, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("public key is %T, want *ecdsa.PublicKey", pub)
	}
	return ec
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewNextcloudRejectsBadInput(t *testing.T) {
	key := newP256(t)
	t.Run("bad url", func(t *testing.T) {
		if _, err := NewNextcloud("not-absolute", sec1PEM(t, key), 30*time.Second, discardLogger()); err == nil {
			t.Error("expected error for non-absolute base URL")
		}
	})
	t.Run("bad key", func(t *testing.T) {
		if _, err := NewNextcloud("https://x.example", "garbage", 30*time.Second, discardLogger()); err == nil {
			t.Error("expected error for invalid key")
		}
	})
}

// TestNextcloudAudience pins the aud claim NewNextcloud mints — the trust identity
// the Nextcloud plugin's validator requires. It constructs through the public
// backend constructor and replaces the former constant assertion, now that aud is
// a per-backend field.
func TestNextcloudAudience(t *testing.T) {
	key := newP256(t)
	svc, err := NewNextcloud("https://provider.example", sec1PEM(t, key), 30*time.Second, discardLogger())
	if err != nil {
		t.Fatalf("NewNextcloud: %v", err)
	}
	c := svc.(*client)
	token, err := c.createToken("s1", "a@b.c", "download", ClientMeta{})
	if err != nil {
		t.Fatalf("createToken: %v", err)
	}
	if _, claims := c.parseToken(t, token); claims["aud"] != "atrium-plugin-nextcloud" {
		t.Errorf("aud = %v, want atrium-plugin-nextcloud", claims["aud"])
	}
}

func TestCreateTokenContract(t *testing.T) {
	c := newTestClient(t, "https://provider.example")
	token, err := c.createToken("share-1", "User@Example.com", "download", ClientMeta{IP: "203.0.113.5", XFF: "203.0.113.5, 10.0.0.1"})
	if err != nil {
		t.Fatalf("createToken: %v", err)
	}
	header, claims := c.parseToken(t, token)

	if header.Algorithm != string(jose.ES256) {
		t.Errorf("alg = %q, want ES256", header.Algorithm)
	}
	for _, k := range []string{"iss", "aud", "iat", "exp", "action", "share_id", "email", "ip", "xff"} {
		if _, ok := claims[k]; !ok {
			t.Errorf("claim %q missing", k)
		}
	}
	if claims["iss"] != tokenIssuer || claims["aud"] != c.audience {
		t.Errorf("iss/aud = %v/%v, want %s/%s", claims["iss"], claims["aud"], tokenIssuer, c.audience)
	}
	// exp - iat must never exceed the 30s TTL.
	iat, exp := claims["iat"].(float64), claims["exp"].(float64)
	if d := exp - iat; d != tokenTTL.Seconds() {
		t.Errorf("exp-iat = %vs, want %vs", d, tokenTTL.Seconds())
	}
}

func TestCreateTokenOmitsEmptyClaims(t *testing.T) {
	c := newTestClient(t, "https://provider.example")
	// list-shares carries no share_id; healthcheck carries neither share_id nor email.
	token, err := c.createToken("", "user@example.com", "list-shares", ClientMeta{})
	if err != nil {
		t.Fatalf("createToken: %v", err)
	}
	_, claims := c.parseToken(t, token)
	for _, absent := range []string{"share_id", "ip", "xff"} {
		if _, ok := claims[absent]; ok {
			t.Errorf("claim %q should be omitted when empty", absent)
		}
	}
	if _, ok := claims["email"]; !ok {
		t.Error("email should be present")
	}
}

// assertBearerAction verifies the request carries a parseable ES256 bearer token
// whose action claim matches wantAction. UnsafeClaimsWithoutVerification is fine
// here: the stub only inspects the action, it does not trust the token.
func assertBearerAction(t *testing.T, r *http.Request, wantAction string) {
	t.Helper()
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, "Bearer ") {
		t.Errorf("missing bearer token: %q", authz)
		return
	}
	tok, err := jwt.ParseSigned(strings.TrimPrefix(authz, "Bearer "), []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Errorf("token not ES256/parseable: %v", err)
		return
	}
	var claims map[string]any
	_ = tok.UnsafeClaimsWithoutVerification(&claims)
	if claims["action"] != wantAction {
		t.Errorf("action = %v, want %s", claims["action"], wantAction)
	}
}

func providerStub(t *testing.T, wantAction string, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBearerAction(t, r, wantAction)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestMapErrorStatus is the single, white-box test of the status → sentinel
// choke-point every Client method funnels non-2xx responses through. The
// per-method tests below no longer re-assert this table; they only keep each
// method's real HTTP error path (and its action claim) exercised.
func TestMapErrorStatus(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		wantErr   error
		wantInMsg string // when set, the message must embed this (e.g. the raw code)
	}{
		{"forbidden", http.StatusForbidden, ErrForbidden, ""},
		{"not found", http.StatusNotFound, ErrShareNotFound, ""},
		{"gone", http.StatusGone, ErrShareGone, ""},
		{"unexpected embeds code", http.StatusInternalServerError, ErrUnexpectedStatus, "500"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := mapErrorStatus(tc.status)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("mapErrorStatus(%d) = %v, want %v", tc.status, err, tc.wantErr)
			}
			if tc.wantInMsg != "" && !strings.Contains(err.Error(), tc.wantInMsg) {
				t.Errorf("message %q does not embed %q", err.Error(), tc.wantInMsg)
			}
		})
	}
}

// TestListSharesStatusMapping keeps ListShares' 200 JSON-decode path under test
// (the only place it is covered) and, via providerStub, the "list-shares" action
// claim. The error-status mapping is covered centrally by TestMapErrorStatus.
func TestListSharesStatusMapping(t *testing.T) {
	srv := providerStub(t, "list-shares", http.StatusOK, `[{"id":"s1","recipient_email":"a@b.c"}]`)
	c := newTestClient(t, srv.URL)
	shares, err := c.ListShares(context.Background(), "a@b.c")
	if err != nil {
		t.Fatalf("ListShares: %v", err)
	}
	if len(shares) != 1 || shares[0].ID != "s1" {
		t.Errorf("shares = %+v, want one share s1", shares)
	}
}

func TestDownloadStreamsContent(t *testing.T) {
	const payload = "the file body streamed straight through"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/shares/s1/content" {
			t.Errorf("got %s %s, want GET /api/v1/shares/s1/content", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="doc.pdf"`)
		_, _ = io.WriteString(w, payload)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL)
	content, err := c.Download(context.Background(), "s1", "a@b.c")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	defer content.Body.Close()
	if content.ContentType != "application/pdf" || content.ContentDisposition != `attachment; filename="doc.pdf"` {
		t.Errorf("headers = %q / %q", content.ContentType, content.ContentDisposition)
	}
	got, _ := io.ReadAll(content.Body)
	if string(got) != payload {
		t.Errorf("body = %q, want %q", got, payload)
	}
}

// TestDownloadStatusMapping keeps Download's real HTTP error path and its
// "download" action claim (asserted only in providerStub) under test with one
// representative status; the full status → sentinel table is TestMapErrorStatus.
func TestDownloadStatusMapping(t *testing.T) {
	srv := providerStub(t, "download", http.StatusForbidden, "backend detail that must not leak")
	c := newTestClient(t, srv.URL)
	if _, err := c.Download(context.Background(), "s1", "a@b.c"); !errors.Is(err, ErrForbidden) {
		t.Errorf("err = %v, want ErrForbidden", err)
	}
}

func TestUploadForwardsBodyAndHeaders(t *testing.T) {
	const payload = "uploaded bytes"
	var gotBody, gotName, gotType, gotMethod, gotPath, gotQueryPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotQueryPath = r.URL.Query().Get("path")
		gotName = r.Header.Get("X-Atrium-Filename")
		gotType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL)
	err := c.Upload(context.Background(), "s1", "a@b.c", "report.txt", "text/plain", "docs/inbox", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/shares/s1/upload" {
		t.Errorf("got %s %s, want POST /api/v1/shares/s1/upload", gotMethod, gotPath)
	}
	if gotQueryPath != "docs/inbox" {
		t.Errorf("forwarded path = %q, want docs/inbox", gotQueryPath)
	}
	if gotName != "report.txt" || gotType != "text/plain" || gotBody != payload {
		t.Errorf("forwarded name/type/body = %q / %q / %q", gotName, gotType, gotBody)
	}
}

// TestUploadStatusMapping keeps Upload's real HTTP error path and its "upload"
// action claim (asserted only in providerStub) under test with one representative
// status; the full status → sentinel table is TestMapErrorStatus.
func TestUploadStatusMapping(t *testing.T) {
	srv := providerStub(t, "upload", http.StatusForbidden, "backend detail")
	c := newTestClient(t, srv.URL)
	err := c.Upload(context.Background(), "s1", "a@b.c", "f.txt", "text/plain", "", strings.NewReader("x"))
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("err = %v, want ErrForbidden", err)
	}
}

func TestListFolder(t *testing.T) {
	t.Run("sub-path is forwarded and the listing decoded", func(t *testing.T) {
		var gotPath, gotQueryPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertBearerAction(t, r, "list-folder")
			gotPath, gotQueryPath = r.URL.Path, r.URL.Query().Get("path")
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"is_file":false,"entries":[{"id":"42","name":"report.pdf","is_folder":false,"is_own":true}]}`)
		}))
		t.Cleanup(srv.Close)

		c := newTestClient(t, srv.URL)
		listing, err := c.ListFolder(context.Background(), "s1", "a@b.c", "docs/sub")
		if err != nil {
			t.Fatalf("ListFolder: %v", err)
		}
		if gotPath != "/api/v1/shares/s1/folder" || gotQueryPath != "docs/sub" {
			t.Errorf("got %s ?path=%q, want /api/v1/shares/s1/folder ?path=docs/sub", gotPath, gotQueryPath)
		}
		if listing.IsFile || len(listing.Entries) != 1 || listing.Entries[0].Name != "report.pdf" {
			t.Errorf("listing = %+v, want one entry report.pdf", listing)
		}
	})

	t.Run("empty path omits the query", func(t *testing.T) {
		var gotRawQuery string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotRawQuery = r.URL.RawQuery
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"is_file":false,"entries":[]}`)
		}))
		t.Cleanup(srv.Close)

		c := newTestClient(t, srv.URL)
		if _, err := c.ListFolder(context.Background(), "s1", "a@b.c", ""); err != nil {
			t.Fatalf("ListFolder: %v", err)
		}
		if gotRawQuery != "" {
			t.Errorf("raw query = %q, want empty for a root listing", gotRawQuery)
		}
	})
}

func TestDownloadFolderFile(t *testing.T) {
	const payload = "nested file body streamed through"
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBearerAction(t, r, "download-file")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="nested.pdf"`)
		_, _ = io.WriteString(w, payload)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL)
	content, err := c.DownloadFolderFile(context.Background(), "s1", "f7", "a@b.c")
	if err != nil {
		t.Fatalf("DownloadFolderFile: %v", err)
	}
	defer content.Body.Close()
	if gotPath != "/api/v1/shares/s1/folder/f7/content" {
		t.Errorf("path = %q, want /api/v1/shares/s1/folder/f7/content", gotPath)
	}
	if content.ContentType != "application/pdf" || content.ContentDisposition != `attachment; filename="nested.pdf"` {
		t.Errorf("headers = %q / %q", content.ContentType, content.ContentDisposition)
	}
	if got, _ := io.ReadAll(content.Body); string(got) != payload {
		t.Errorf("body = %q, want %q", got, payload)
	}
}

func TestHealthCheck(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		srv := providerStub(t, "healthcheck", http.StatusOK, `{"status":"ok"}`)
		c := newTestClient(t, srv.URL)
		if err := c.HealthCheck(context.Background()); err != nil {
			t.Fatalf("HealthCheck: %v", err)
		}
	})
	t.Run("trust not configured", func(t *testing.T) {
		srv := providerStub(t, "healthcheck", http.StatusForbidden, `{"error":"trust_not_configured"}`)
		c := newTestClient(t, srv.URL)
		err := c.HealthCheck(context.Background())
		if !errors.Is(err, ErrTrustNotConfigured) {
			t.Fatalf("err = %v, want ErrTrustNotConfigured", err)
		}
		// The diagnostic must include the public key to install on the provider.
		// The backend-specific install command lives in the provider's own docs,
		// so the core message stays storage-agnostic.
		if !strings.Contains(err.Error(), "BEGIN PUBLIC KEY") {
			t.Errorf("diagnostic message not actionable: %v", err)
		}
	})
}

func TestMetaFromRequest(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "198.51.100.9:54321"
	r.Header.Set("X-Forwarded-For", "198.51.100.9, 10.0.0.2")
	meta := MetaFromRequest(r)
	if meta.IP != "198.51.100.9" {
		t.Errorf("IP = %q, want host without port", meta.IP)
	}
	if meta.XFF != "198.51.100.9, 10.0.0.2" {
		t.Errorf("XFF = %q", meta.XFF)
	}
}
