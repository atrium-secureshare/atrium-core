package tos

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func writeTOS(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tos.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write tos file: %v", err)
	}
	return path
}

func TestNewManagerLoadsAndHashes(t *testing.T) {
	const content = "# Terms\n\nBe nice.\n"
	path := writeTOS(t, content)

	m, err := NewManager(path, "", []byte("0123456789abcdef0123456789abcdef"), true, discardLogger())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	sum := sha256.Sum256([]byte(content))
	wantHash := hex.EncodeToString(sum[:])
	if m.contentHash != wantHash {
		t.Errorf("contentHash = %q, want %q", m.contentHash, wantHash)
	}
	if want := wantHash[:versionHashLen]; m.Version() != want {
		t.Errorf("Version() = %q, want short hash %q", m.Version(), want)
	}
	if !m.Enabled() {
		t.Error("Enabled() = false, want true")
	}
}

func TestNewManagerExplicitVersionWins(t *testing.T) {
	m, err := NewManager(writeTOS(t, "terms"), "2026-01", nil, false, discardLogger())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if m.Version() != "2026-01" {
		t.Errorf("Version() = %q, want %q", m.Version(), "2026-01")
	}
}

func TestNewManagerMissingFile(t *testing.T) {
	if _, err := NewManager(filepath.Join(t.TempDir(), "absent.md"), "", nil, false, discardLogger()); err == nil {
		t.Fatal("expected error for missing tos file")
	}
}

func TestDisabledManagerIsNilSafe(t *testing.T) {
	var m *Manager
	if m.Enabled() {
		t.Error("nil Manager Enabled() = true, want false")
	}
}
