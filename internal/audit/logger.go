package audit

import (
	"io"
	"log/slog"
	"net"
	"net/http"
)

const emailKey = "email"

// Log emits a request-scoped structured record for event at level, combining the
// request attributes (ip, xff, user_agent) with any event-specific extras.
func Log(logger *slog.Logger, r *http.Request, level slog.Level, event string, extra ...slog.Attr) {
	logger.LogAttrs(r.Context(), level, event, append(requestAttrs(r), extra...)...)
}

// requestAttrs returns the attributes common to every request-scoped event (ip,
// xff, user_agent), omitting empty values.
func requestAttrs(r *http.Request) []slog.Attr {
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		ip = host
	}
	attrs := []slog.Attr{slog.String("ip", ip)}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		attrs = append(attrs, slog.String("xff", xff))
	}
	if ua := r.Header.Get("User-Agent"); ua != "" {
		attrs = append(attrs, slog.String("user_agent", ua))
	}
	return attrs
}

// New builds the application logger writing JSON to w. Audit records always pass;
// technical records are filtered by level. Every record's "email" attribute is
// pseudonymized so no address is ever logged in clear (unless pseudonymize is
// false); salt optionally strengthens the hash.
func New(w io.Writer, level slog.Level, salt []byte, pseudonymize bool) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: replaceAttr(salt, pseudonymize),
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

// replaceAttr returns the slog ReplaceAttr hook enforcing the two logging
// policies: email pseudonymization and the AUDIT level name.
func replaceAttr(salt []byte, pseudonymize bool) func([]string, slog.Attr) slog.Attr {
	return func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == slog.LevelKey {
			if level, ok := a.Value.Any().(slog.Level); ok && level == LevelAudit {
				a.Value = slog.StringValue("AUDIT")
			}
			return a
		}
		if pseudonymize && len(groups) == 0 && a.Key == emailKey {
			a.Value = slog.StringValue(hashEmail(salt, a.Value.String()))
		}
		return a
	}
}
