package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atrium-secureshare/atrium-core/internal/audit"
	"github.com/atrium-secureshare/atrium-core/internal/auth"
	"github.com/atrium-secureshare/atrium-core/internal/provider"
)

const testEmail = "User@Example.com"

// fakeProvider is a scripted Streamer for the proxy handlers. It records what it
// was called with and always drains the upload body so a MaxBytesReader trips.
type fakeProvider struct {
	content     *provider.Content
	downloadErr error
	uploadErr   error

	calledDownload bool
	calledUpload   bool
	gotShareID     string
	gotEmail       string
	gotFileID      string
	gotFilename    string
	gotType        string
	gotPath        string
	gotBody        []byte
}

func (f *fakeProvider) Download(_ context.Context, shareID, email string) (*provider.Content, error) {
	f.calledDownload, f.gotShareID, f.gotEmail = true, shareID, email
	if f.downloadErr != nil {
		return nil, f.downloadErr
	}
	return f.content, nil
}

func (f *fakeProvider) DownloadFolderFile(_ context.Context, shareID, fileID, email string) (*provider.Content, error) {
	f.calledDownload, f.gotShareID, f.gotEmail, f.gotFileID = true, shareID, email, fileID
	if f.downloadErr != nil {
		return nil, f.downloadErr
	}
	return f.content, nil
}

func (f *fakeProvider) Upload(_ context.Context, shareID, email, filename, contentType, path string, body io.Reader) error {
	f.calledUpload, f.gotShareID, f.gotEmail = true, shareID, email
	f.gotFilename, f.gotType, f.gotPath = filename, contentType, path
	b, err := io.ReadAll(body)
	f.gotBody = b
	if err != nil {
		return err // surfaces *http.MaxBytesError for the size-limit path
	}
	return f.uploadErr
}

// newProxy builds a Proxy whose logger writes audit lines to an in-memory buffer
// the tests can inspect. Pseudonymization is on, as in production.
func newProxy(a Streamer, max int64) (*Proxy, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	p := New(a, max, audit.New(buf, slog.LevelInfo, nil, true))
	return p, buf
}

func authedRequest(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	req = req.WithContext(auth.NewContext(req.Context(), auth.SessionData{Email: testEmail}))
	req.SetPathValue("shareID", "s1")
	return req
}

func TestHandleDownloadStreamsAndAudits(t *testing.T) {
	const payload = "streamed body"
	fake := &fakeProvider{content: &provider.Content{
		Body:               io.NopCloser(strings.NewReader(payload)),
		ContentType:        "application/pdf",
		ContentDisposition: `attachment; filename="doc.pdf"`,
		ContentLength:      int64(len(payload)),
	}}
	p, auditBuf := newProxy(fake, 1<<20)

	rec := httptest.NewRecorder()
	p.HandleDownload(rec, authedRequest(http.MethodGet, "/api/shares/s1/content", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != payload {
		t.Errorf("body = %q, want %q", rec.Body.String(), payload)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="doc.pdf"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if cl := rec.Header().Get("Content-Length"); cl != "13" {
		t.Errorf("Content-Length = %q, want 13", cl)
	}
	if fake.gotShareID != "s1" || fake.gotEmail != testEmail {
		t.Errorf("provider called with %q / %q", fake.gotShareID, fake.gotEmail)
	}
	assertAudit(t, auditBuf, audit.EventDownloadStart)
	assertOperational(t, auditBuf, audit.EventDownloadComplete)
}

func TestHandleFolderFileDownloadStreamsChild(t *testing.T) {
	const payload = "inner file body"
	fake := &fakeProvider{content: &provider.Content{
		Body:               io.NopCloser(strings.NewReader(payload)),
		ContentType:        "text/plain",
		ContentDisposition: `attachment; filename="inner.txt"`,
		ContentLength:      int64(len(payload)),
	}}
	p, auditBuf := newProxy(fake, 1<<20)

	req := authedRequest(http.MethodGet, "/api/shares/s1/folder/42/content", nil)
	req.SetPathValue("fileID", "42")
	rec := httptest.NewRecorder()
	p.HandleFolderFileDownload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != payload {
		t.Errorf("body = %q, want %q", rec.Body.String(), payload)
	}
	if fake.gotShareID != "s1" || fake.gotFileID != "42" || fake.gotEmail != testEmail {
		t.Errorf("provider called with share=%q file=%q email=%q", fake.gotShareID, fake.gotFileID, fake.gotEmail)
	}
	// The audit line carries the child file id alongside the share.
	ev := assertAudit(t, auditBuf, audit.EventDownloadStart)
	if ev["file_id"] != "42" {
		t.Errorf("audit file_id = %v, want 42", ev["file_id"])
	}
	assertOperational(t, auditBuf, audit.EventDownloadComplete)
}

func TestHandleDownloadErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantEvent  string // "" means no audit event expected
	}{
		{"not found", provider.ErrShareNotFound, http.StatusNotFound, audit.EventAccessDenied},
		{"forbidden", provider.ErrForbidden, http.StatusForbidden, audit.EventAccessDenied},
		{"gone", provider.ErrShareGone, http.StatusGone, audit.EventShareExpired},
		{"unexpected", provider.ErrUnexpectedStatus, http.StatusInternalServerError, ""},
		{"unreachable", context.DeadlineExceeded, http.StatusServiceUnavailable, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, auditBuf := newProxy(&fakeProvider{downloadErr: tc.err}, 1<<20)
			rec := httptest.NewRecorder()
			p.HandleDownload(rec, authedRequest(http.MethodGet, "/api/shares/s1/content", nil))
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if strings.Contains(rec.Body.String(), tc.err.Error()) {
				t.Errorf("response leaks backend error: %q", rec.Body.String())
			}
			if tc.wantEvent != "" {
				assertAudit(t, auditBuf, tc.wantEvent)
			} else {
				assertNoAudit(t, auditBuf)
			}
		})
	}
}

func TestHandleUploadSuccess(t *testing.T) {
	const payload = "file contents"
	fake := &fakeProvider{}
	p, auditBuf := newProxy(fake, 1<<20)

	req := authedRequest(http.MethodPost, "/api/shares/s1/upload?path=docs%2Finbox", strings.NewReader(payload))
	req.Header.Set("X-Atrium-Filename", "report.txt")
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	p.HandleUpload(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if !fake.calledUpload || string(fake.gotBody) != payload {
		t.Errorf("upload body = %q, want %q", fake.gotBody, payload)
	}
	if fake.gotFilename != "report.txt" || fake.gotType != "text/plain" {
		t.Errorf("filename/type = %q / %q", fake.gotFilename, fake.gotType)
	}
	if fake.gotPath != "docs/inbox" {
		t.Errorf("path = %q, want the decoded sub-path forwarded to the provider", fake.gotPath)
	}
	assertAudit(t, auditBuf, audit.EventUploadStart)
	assertOperational(t, auditBuf, audit.EventUploadComplete)
}

func TestHandleUploadRequiresFilename(t *testing.T) {
	fake := &fakeProvider{}
	p, _ := newProxy(fake, 1<<20)
	rec := httptest.NewRecorder()
	p.HandleUpload(rec, authedRequest(http.MethodPost, "/api/shares/s1/upload", strings.NewReader("x")))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if fake.calledUpload {
		t.Error("provider should not be called without a filename")
	}
}

func TestHandleUploadRejectsDeclaredOversize(t *testing.T) {
	fake := &fakeProvider{}
	p, _ := newProxy(fake, 10)
	req := authedRequest(http.MethodPost, "/api/shares/s1/upload", strings.NewReader("way past the ten byte limit"))
	req.Header.Set("X-Atrium-Filename", "big.bin")
	rec := httptest.NewRecorder()
	p.HandleUpload(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
	if fake.calledUpload {
		t.Error("provider should not be called for a declared-oversize upload")
	}
}

func TestHandleUploadEnforcesLimitOnBody(t *testing.T) {
	fake := &fakeProvider{}
	p, _ := newProxy(fake, 10)
	// Content-Length unknown (-1), so only the streaming MaxBytesReader can catch
	// the oversize body once the provider starts reading it.
	req := authedRequest(http.MethodPost, "/api/shares/s1/upload", strings.NewReader("this body is definitely longer than ten bytes"))
	req.ContentLength = -1
	req.Header.Set("X-Atrium-Filename", "big.bin")
	rec := httptest.NewRecorder()
	p.HandleUpload(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}

// errReader yields n bytes, then a non-EOF error, simulating a transfer that
// breaks mid-stream (a disconnect or a backend read failure).
type errReader struct {
	data []byte
	off  int
}

func (e *errReader) Read(p []byte) (int, error) {
	if e.off >= len(e.data) {
		return 0, io.ErrUnexpectedEOF
	}
	n := copy(p, e.data[e.off:])
	e.off += n
	return n, nil
}

func TestHandleDownloadInterruptedMidStream(t *testing.T) {
	// The provider body errors after a few bytes. Headers are already committed,
	// so the handler must write what it got and return cleanly, not panic.
	fake := &fakeProvider{content: &provider.Content{
		Body:          io.NopCloser(&errReader{data: []byte("partial")}),
		ContentType:   "application/octet-stream",
		ContentLength: 1024,
	}}
	p, _ := newProxy(fake, 1<<20)
	rec := httptest.NewRecorder()
	p.HandleDownload(rec, authedRequest(http.MethodGet, "/api/shares/s1/content", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (already committed before the break)", rec.Code)
	}
	if rec.Body.String() != "partial" {
		t.Errorf("body = %q, want the bytes read before the break", rec.Body.String())
	}
}

func TestHandleUploadInterruptedMidStream(t *testing.T) {
	// A read failure that is not a size violation surfaces from the provider; the
	// core reports 503 without leaking, and holds no partial state of its own.
	fake := &fakeProvider{}
	p, _ := newProxy(fake, 1<<20)
	req := authedRequest(http.MethodPost, "/api/shares/s1/upload", &errReader{data: []byte("abc")})
	req.ContentLength = -1
	req.Header.Set("X-Atrium-Filename", "f.bin")
	rec := httptest.NewRecorder()
	p.HandleUpload(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestHandlersRequireSession(t *testing.T) {
	p, _ := newProxy(&fakeProvider{}, 1<<20)
	for _, tc := range []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
	}{
		{"download", p.HandleDownload},
		{"folder file download", p.HandleFolderFileDownload},
		{"upload", p.HandleUpload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/shares/s1/content", nil)
			req.SetPathValue("shareID", "s1")
			rec := httptest.NewRecorder()
			tc.call(rec, req) // no session in context
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", rec.Code)
			}
		})
	}
}

func assertAudit(t *testing.T, buf *bytes.Buffer, event string) map[string]any {
	t.Helper()

	scanner := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
	for scanner.Scan() {
		var ev map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("audit line not JSON: %v (%q)", err, scanner.Text())
		}
		if ev["msg"] != event {
			continue
		}
		if ev["level"] != "AUDIT" || ev["share_id"] != "s1" {
			t.Errorf("audit event = %+v, want level=AUDIT share=s1", ev)
		}
		if email, _ := ev["email"].(string); email == testEmail || strings.Contains(email, "@") {
			t.Errorf("audit email = %q, want a hash of %q", email, testEmail)
		}
		return ev
	}
	t.Errorf("no audit event %q found in %q", event, buf.String())
	return nil
}

func assertNoAudit(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
	for scanner.Scan() {
		var ev map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("log line not JSON: %v (%q)", err, scanner.Text())
		}
		if ev["level"] == "AUDIT" {
			t.Errorf("expected no audit event, got %q", scanner.Text())
		}
	}
}

// assertOperational checks that an INFO-level (non-audit) line for event was
// emitted to buf and returns it. It is the counterpart to assertAudit for the
// events deliberately kept off the audit trail (listings, transfer
// completions): they must still be logged, just not at AUDIT.
func assertOperational(t *testing.T, buf *bytes.Buffer, event string) map[string]any {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
	for scanner.Scan() {
		var ev map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("log line not JSON: %v (%q)", err, scanner.Text())
		}
		if ev["msg"] != event {
			continue
		}
		if ev["level"] != "INFO" {
			t.Errorf("event %q level = %v, want INFO (operational, not audit)", event, ev["level"])
		}
		return ev
	}
	t.Errorf("no operational event %q found in %q", event, buf.String())
	return nil
}
