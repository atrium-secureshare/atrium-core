package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/atrium-secureshare/atrium-core/internal/provider"
)

// stubProvider is an in-process stand-in implementing the core's backend-neutral
// JSON provider contract, nothing Nextcloud-aware. It only decodes the
// per-request JWT (never verifies it) to emulate the contract's authorization.
type stubProvider struct {
	store *store
	url   string
}

// startStubProvider serves the provider contract on an ephemeral local port; its
// base URL feeds the core's provider client.
func startStubProvider(st *store) (*stubProvider, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen stub provider: %w", err)
	}
	a := &stubProvider{store: st, url: "http://" + ln.Addr().String()}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", a.handleHealth)
	mux.HandleFunc("GET /api/v1/shares", a.handleShares)
	mux.HandleFunc("GET /api/v1/shares/{id}/content", a.handleContent)
	mux.HandleFunc("GET /api/v1/shares/{id}/folder", a.handleFolder)
	mux.HandleFunc("GET /api/v1/shares/{id}/folder/{fileID}/content", a.handleFolderFile)
	mux.HandleFunc("POST /api/v1/shares/{id}/upload", a.handleUpload)
	// Control endpoint (not part of the provider contract): reset the dataset.
	mux.HandleFunc("POST /_seed", func(w http.ResponseWriter, _ *http.Request) {
		st.seed()
		w.WriteHeader(http.StatusNoContent)
	})

	go func() { _ = http.Serve(ln, mux) }()
	return a, nil
}

// tokenClaims are the fields the stub reads from the (unverified) request JWT.
type tokenClaims struct {
	Email  string `json:"email"`
	Action string `json:"action"`
}

// claimsFromRequest decodes the Bearer JWT payload without verifying it: the
// trust boundary is exercised by the provider's own suite, so the stub only
// needs the asserted email.
func claimsFromRequest(r *http.Request) tokenClaims {
	raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return tokenClaims{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return tokenClaims{}
	}
	var c tokenClaims
	_ = json.Unmarshal(payload, &c)
	return c
}

func (a *stubProvider) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (a *stubProvider) handleShares(w http.ResponseWriter, r *http.Request) {
	email := claimsFromRequest(r).Email
	writeJSON(w, a.store.listFor(email))
}

// authorize enforces the email binding: a share the caller does not own is a
// 404, indistinguishable from a non-existent one.
func (a *stubProvider) authorize(w http.ResponseWriter, r *http.Request) *shareState {
	st := a.store.find(r.PathValue("id"))
	if st == nil || st.recipient != claimsFromRequest(r).Email {
		http.Error(w, "not found", http.StatusNotFound)
		return nil
	}
	return st
}

func (a *stubProvider) handleContent(w http.ResponseWriter, r *http.Request) {
	st := a.authorize(w, r)
	if st == nil {
		return
	}
	a.store.mu.Lock()
	if m := st.share.MaxDownloads; m != nil && st.share.DownloadCount >= *m {
		a.store.mu.Unlock()
		http.Error(w, "gone", http.StatusGone)
		return
	}
	st.share.DownloadCount++
	content := st.content
	name := st.share.FileName
	a.store.mu.Unlock()

	serveFile(w, name, content)
}

func (a *stubProvider) handleFolder(w http.ResponseWriter, r *http.Request) {
	st := a.authorize(w, r)
	if st == nil {
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()

	target, isFile := resolve(st.root, r.URL.Query().Get("path"))
	if target == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if isFile {
		e := target.entry
		writeJSON(w, provider.FolderListing{IsFile: true, Entry: &e})
		return
	}
	writeJSON(w, provider.FolderListing{
		Entries: filterByMode(target.children, st.share.Mode),
	})
}

func (a *stubProvider) handleFolderFile(w http.ResponseWriter, r *http.Request) {
	st := a.authorize(w, r)
	if st == nil {
		return
	}
	a.store.mu.Lock()
	n := findByID(st.root, r.PathValue("fileID"))
	var name string
	var content []byte
	if n != nil {
		name, content = n.entry.Name, n.content
	}
	a.store.mu.Unlock()

	if n == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	serveFile(w, name, content)
}

func (a *stubProvider) handleUpload(w http.ResponseWriter, r *http.Request) {
	st := a.authorize(w, r)
	if st == nil {
		return
	}
	if st.share.Mode == 0 { // read-only folders are not writable
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	name := r.Header.Get("X-Atrium-Filename")
	if decoded, err := url.QueryUnescape(name); err == nil {
		name = decoded
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	folder, isFile := resolve(st.root, r.URL.Query().Get("path"))
	if folder == nil || isFile {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	size := int64(len(body))
	folder.children = append(folder.children, &node{
		entry: provider.FolderEntry{
			ID: fmt.Sprintf("up-%d", len(folder.children)+1), Name: name,
			Size: &size, IsOwn: true,
		},
		content: body,
	})
	w.WriteHeader(http.StatusCreated)
}

// resolve walks a share tree to the node at path, returning (node, isFile) or
// (nil, false) when the path is missing.
func resolve(root *node, path string) (*node, bool) {
	if root == nil {
		return nil, false
	}
	cur := root
	for _, seg := range strings.Split(path, "/") {
		if seg == "" {
			continue
		}
		var next *node
		for _, c := range cur.children {
			if c.entry.Name == seg {
				next = c
				break
			}
		}
		if next == nil {
			return nil, false
		}
		cur = next
	}
	if cur != root && !cur.entry.IsFolder {
		return cur, true
	}
	return cur, false
}

func findByID(root *node, id string) *node {
	if root == nil {
		return nil
	}
	for _, c := range root.children {
		if c.entry.ID == id {
			return c
		}
		if c.entry.IsFolder {
			if found := findByID(c, id); found != nil {
				return found
			}
		}
	}
	return nil
}

// filterByMode applies the share's sharing mode to a folder's children, mirroring
// what the provider does before the core forwards the listing.
func filterByMode(children []*node, mode int) []provider.FolderEntry {
	out := make([]provider.FolderEntry, 0, len(children))
	for _, c := range children {
		switch mode {
		case 1: // write/read-own
			if !c.entry.IsOwn {
				continue
			}
		case 3: // dropzone
			continue
		}
		out = append(out, c.entry)
	}
	return out
}

func serveFile(w http.ResponseWriter, name string, content []byte) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
