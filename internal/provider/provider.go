// Package provider is the storage-agnostic seam between the core and a storage
// backend. Service is the one contract the core depends on; a concrete backend is
// a thin constructor over the shared REST client (see client.go, nextcloud.go).
// Every request carries a short-lived ES256 JWT the backend verifies against the
// core's public key, so the backend only acts on calls the core signed.
package provider

import (
	"context"
	"errors"
	"io"
	"time"
)

// Service is the contract a storage backend fulfils for the core: listing and
// browsing shares, streaming transfers, and the trust healthcheck. It is the
// single seam a new backend implements — the applied Adapter pattern, though no
// identifier here is named "adapter". The core holds a Service value, so a backend
// that overrides one method dispatches dynamically without the core changing.
type Service interface {
	// ListShares returns the shares the backend holds for the recipient.
	ListShares(ctx context.Context, email string) ([]Share, error)
	// ListFolder browses a shared folder at a relative path (empty for the share
	// root), mode-filtered and traversal-resolved by the backend.
	ListFolder(ctx context.Context, shareID, email, path string) (*FolderListing, error)
	// Download opens a share's file content for streaming; the caller closes
	// Content.Body.
	Download(ctx context.Context, shareID, email string) (*Content, error)
	// DownloadFolderFile opens one file within a shared folder for streaming; the
	// caller closes Content.Body.
	DownloadFolderFile(ctx context.Context, shareID, fileID, email string) (*Content, error)
	// Upload streams a file body into a share at a relative path (empty for the
	// share root).
	Upload(ctx context.Context, shareID, email, filename, contentType, path string, body io.Reader) error
	// HealthCheck verifies the trust relationship with the backend.
	HealthCheck(ctx context.Context) error
	// PublicKeyPEM returns the core signing key's public half, backend-independent,
	// for the trust-setup flow (the operator installs it on the backend).
	PublicKeyPEM() string
}

// Errors returned by Service methods, mapping backend HTTP responses to core-side
// conditions. Response bodies are never surfaced, so backend internals cannot leak
// to recipients.
var (
	// ErrForbidden maps a 403 from the backend: the trust relationship or an
	// authorization check (email/share binding) was rejected.
	ErrForbidden = errors.New("provider denied the request")
	// ErrShareNotFound maps a 404: the share does not exist or was revoked.
	ErrShareNotFound = errors.New("share not found")
	// ErrShareGone maps a 410 (expired or download limit reached), kept distinct
	// from a 404 so the proxy can tell the recipient the difference.
	ErrShareGone = errors.New("share expired or download limit reached")
	// ErrTrustNotConfigured is returned by HealthCheck when the backend rejects the
	// signed healthcheck, i.e. the core public key is not installed there.
	ErrTrustNotConfigured = errors.New("provider trust relationship not configured")
	// ErrUnexpectedStatus is returned for any other non-2xx backend response.
	ErrUnexpectedStatus = errors.New("unexpected provider response")
)

// Content is a streamed file body from the backend together with the headers a
// download response needs. Body must be closed by the caller.
type Content struct {
	Body               io.ReadCloser
	ContentType        string
	ContentDisposition string
	// ContentLength is the body size in bytes, or -1 when the backend declared
	// none (the proxy then streams with chunked transfer encoding).
	ContentLength int64
}

// Share is the core-side view of a share; only the fields the core acts on or
// forwards to the recipient UI are decoded.
type Share struct {
	ID             string `json:"id"`
	RecipientEmail string `json:"recipient_email"`
	DisplayName    string `json:"display_name,omitempty"`
	FileName       string `json:"file_name,omitempty"`
	Size           *int64 `json:"size,omitempty"`
	IsFolder       bool   `json:"is_folder"`
	// Mode is the sharing mode (0=read-only, 1=write/read-own, 2=write/read-all,
	// 3=dropzone), not a permission bitmask. The recipient UI derives visibility
	// and upload affordances from it.
	Mode          int        `json:"mode"`
	MaxDownloads  *int       `json:"max_downloads,omitempty"`
	DownloadCount int        `json:"download_count"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	CreatedAt     *time.Time `json:"created_at,omitempty"`
}

// FolderListing is the backend's response for a folder path: a folder's
// mode-filtered entries, or a single file's entry (IsFile true) so a file
// deep-link resolves in one call.
type FolderListing struct {
	IsFile  bool          `json:"is_file"`
	Entries []FolderEntry `json:"entries"`
	Entry   *FolderEntry  `json:"entry"`
}

// FolderEntry is one item inside a shared folder as the backend reports it. The
// backend has already applied the share's mode, so the core forwards it verbatim.
type FolderEntry struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Size       *int64     `json:"size,omitempty"`
	MimeType   string     `json:"mime_type,omitempty"`
	IsFolder   bool       `json:"is_folder"`
	IsOwn      bool       `json:"is_own"`
	UploadedAt *time.Time `json:"uploaded_at,omitempty"`
}
