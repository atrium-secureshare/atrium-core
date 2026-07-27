package webui

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
)

// sampleIndex mirrors the built shell: the pre-paint theme script early in the
// head and the stylesheet link near the end, so tests can assert both the
// __ATRIUM__ script (which must precede the theme script) and the accent style
// (which must follow the stylesheet link to win on source order).
const sampleIndex = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <script>
      // pre-paint theme
      var t = (window.__ATRIUM__ && window.__ATRIUM__.defaultTheme) || 'light'
    </script>
    <script type="module" src="/assets/index.js"></script>
    <link rel="stylesheet" href="/assets/index.css">
  </head>
  <body></body>
</html>`

func TestShellStatus(t *testing.T) {
	for path, want := range map[string]int{
		"/":                     http.StatusOK,
		"/share/abc":            http.StatusOK,
		"/share/abc/sub/folder": http.StatusOK,
		"/auth/error":           http.StatusForbidden,
		"/foo":                  http.StatusNotFound,
		"/share":                http.StatusNotFound, // no hash: not a share route
		"/auth/login":           http.StatusNotFound, // a server route, never a shell
	} {
		if got := shellStatus(path); got != want {
			t.Errorf("shellStatus(%q) = %d, want %d", path, got, want)
		}
	}
}

func TestInjectBrandNoopWhenEmpty(t *testing.T) {
	out := injectBrand([]byte(sampleIndex), Brand{})
	if string(out) != sampleIndex {
		t.Fatalf("empty brand must serve index verbatim, got:\n%s", out)
	}
}

func TestInjectBrandInsertsTextConfig(t *testing.T) {
	out := string(injectBrand([]byte(sampleIndex), Brand{Name: "Acme", DefaultTheme: "dark"}))

	if !strings.Contains(out, `window.__ATRIUM__=`) {
		t.Fatalf("injected script missing:\n%s", out)
	}
	for _, want := range []string{`"brandName":"Acme"`, `"defaultTheme":"dark"`} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %s in output:\n%s", want, out)
		}
	}
	// Sub was empty (omitempty) and the accent lives in CSS, never the script.
	if strings.Contains(out, "brandSub") {
		t.Errorf("empty field must be omitted, found brandSub:\n%s", out)
	}
	if strings.Contains(out, "accentColor") {
		t.Errorf("accent must not appear in the __ATRIUM__ script:\n%s", out)
	}
	// The injected config must precede the pre-paint theme script that reads
	// defaultTheme, so window.__ATRIUM__ already exists when that script runs.
	inject := strings.Index(out, "window.__ATRIUM__=")
	reader := strings.Index(out, "window.__ATRIUM__ &&") // the pre-paint script
	if reader < 0 {
		t.Fatalf("pre-paint theme script marker not found:\n%s", out)
	}
	if inject > reader {
		t.Fatalf("injected config (%d) must precede the theme script that reads it (%d)", inject, reader)
	}
}

func TestInjectBrandAccentStyle(t *testing.T) {
	out := string(injectBrand([]byte(sampleIndex), Brand{AccentColor: "#2563eb"}))

	if !strings.Contains(out, "--primary:#2563eb") {
		t.Fatalf("accent not mapped onto --primary:\n%s", out)
	}
	if !strings.Contains(out, "--accent:#2563eb1f") {
		t.Fatalf("accent tint (12%% alpha) missing:\n%s", out)
	}
	// The accent style must sit after the stylesheet link so it wins by source
	// order, and no __ATRIUM__ script is emitted for an accent-only brand.
	style := strings.Index(out, "<style>:root{--primary")
	css := strings.Index(out, `rel="stylesheet"`)
	if style < 0 || css < 0 || style < css {
		t.Fatalf("accent <style> (%d) must follow the stylesheet link (%d):\n%s", style, css, out)
	}
	if strings.Contains(out, "window.__ATRIUM__=") {
		t.Errorf("accent-only brand must not emit an __ATRIUM__ script:\n%s", out)
	}
}

func TestInjectBrandAccentShortHexNoTint(t *testing.T) {
	// A non-#rrggbb value carries its own alpha (or none); no 1f is appended.
	out := string(injectBrand([]byte(sampleIndex), Brand{AccentColor: "#abc"}))
	if !strings.Contains(out, "--accent:#abc}") {
		t.Fatalf("short hex must be used verbatim as the tint:\n%s", out)
	}
}

func TestInjectBrandEscapesScriptClose(t *testing.T) {
	// A value containing </script> must not break out of the injected tag.
	out := string(injectBrand([]byte(sampleIndex), Brand{Name: "</script><script>alert(1)</script>"}))
	if strings.Contains(out, "</script><script>alert(1)") {
		t.Fatalf("raw </script> leaked into the document:\n%s", out)
	}
	// json's default HTML escaping renders '<' and '>' as \uXXXX.
	if !strings.Contains(out, "u003cscript") {
		t.Fatalf("expected the payload escaped as \\u003cscript..., got:\n%s", out)
	}
}

func TestInjectBrandNoAnchorReturnsInput(t *testing.T) {
	const noHead = "<html><body>no head here</body></html>"
	out := injectBrand([]byte(noHead), Brand{Name: "Acme", AccentColor: "#123456"})
	if string(out) != noHead {
		t.Fatalf("missing anchors must return input unchanged, got:\n%s", out)
	}
}

// sha256Token mirrors how inlineScriptHashes formats a CSP hash source, so a
// test can assert byte-exact hashing of a known script body.
func sha256Token(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

func TestInlineScriptHashes(t *testing.T) {
	// Each input holds one inline <script> plus one external module script (with
	// src), so only the inline one is hashed: exactly one token. When want is
	// set, the token must also byte-exactly match the inline body's hash, which
	// (via sha256Token) proves the 'sha256-<base64>' CSP source format too.
	const minimal = `<head><script>var a=1;</script><script type="module" src="/x.js"></script></head>`
	tests := []struct {
		name string
		html string
		want string // exact expected token, or "" to assert the count only
	}{
		{"skips external script in the built shell", sampleIndex, ""},
		{"hashes the exact inline body", minimal, sha256Token("var a=1;")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hashes := inlineScriptHashes([]byte(tc.html))
			if len(hashes) != 1 {
				t.Fatalf("want 1 inline-script hash, got %d: %v", len(hashes), hashes)
			}
			if tc.want != "" && hashes[0] != tc.want {
				t.Fatalf("hash mismatch:\n got %s\nwant %s", hashes[0], tc.want)
			}
		})
	}
}

func TestInlineScriptHashesCoversInjectedBrandScript(t *testing.T) {
	// A brand adds the window.__ATRIUM__ inline script, so the served shell then
	// carries two inline scripts and the CSP must permit both.
	index := injectBrand([]byte(sampleIndex), Brand{Name: "Acme"})
	hashes := inlineScriptHashes(index)
	if len(hashes) != 2 {
		t.Fatalf("want 2 inline-script hashes with a brand set, got %d: %v", len(hashes), hashes)
	}
}

func TestHandlerReturnsHashesForServedShell(t *testing.T) {
	// The embedded dist is present only when the frontend has been built; when it
	// has, Handler must report at least the theme-script hash so the CSP is not
	// silently left without a script-src exception for the shell.
	h, hashes := Handler(Brand{})
	if h == nil {
		t.Fatal("Handler returned a nil handler")
	}
	// No assertion on len when the bundle is absent (hashes is nil then); when
	// present, every reported token must be a well-formed CSP hash source.
	for _, tok := range hashes {
		if !strings.HasPrefix(tok, "'sha256-") || !strings.HasSuffix(tok, "'") {
			t.Fatalf("malformed hash token: %q", tok)
		}
	}
}
