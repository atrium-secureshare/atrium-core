package provider

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// Trust-boundary constants shared with the backend's JWT validator, which pins
// the same issuer. The audience is per-backend and lives on the client (see the
// audience field) rather than here, so each backend can require its own aud.
const (
	tokenIssuer = "atrium-core"
	// tokenTTL is deliberately short: a token is in flight for a single hop, so a
	// tight window bounds replay without needing revocation.
	tokenTTL = 30 * time.Second
)

// client is the shared REST base every backend uses to reach a storage provider
// speaking the Atrium plugin REST API, signing each request with a fresh ES256
// JWT. Control-plane calls use a short-timeout HTTP client; file transfers use a
// separate one with a generous timeout. A backend is a thin constructor over this
// base (see nextcloud.go); one with a differing method embeds *client and
// overrides it. It implements Service.
type client struct {
	baseURL      string
	signer       jose.Signer
	httpClient   *http.Client
	streamClient *http.Client
	logger       *slog.Logger
	publicKeyPEM string
	// audience is the aud claim minted into every token, the trust identity the
	// backend's validator pins. Set once per backend by the constructor.
	audience string
}

// newClient builds the shared REST base for the backend at baseURL with the given
// audience, signing requests with the P-256 key from privateKeyPEM. It fails fast
// on a non-absolute URL or unusable key. Backends call it from their constructor
// rather than exposing it, so a backend is always reached through its typed entry
// point (see NewNextcloud).
func newClient(baseURL, privateKeyPEM, audience string, streamTimeout time.Duration, logger *slog.Logger) (*client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || !u.IsAbs() {
		return nil, fmt.Errorf("provider base URL must be absolute: %q", baseURL)
	}

	key, err := LoadPrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	signer, err := newSigner(key)
	if err != nil {
		return nil, err
	}
	pub, err := PublicKeyPEM(key)
	if err != nil {
		return nil, err
	}

	return &client{
		baseURL:      u.String(),
		signer:       signer,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		streamClient: &http.Client{Timeout: streamTimeout},
		logger:       logger,
		publicKeyPEM: pub,
		audience:     audience,
	}, nil
}

// newSigner builds an ES256 JWT signer for key.
func newSigner(key *ecdsa.PrivateKey) (jose.Signer, error) {
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		return nil, fmt.Errorf("create provider JWT signer: %w", err)
	}
	return signer, nil
}

// PublicKeyPEM returns the PKIX/SPKI PEM of the signing key's public half.
func (c *client) PublicKeyPEM() string { return c.publicKeyPEM }

// createToken mints a fresh, short-lived ES256 JWT for a single provider call.
// Empty optional claims are omitted (e.g. list-shares has no share_id).
func (c *client) createToken(shareID, email, action string, meta ClientMeta) (string, error) {
	now := time.Now()
	claims := map[string]any{
		"iss":    tokenIssuer,
		"aud":    c.audience,
		"iat":    now.Unix(),
		"exp":    now.Add(tokenTTL).Unix(),
		"action": action,
	}
	if shareID != "" {
		claims["share_id"] = shareID
	}
	if email != "" {
		claims["email"] = email
	}
	if meta.IP != "" {
		claims["ip"] = meta.IP
	}
	if meta.XFF != "" {
		claims["xff"] = meta.XFF
	}

	token, err := jwt.Signed(c.signer).Claims(claims).Serialize()
	if err != nil {
		return "", fmt.Errorf("sign provider token: %w", err)
	}
	return token, nil
}

// request signs and performs a request for action. On 2xx it returns the response
// with the body open (caller closes it); a non-2xx is mapped to a sentinel error
// and the body closed, so the provider's body never reaches callers.
func (c *client) request(ctx context.Context, client *http.Client, method, path, shareID, email, action string, body io.Reader, headers map[string]string) (*http.Response, error) {
	token, err := c.createToken(shareID, email, action, MetaFromContext(ctx))
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("build provider request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call provider %s %s: %w", method, path, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	status := resp.StatusCode
	resp.Body.Close()
	return nil, mapErrorStatus(status)
}

// do performs a control-plane request (no body) on the short-timeout client.
func (c *client) do(ctx context.Context, method, path, shareID, email, action string) (*http.Response, error) {
	return c.request(ctx, c.httpClient, method, path, shareID, email, action, nil, nil)
}

// mapErrorStatus maps a non-2xx provider status to a sentinel error, so backend
// internals never leak to recipients.
func mapErrorStatus(status int) error {
	switch status {
	case http.StatusForbidden:
		return ErrForbidden
	case http.StatusNotFound:
		return ErrShareNotFound
	case http.StatusGone:
		return ErrShareGone
	default:
		return fmt.Errorf("%w: %d", ErrUnexpectedStatus, status)
	}
}

// Download opens the file content for a share. It streams over the long-timeout
// client so large transfers are not cut short; the caller closes Content.Body.
func (c *client) Download(ctx context.Context, shareID, email string) (*Content, error) {
	resp, err := c.request(ctx, c.streamClient, http.MethodGet,
		"/api/v1/shares/"+url.PathEscape(shareID)+"/content", shareID, email, "download", nil, nil)
	if err != nil {
		return nil, err
	}
	return &Content{
		Body:               resp.Body,
		ContentType:        resp.Header.Get("Content-Type"),
		ContentDisposition: resp.Header.Get("Content-Disposition"),
		ContentLength:      resp.ContentLength,
	}, nil
}

// Upload streams a file body into a share at the given relative path (empty for
// the share root). The body is sent chunked, so nothing is buffered in the core;
// the provider authorizes and resolves the target traversal-safely.
func (c *client) Upload(ctx context.Context, shareID, email, filename, contentType, path string, body io.Reader) error {
	p := "/api/v1/shares/" + url.PathEscape(shareID) + "/upload"
	if path != "" {
		p += "?path=" + url.QueryEscape(path)
	}
	resp, err := c.request(ctx, c.streamClient, http.MethodPost,
		p, shareID, email, "upload", body, map[string]string{
			"Content-Type":      contentType,
			"X-Atrium-Filename": filename,
		})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ListFolder browses a shared folder at the given relative path (empty for the
// share root). The provider applies the mode and resolves the path
// traversal-safely, so the core forwards the result; a file path comes back with
// IsFile true.
func (c *client) ListFolder(ctx context.Context, shareID, email, path string) (*FolderListing, error) {
	p := "/api/v1/shares/" + url.PathEscape(shareID) + "/folder"
	if path != "" {
		p += "?path=" + url.QueryEscape(path)
	}
	resp, err := c.do(ctx, http.MethodGet, p, shareID, email, "list-folder")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var listing FolderListing
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		return nil, fmt.Errorf("decode provider folder listing: %w", err)
	}
	return &listing, nil
}

// DownloadFolderFile opens one file within a shared folder for streaming; the
// provider enforces child ownership. The caller closes Content.Body.
func (c *client) DownloadFolderFile(ctx context.Context, shareID, fileID, email string) (*Content, error) {
	resp, err := c.request(ctx, c.streamClient, http.MethodGet,
		"/api/v1/shares/"+url.PathEscape(shareID)+"/folder/"+url.PathEscape(fileID)+"/content",
		shareID, email, "download-file", nil, nil)
	if err != nil {
		return nil, err
	}
	return &Content{
		Body:               resp.Body,
		ContentType:        resp.Header.Get("Content-Type"),
		ContentDisposition: resp.Header.Get("Content-Disposition"),
		ContentLength:      resp.ContentLength,
	}, nil
}

// ListShares returns the shares the provider holds for the given recipient.
func (c *client) ListShares(ctx context.Context, email string) ([]Share, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/v1/shares", "", email, "list-shares")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var shares []Share
	if err := json.NewDecoder(resp.Body).Decode(&shares); err != nil {
		return nil, fmt.Errorf("decode provider shares: %w", err)
	}
	return shares, nil
}

// HealthCheck sends a signed healthcheck to verify the trust relationship. A 403
// means the provider lacks the core public key: the error wraps
// ErrTrustNotConfigured and includes the key to install.
func (c *client) HealthCheck(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "/api/v1/health", "", "", "healthcheck")
	if err != nil {
		if errors.Is(err, ErrForbidden) {
			return fmt.Errorf("%w; install this core public key on the provider:\n%s",
				ErrTrustNotConfigured, c.publicKeyPEM)
		}
		return err
	}
	resp.Body.Close()
	return nil
}
