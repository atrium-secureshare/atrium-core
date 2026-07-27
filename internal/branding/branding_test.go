package branding

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Body.String()
}

func TestHandlerServesEmbeddedDefault(t *testing.T) {
	code, body := get(t, Handler(""), "/branding/logo.svg")
	if code != http.StatusOK {
		t.Fatalf("logo.svg: status = %d, want 200", code)
	}
	if len(body) == 0 || body[:4] != "<svg" {
		t.Fatalf("logo.svg: body does not look like an SVG: %.20q", body)
	}
}

func TestHandlerMountedFileWins(t *testing.T) {
	dir := t.TempDir()
	const override = "<svg>override</svg>"
	if err := os.WriteFile(filepath.Join(dir, "logo.svg"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}

	code, body := get(t, Handler(dir), "/branding/logo.svg")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if body != override {
		t.Fatalf("body = %q, want mounted override %q", body, override)
	}
}

func TestHandlerFallsBackWhenFileAbsentInMount(t *testing.T) {
	// A configured but incomplete mount (only logo.svg) must still serve the
	// embedded default for the untouched dark logo.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logo.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, body := get(t, Handler(dir), "/branding/logo-dark.svg")
	if code != http.StatusOK {
		t.Fatalf("logo-dark.svg: status = %d, want 200 (embedded fallback)", code)
	}
	if len(body) < 4 || body[:4] != "<svg" {
		t.Fatalf("logo-dark.svg: not the embedded default: %.20q", body)
	}
}

func TestHandlerUnknownFileIs404(t *testing.T) {
	code, _ := get(t, Handler(""), "/branding/does-not-exist.svg")
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}
}

func TestHandlerRejectsTraversal(t *testing.T) {
	// Plant a secret next to (outside) the mount and try to escape into it.
	base := t.TempDir()
	mount := filepath.Join(base, "branding")
	if err := os.Mkdir(mount, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "secret.txt"), []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := Handler(mount)
	// Both a lexical escape (caught before the filesystem) and an encoded one
	// (resolved by os.Root) must not reach the secret; the response is a 404,
	// never the secret's contents.
	for _, p := range []string{"/branding/../secret.txt", "/branding/%2e%2e/secret.txt"} {
		code, body := get(t, h, p)
		if code == http.StatusOK && body == "top secret" {
			t.Fatalf("%s: leaked file outside the mount", p)
		}
	}
}
