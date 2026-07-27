package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func logRecord(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("log line is not JSON: %v (%q)", err, buf.String())
	}
	return rec
}

func newLogger(t *testing.T, level slog.Level, pseudonymize bool) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return New(&buf, level, nil, pseudonymize), &buf
}

func TestAuditRecordShape(t *testing.T) {
	logger, buf := newLogger(t, slog.LevelInfo, true)

	logger.LogAttrs(context.Background(), LevelAudit, EventDownloadStart,
		slog.String("share_id", "s1"), slog.String("email", "user@example.com"))

	rec := logRecord(t, buf)
	if rec["level"] != "AUDIT" {
		t.Errorf("level = %v, want AUDIT", rec["level"])
	}
	if rec["msg"] != EventDownloadStart {
		t.Errorf("msg = %v, want %q", rec["msg"], EventDownloadStart)
	}
	if rec["share_id"] != "s1" {
		t.Errorf("share_id = %v, want s1", rec["share_id"])
	}
}

func TestLogAttachesRequestContext(t *testing.T) {
	t.Run("full context, ip host without port", func(t *testing.T) {
		logger, buf := newLogger(t, slog.LevelInfo, false)
		r := httptest.NewRequest(http.MethodGet, "/api/shares/s1/content", nil)
		r.RemoteAddr = "203.0.113.5:44321"
		r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
		r.Header.Set("User-Agent", "Mozilla/5.0")

		Log(logger, r, LevelAudit, EventDownloadStart, slog.String("share_id", "s1"))

		rec := logRecord(t, buf)
		if rec["ip"] != "203.0.113.5" {
			t.Errorf("ip = %v, want 203.0.113.5 (host without port)", rec["ip"])
		}
		if rec["xff"] != "203.0.113.5, 10.0.0.1" {
			t.Errorf("xff = %v", rec["xff"])
		}
		if rec["user_agent"] != "Mozilla/5.0" {
			t.Errorf("user_agent = %v", rec["user_agent"])
		}
		// The call site's own attrs ride alongside the request context.
		if rec["share_id"] != "s1" {
			t.Errorf("share_id = %v, want s1", rec["share_id"])
		}
	})

	t.Run("absent xff and user agent are omitted", func(t *testing.T) {
		logger, buf := newLogger(t, slog.LevelInfo, false)
		r := httptest.NewRequest(http.MethodGet, "/api/shares/s1/content", nil)
		r.RemoteAddr = "203.0.113.5:44321"

		Log(logger, r, LevelAudit, EventDownloadStart)

		rec := logRecord(t, buf)
		if rec["ip"] != "203.0.113.5" {
			t.Errorf("ip = %v, want 203.0.113.5", rec["ip"])
		}
		if _, ok := rec["xff"]; ok {
			t.Error("xff should be omitted when absent")
		}
		if _, ok := rec["user_agent"]; ok {
			t.Error("user_agent should be omitted when absent")
		}
	})
}

// TestEmailPseudonymizationPolicy exercises the replaceAttr email branch. With
// pseudonymization on, an email attribute is hashed at every level, since the
// policy is global rather than audit-only: an audit record and an ordinary
// technical record are both pseudonymized. With it off the address passes
// through in clear.
func TestEmailPseudonymizationPolicy(t *testing.T) {
	const email = "user@example.com"
	tests := []struct {
		name         string
		level        slog.Level
		pseudonymize bool
		want         string
	}{
		{"audit record is pseudonymized", LevelAudit, true, hashEmail(nil, email)},
		{"technical record is pseudonymized too", slog.LevelInfo, true, hashEmail(nil, email)},
		{"disabled passes email through", LevelAudit, false, email},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger, buf := newLogger(t, slog.LevelInfo, tc.pseudonymize)
			logger.LogAttrs(context.Background(), tc.level, "record", slog.String("email", email))
			if got := logRecord(t, buf)["email"]; got != tc.want {
				t.Errorf("email = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLogLevelFiltersTechnicalButNotAudit(t *testing.T) {
	logger, buf := newLogger(t, slog.LevelWarn, true)

	logger.Info("filtered out")
	if buf.Len() != 0 {
		t.Fatalf("info record should be filtered at LevelWarn, got %q", buf.String())
	}

	logger.LogAttrs(context.Background(), LevelAudit, EventLogin)
	if buf.Len() == 0 {
		t.Error("audit record must pass regardless of the level floor")
	}
}
