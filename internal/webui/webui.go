// Package webui serves the compiled recipient frontend (the Vite build output) as
// a single-page app. The assets are embedded so the gateway ships as one
// self-contained image. Only dist/.gitkeep is committed; a plain go build without
// a frontend build compiles and serves a 404 for the shell until assets exist.
package webui

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// Brand carries the optional white-label values injected into index.html. Text
// and theme values are emitted as window.__ATRIUM__ JSON; AccentColor is applied
// separately as a CSS override (see injectBrand), so it is not part of the JSON.
type Brand struct {
	Name         string `json:"brandName,omitempty"`
	Sub          string `json:"brandSub,omitempty"`
	DefaultTheme string `json:"defaultTheme,omitempty"`
	// AccentColor is a validated hex colour (config enforces the format), so it
	// is safe to interpolate straight into the injected <style>.
	AccentColor string `json:"-"`
}

// Handler serves the SPA: real files are served directly with long-lived caching,
// any other path falls back to index.html. It returns a 404-only handler when the
// bundle is absent, so API and auth routes still work. The second return value is
// the CSP script-src hashes for the shell's inline scripts, so the caller can
// permit exactly them without 'unsafe-inline'; nil when no shell is served.
func Handler(brand Brand) (http.Handler, []string) {
	dist, err := fs.Sub(embedded, "dist")
	if err != nil {
		return http.NotFoundHandler(), nil
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		// No build present; the shell is unavailable but nothing else breaks.
		return http.NotFoundHandler(), nil
	}

	index, _ := fs.ReadFile(dist, "index.html")
	index = injectBrand(index, brand)
	scriptHashes := inlineScriptHashes(index)
	fileServer := http.FileServer(http.FS(dist))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		// Content-hashed asset filenames are safe to cache hard; the shell itself
		// (served below) stays uncached.
		if name != "" {
			if _, err := fs.Stat(dist, name); err == nil {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// A client-side route: serve the SPA shell under the status the route
		// warrants (see shellStatus), not a blanket 200, so status and rendered
		// page agree for crawlers, caches and a WAF.
		serveShell(w, r, index, shellStatus(r.URL.Path))
	}), scriptHashes
}

// shellStatus mirrors the client routes: known app routes are 200, the sign-in
// error route is 403, anything else 404. Server and router derive the status from
// the same path, so no status or untrusted query parameter is signalled between them.
func shellStatus(p string) int {
	switch p = path.Clean(p); {
	case p == "/" || strings.HasPrefix(p, "/share/"):
		return http.StatusOK
	case p == "/auth/error":
		return http.StatusForbidden
	default:
		return http.StatusNotFound
	}
}

// inlineScriptHashes returns the CSP tokens ('sha256-<base64>') for every inline
// <script> block in html. External scripts (with src) are omitted; script-src
// 'self' already covers them.
func inlineScriptHashes(html []byte) []string {
	var hashes []string
	rest := string(html)
	for {
		open := strings.Index(rest, "<script")
		if open < 0 {
			break
		}
		tagClose := strings.Index(rest[open:], ">")
		if tagClose < 0 {
			break
		}
		openTag := rest[open : open+tagClose]
		contentStart := open + tagClose + 1
		end := strings.Index(rest[contentStart:], "</script>")
		if end < 0 {
			break
		}
		content := rest[contentStart : contentStart+end]
		rest = rest[contentStart+end+len("</script>"):]
		if strings.Contains(openTag, "src=") {
			continue
		}
		sum := sha256.Sum256([]byte(content))
		hashes = append(hashes, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	return hashes
}

// injectBrand splices the white-label brand into index.html. Text/theme values go
// into a window.__ATRIUM__ script after <head>, so they exist before the pre-paint
// theme script; encoding/json's HTML escaping renders "</script>" harmless. The
// accent goes into a :root override before </head> (after the stylesheet, so it
// wins on source order). Each part is a no-op when unset, so the stock shell is
// served verbatim.
func injectBrand(index []byte, brand Brand) []byte {
	s := string(index)
	if script := brandScript(brand); script != "" {
		s = spliceAfter(s, "<head>", script)
	}
	if style := accentStyle(brand.AccentColor); style != "" {
		s = spliceBefore(s, "</head>", style)
	}
	return []byte(s)
}

// brandScript builds the window.__ATRIUM__ script for the text/theme values, or
// "" when none is set.
func brandScript(brand Brand) string {
	if brand.Name == "" && brand.Sub == "" && brand.DefaultTheme == "" {
		return ""
	}
	data, err := json.Marshal(brand)
	if err != nil {
		return ""
	}
	return "\n    <script>window.__ATRIUM__=" + string(data) + "</script>"
}

// accentStyle builds a :root override mapping the accent onto the theme tokens, or
// "" when none is set. The tint appends ~12% alpha for a #rrggbb value. The accent
// is a validated hex colour, so it cannot contain CSS-breaking characters.
func accentStyle(accent string) string {
	if accent == "" {
		return ""
	}
	tint := accent
	if len(accent) == 7 { // #rrggbb -> #rrggbb1f
		tint = accent + "1f"
	}
	return "\n    <style>:root{--primary:" + accent + ";--primary-hover:" + accent +
		";--accent-foreground:" + accent + ";--ring:" + accent + ";--accent:" + tint + "}</style>"
}

// spliceAfter inserts ins immediately after the first occurrence of anchor,
// returning s unchanged when the anchor is absent.
func spliceAfter(s, anchor, ins string) string {
	i := strings.Index(s, anchor)
	if i < 0 {
		return s
	}
	at := i + len(anchor)
	return s[:at] + ins + s[at:]
}

// spliceBefore inserts ins immediately before the first occurrence of anchor,
// returning s unchanged when the anchor is absent.
func spliceBefore(s, anchor, ins string) string {
	i := strings.Index(s, anchor)
	if i < 0 {
		return s
	}
	return s[:i] + ins + s[i:]
}

// serveShell writes the SPA shell under an explicit status. It writes the body
// directly rather than via http.ServeContent, which only negotiates 200/206/304;
// an error shell (403/404) needs its status set verbatim.
func serveShell(w http.ResponseWriter, _ *http.Request, index []byte, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	_, _ = w.Write(index)
}
