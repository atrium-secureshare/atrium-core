// Package api wires the HTTP handlers for the Atrium core gateway.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/atrium-secureshare/atrium-core/internal/audit"
	"github.com/atrium-secureshare/atrium-core/internal/auth"
	"github.com/atrium-secureshare/atrium-core/internal/branding"
	"github.com/atrium-secureshare/atrium-core/internal/provider"
	"github.com/atrium-secureshare/atrium-core/internal/proxy"
	"github.com/atrium-secureshare/atrium-core/internal/tos"
	"github.com/atrium-secureshare/atrium-core/internal/webui"
)

// TrustChecker reports whether the provider trust relationship is established;
// the small interface lets tests substitute a stub.
type TrustChecker interface {
	HealthCheck(ctx context.Context) error
}

// ShareLister returns the shares the provider holds for a recipient.
type ShareLister interface {
	ListShares(ctx context.Context, email string) ([]provider.Share, error)
}

// FolderLister browses a shared folder at a relative path, mode-filtered and
// traversal-resolved by the provider.
type FolderLister interface {
	ListFolder(ctx context.Context, shareID, email, path string) (*provider.FolderListing, error)
}

// ProviderService is the provider dependency the router needs beyond the stream
// proxy: readiness probing and share/folder listing.
type ProviderService interface {
	TrustChecker
	ShareLister
	FolderLister
}

// Handler builds the gateway's root HTTP handler. The security boundary is the
// /api/ subtree, gated deny-by-default by RequireAuth -> RequireTOS, so any route
// added under /api/ is protected automatically.
func Handler(oidc *auth.OIDCAuth, tosMgr *tos.Manager, providerSvc ProviderService, streamProxy *proxy.Proxy, brandingDir string, brand webui.Brand, secureTransport bool, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	// The embedded SPA is the all-methods root fallback; more specific patterns
	// (and "/api/") take precedence. Registering it without a method avoids
	// conflicting with the all-methods "/api/" subtree, and it reports its inline
	// script hashes so the CSP can permit exactly them.
	spa, scriptHashes := webui.Handler(brand)
	mux.Handle("/", spa)
	// White-label assets served from BRANDING_DIR with embedded defaults; more
	// specific than "/", so it wins over the SPA fallback.
	mux.Handle("/branding/", branding.Handler(brandingDir))
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /readyz", handleReadyz(providerSvc))
	mux.HandleFunc("GET "+auth.LoginPath, oidc.LoginHandler)
	mux.HandleFunc("GET "+auth.CallbackPath, oidc.CallbackHandler)
	mux.HandleFunc("POST "+auth.LogoutPath, oidc.LogoutHandler)

	// Gated API surface: RequireAuth -> RequireTOS wraps apiMux once, so routes
	// inherit both and the gate cannot be forgotten per route.
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("GET /api/me", handleMe)
	apiMux.HandleFunc("GET /api/shares", handleListShares(providerSvc, logger))
	apiMux.HandleFunc("GET /api/shares/{shareID}/content", streamProxy.HandleDownload)
	apiMux.HandleFunc("GET /api/shares/{shareID}/folder", handleListFolder(providerSvc, logger))
	apiMux.HandleFunc("GET /api/shares/{shareID}/folder/{fileID}/content", streamProxy.HandleFolderFileDownload)
	apiMux.HandleFunc("POST /api/shares/{shareID}/upload", streamProxy.HandleUpload)
	mux.Handle("/api/", oidc.RequireAuth(tosMgr.RequireTOS(apiMux)))

	// Consent endpoints: authenticated but in front of the gate so a recipient
	// can fetch the terms and record acceptance. The only pre-consent exceptions.
	if tosMgr.Enabled() {
		mux.Handle("GET "+tos.ContentPath, oidc.RequireAuth(http.HandlerFunc(tosMgr.ContentHandler)))
		mux.Handle("POST "+tos.AcceptPath, oidc.RequireAuth(http.HandlerFunc(tosMgr.AcceptHandler)))
	}

	// Security headers wrap the whole surface so none can be forgotten per route.
	return securityHeaders(mux, contentSecurityPolicy(scriptHashes), secureTransport)
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// handleReadyz mirrors the provider trust relationship (200/503), kept
// independent of liveness so a degraded core is not restarted.
func handleReadyz(trust TrustChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		status, ready := http.StatusOK, true
		if err := trust.HealthCheck(ctx); err != nil {
			status, ready = http.StatusServiceUnavailable, false
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]bool{"provider": ready})
	}
}

// handleMe returns the authenticated recipient's identity.
func handleMe(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{"email": session.Email})
}

// shareView is the recipient-facing projection of a share: the recipient's email
// is never echoed back, and field names are the frontend's contract so the two
// can evolve independently.
type shareView struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Size          *int64     `json:"size"`
	IsFolder      bool       `json:"isFolder"`
	Mode          int        `json:"mode"`
	DownloadCount int        `json:"downloadCount"`
	MaxDownloads  *int       `json:"maxDownloads"`
	ExpiresAt     *time.Time `json:"expiresAt"`
	CreatedAt     *time.Time `json:"createdAt"`
}

// handleListShares returns the recipient's active shares. The recipient comes
// from the session, never a request parameter, so a caller lists only their own.
func handleListShares(shares ShareLister, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := auth.SessionFromContext(r.Context())
		if !ok {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		ctx := provider.ContextWithMeta(r.Context(), provider.MetaFromRequest(r))
		list, err := shares.ListShares(ctx, session.Email)
		if err != nil {
			// Generic 502; backend detail stays in the logs, never leaked.
			if errors.Is(err, provider.ErrUnexpectedStatus) {
				http.Error(w, "An error occurred", http.StatusBadGateway)
				return
			}
			http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		audit.Log(logger, r, slog.LevelInfo, audit.EventListShares, slog.String("email", session.Email))

		views := make([]shareView, 0, len(list))
		for _, s := range list {
			name := s.DisplayName
			if name == "" {
				name = s.FileName
			}
			views = append(views, shareView{
				ID:            s.ID,
				Name:          name,
				Size:          s.Size,
				IsFolder:      s.IsFolder,
				Mode:          s.Mode,
				DownloadCount: s.DownloadCount,
				MaxDownloads:  s.MaxDownloads,
				ExpiresAt:     s.ExpiresAt,
				CreatedAt:     s.CreatedAt,
			})
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(views)
	}
}

// folderEntryView is the recipient-facing projection of one shared-folder item,
// using the frontend's camelCase contract.
type folderEntryView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Size       *int64     `json:"size"`
	MimeType   string     `json:"mimeType"`
	IsFolder   bool       `json:"isFolder"`
	IsOwn      bool       `json:"isOwn"`
	UploadedAt *time.Time `json:"uploadedAt"`
}

// folderListingView is the recipient-facing projection of a folder path: its
// entries, or a single file's entry (IsFile true) so a file deep-link resolves
// directly.
type folderListingView struct {
	IsFile  bool              `json:"isFile"`
	Entries []folderEntryView `json:"entries"`
	Entry   *folderEntryView  `json:"entry,omitempty"`
}

func toEntryView(e provider.FolderEntry) folderEntryView {
	return folderEntryView{
		ID:         e.ID,
		Name:       e.Name,
		Size:       e.Size,
		MimeType:   e.MimeType,
		IsFolder:   e.IsFolder,
		IsOwn:      e.IsOwn,
		UploadedAt: e.UploadedAt,
	}
}

// handleListFolder browses a shared folder for the recipient. The provider
// resolves the optional path traversal-safely and applies the share's mode, so
// this handler forwards the result without re-deciding visibility. The recipient
// comes from the session, never a request parameter.
func handleListFolder(folders FolderLister, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := auth.SessionFromContext(r.Context())
		if !ok {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		shareID := r.PathValue("shareID")
		path := r.URL.Query().Get("path")

		ctx := provider.ContextWithMeta(r.Context(), provider.MetaFromRequest(r))
		listing, err := folders.ListFolder(ctx, shareID, session.Email, path)
		if err != nil {
			writeShareError(w, err)
			return
		}
		audit.Log(logger, r, slog.LevelInfo, audit.EventListFolder, slog.String("share_id", shareID), slog.String("path", path), slog.String("email", session.Email))

		view := folderListingView{IsFile: listing.IsFile, Entries: make([]folderEntryView, 0, len(listing.Entries))}
		for _, e := range listing.Entries {
			view.Entries = append(view.Entries, toEntryView(e))
		}
		if listing.Entry != nil {
			ev := toEntryView(*listing.Entry)
			view.Entry = &ev
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(view)
	}
}

// writeShareError maps a share-scoped provider error to a recipient-facing status
// without leaking backend detail.
func writeShareError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, provider.ErrShareNotFound):
		http.Error(w, "Share not found", http.StatusNotFound)
	case errors.Is(err, provider.ErrForbidden):
		http.Error(w, "Access denied", http.StatusForbidden)
	case errors.Is(err, provider.ErrShareGone):
		http.Error(w, "Share expired or download limit reached", http.StatusGone)
	case errors.Is(err, provider.ErrUnexpectedStatus):
		http.Error(w, "An error occurred", http.StatusBadGateway)
	default:
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
	}
}
