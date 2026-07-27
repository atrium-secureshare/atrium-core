// Package proxy streams file downloads and uploads between recipients and the
// storage provider without buffering in the core. It depends on a backend-neutral
// Streamer interface (its narrow view of provider.Service), so the core stays
// storage-agnostic.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/atrium-secureshare/atrium-core/internal/audit"
	"github.com/atrium-secureshare/atrium-core/internal/auth"
	"github.com/atrium-secureshare/atrium-core/internal/provider"
)

// Streamer is the backend-neutral view the proxy needs of the storage provider:
// the file-transfer operations it streams. It is the proxy's narrow slice of
// provider.Service; keeping it an interface decouples the proxy from the backend
// and lets tests substitute a fake.
type Streamer interface {
	Download(ctx context.Context, shareID, email string) (*provider.Content, error)
	DownloadFolderFile(ctx context.Context, shareID, fileID, email string) (*provider.Content, error)
	Upload(ctx context.Context, shareID, email, filename, contentType, path string, body io.Reader) error
}

// Proxy holds the collaborators for the stream-proxy handlers.
type Proxy struct {
	provider      Streamer
	maxUploadSize int64
	logger        *slog.Logger
}

// New builds a Proxy that enforces maxUploadSize on uploads and emits transfer
// audit events through logger.
func New(s Streamer, maxUploadSize int64, logger *slog.Logger) *Proxy {
	return &Proxy{provider: s, maxUploadSize: maxUploadSize, logger: logger}
}

// HandleDownload streams a share's content to the recipient without buffering.
// The core holds no state, so a mid-stream disconnect needs no rollback.
func (p *Proxy) HandleDownload(w http.ResponseWriter, r *http.Request) {
	shareID := r.PathValue("shareID")
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ctx := provider.ContextWithMeta(r.Context(), provider.MetaFromRequest(r))
	content, err := p.provider.Download(ctx, shareID, session.Email)
	if err != nil {
		p.writeProviderError(w, r, "download", shareID, session.Email, err)
		return
	}
	p.streamContent(w, r, shareID, session.Email, content)
}

// HandleFolderFileDownload streams one file from within a shared folder; the
// provider enforces the share's mode, so a denied child maps to the same statuses
// as any other download.
func (p *Proxy) HandleFolderFileDownload(w http.ResponseWriter, r *http.Request) {
	shareID := r.PathValue("shareID")
	fileID := r.PathValue("fileID")
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ctx := provider.ContextWithMeta(r.Context(), provider.MetaFromRequest(r))
	content, err := p.provider.DownloadFolderFile(ctx, shareID, fileID, session.Email)
	if err != nil {
		p.writeProviderError(w, r, "download", shareID, session.Email, err)
		return
	}
	p.streamContent(w, r, shareID, session.Email, content, slog.String("file_id", fileID))
}

// streamContent forwards a provider content body to the recipient without
// buffering, emitting download-start up front and download-complete on success.
// It closes content.Body.
func (p *Proxy) streamContent(w http.ResponseWriter, r *http.Request, shareID, email string, content *provider.Content, extra ...slog.Attr) {
	defer content.Body.Close()

	audit.Log(p.logger, r, audit.LevelAudit, audit.EventDownloadStart, append(shareAttrs(shareID, email), extra...)...)

	if content.ContentType != "" {
		w.Header().Set("Content-Type", content.ContentType)
	}
	if content.ContentDisposition != "" {
		w.Header().Set("Content-Disposition", content.ContentDisposition)
	}
	if content.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(content.ContentLength, 10))
	}

	// Headers are committed on the first write, so once copying starts an error can
	// only be logged (almost always the recipient disconnecting).
	n, err := io.Copy(w, content.Body)
	if err != nil {
		p.logger.Warn("download stream interrupted", "share_id", shareID, "error", err)
		return
	}
	audit.Log(p.logger, r, slog.LevelInfo, audit.EventDownloadComplete, append(append(shareAttrs(shareID, email), extra...), slog.Int64("file_size", n))...)
}

// HandleUpload streams an uploaded file into a share. The size limit is enforced
// twice: cheaply from Content-Length, and authoritatively with a MaxBytesReader
// that aborts a chunked or under-declared body. The body is not buffered.
func (p *Proxy) HandleUpload(w http.ResponseWriter, r *http.Request) {
	shareID := r.PathValue("shareID")
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if r.ContentLength > p.maxUploadSize {
		p.tooLarge(w)
		return
	}
	filename := r.Header.Get("X-Atrium-Filename")
	if filename == "" {
		http.Error(w, "missing X-Atrium-Filename header", http.StatusBadRequest)
		return
	}
	path := r.URL.Query().Get("path")

	startAttrs := append(shareAttrs(shareID, session.Email), slog.String("file_name", filename))
	if r.ContentLength >= 0 {
		startAttrs = append(startAttrs, slog.Int64("file_size", r.ContentLength))
	}
	audit.Log(p.logger, r, audit.LevelAudit, audit.EventUploadStart, startAttrs...)

	// Count the bytes actually streamed so the completion event reports the real
	// size, not the declared Content-Length.
	body := &countingReader{r: http.MaxBytesReader(w, r.Body, p.maxUploadSize)}
	ctx := provider.ContextWithMeta(r.Context(), provider.MetaFromRequest(r))
	if err := p.provider.Upload(ctx, shareID, session.Email, filename, r.Header.Get("Content-Type"), path, body); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			p.tooLarge(w)
			return
		}
		p.writeProviderError(w, r, "upload", shareID, session.Email, err)
		return
	}

	audit.Log(p.logger, r, slog.LevelInfo, audit.EventUploadComplete,
		append(shareAttrs(shareID, session.Email), slog.String("file_name", filename), slog.Int64("file_size", body.n))...)
	w.WriteHeader(http.StatusCreated)
}

// writeProviderError maps a provider error to a recipient-facing status without
// leaking backend detail. Known share conditions are audited as denied access;
// an unexpected status (500) or transport failure (503) is an operator fault, so
// only logged technically.
func (p *Proxy) writeProviderError(w http.ResponseWriter, r *http.Request, op, shareID, email string, err error) {
	switch {
	case errors.Is(err, provider.ErrShareNotFound):
		audit.Log(p.logger, r, audit.LevelAudit, audit.EventAccessDenied, append(shareAttrs(shareID, email), slog.String("reason", "share_not_found"))...)
		http.Error(w, "Share not found", http.StatusNotFound)
	case errors.Is(err, provider.ErrForbidden):
		audit.Log(p.logger, r, audit.LevelAudit, audit.EventAccessDenied, append(shareAttrs(shareID, email), slog.String("reason", "forbidden"))...)
		http.Error(w, "Access denied", http.StatusForbidden)
	case errors.Is(err, provider.ErrShareGone):
		audit.Log(p.logger, r, audit.LevelAudit, audit.EventShareExpired, shareAttrs(shareID, email)...)
		http.Error(w, "Share expired or download limit reached", http.StatusGone)
	case errors.Is(err, provider.ErrUnexpectedStatus):
		p.logger.Error("provider "+op+" failed", "share_id", shareID, "error", err)
		http.Error(w, "An error occurred", http.StatusInternalServerError)
	default:
		p.logger.Error("provider "+op+" unreachable", "share_id", shareID, "error", err)
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
	}
}

// tooLarge answers a rejected oversize upload with 413 and the configured limit.
func (p *Proxy) tooLarge(w http.ResponseWriter) {
	http.Error(w, fmt.Sprintf("File too large. Maximum: %d bytes", p.maxUploadSize), http.StatusRequestEntityTooLarge)
}

// shareAttrs returns the audit attributes common to every share-scoped event.
func shareAttrs(shareID, email string) []slog.Attr {
	return []slog.Attr{slog.String("share_id", shareID), slog.String("email", email)}
}

// countingReader wraps a reader and records how many bytes were read, so an
// upload can be audited with the number of bytes actually streamed.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
