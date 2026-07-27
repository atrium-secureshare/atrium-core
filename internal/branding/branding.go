// Package branding serves white-label assets under the public /branding/ route.
// A file mounted in BRANDING_DIR overrides the same-named embedded default, so an
// unconfigured build ships the Atrium logos and nothing 404s. It is deliberately a
// plain file server, not a template seam (YAGNI).
package branding

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

//go:embed defaults
var embedded embed.FS

// Handler serves /branding/<file>: a mounted file wins, else the embedded default,
// else 404. dir is opened per request via os.Root, so traversal outside the mount
// is impossible and a changed file is picked up without a restart.
func Handler(dir string) http.Handler {
	defaults, _ := fs.Sub(embedded, "defaults")
	return http.StripPrefix("/branding/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		// A remaining ".." or absolute segment after Clean means an escape attempt.
		if name == "." || strings.HasPrefix(name, "..") || path.IsAbs(name) {
			http.NotFound(w, r)
			return
		}
		if dir != "" && serveMounted(w, r, dir, name) {
			return
		}
		serveDefault(w, r, defaults, name)
	}))
}

// serveMounted serves name from the os.Root-rooted mount, reporting whether it
// handled the request; a missing file returns false so the caller falls back to
// the embedded default.
func serveMounted(w http.ResponseWriter, r *http.Request, dir, name string) bool {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return false
	}
	defer root.Close()

	f, err := root.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	// *os.File already implements io.ReadSeeker.
	http.ServeContent(w, r, name, info.ModTime(), f)
	return true
}

// serveDefault serves name from the embedded defaults, 404ing when absent.
func serveDefault(w http.ResponseWriter, r *http.Request, defaults fs.FS, name string) {
	f, err := defaults.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, name, time.Time{}, f.(io.ReadSeeker))
}
