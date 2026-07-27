// Package tos implements the optional Terms-of-Service consent gate. When enabled
// it loads a Markdown document at startup, derives a content-hash version and
// gates access behind a signed acceptance cookie. When disabled, consent is
// delegated to the identity provider.
package tos

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
)

// acceptanceCookieName is the long-lived cookie recording ToS acceptance.
const acceptanceCookieName = "tos_accepted"

// versionHashLen is the number of hex characters of the content hash used as
// the derived version when no explicit version is configured.
const versionHashLen = 12

// Manager holds the loaded ToS document and everything needed to gate and record
// acceptance. A nil *Manager is the disabled feature and is safe to use.
type Manager struct {
	content     []byte
	contentHash string // hex-encoded SHA-256 of content
	version     string // explicit version or short content-hash prefix

	signingKey    []byte
	secureCookies bool
	logger        *slog.Logger
}

// NewManager loads the ToS document from path and derives the version
// (explicitVersion, else a content-hash prefix). It fails fast on a read error so
// a misconfigured deployment does not start with the gate silently disabled.
func NewManager(path, explicitVersion string, signingKey []byte, secureCookies bool, logger *slog.Logger) (*Manager, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tos file %q: %w", path, err)
	}
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])

	version := explicitVersion
	if version == "" {
		version = hash[:versionHashLen]
	}
	return &Manager{
		content:       content,
		contentHash:   hash,
		version:       version,
		signingKey:    signingKey,
		secureCookies: secureCookies,
		logger:        logger,
	}, nil
}

// Enabled reports whether the consent gate is active; it is nil-safe.
func (m *Manager) Enabled() bool { return m != nil }

// Version returns the ToS version label recorded in audit events.
func (m *Manager) Version() string { return m.version }
