package tos

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/atrium-secureshare/atrium-core/internal/audit"
	"github.com/atrium-secureshare/atrium-core/internal/auth"
)

// Consent API paths, behind authentication but in front of the consent gate so a
// recipient can fetch the terms and record acceptance while otherwise blocked.
const (
	// ContentPath serves the ToS document so the frontend can render the overlay.
	ContentPath = "/api/tos"
	// AcceptPath records acceptance.
	AcceptPath = "/api/tos/accept"
)

// RequireTOS gates the wrapped handler behind ToS acceptance. It must run after
// authentication so the session is in context; an un-accepted ToS yields 403
// tos_required. A nil *Manager (disabled) returns next unchanged.
func (m *Manager) RequireTOS(next http.Handler) http.Handler {
	if !m.Enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := auth.SessionFromContext(r.Context())
		if !ok {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if !m.CheckAcceptance(r, session.Email) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "tos_required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ContentHandler returns the current ToS document and version. It requires
// authentication but not prior acceptance.
func (m *Manager) ContentHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version": m.version,
		"content": string(m.content),
	})
}

// AcceptHandler records the recipient's ToS acceptance: it audits, sets the
// acceptance cookie and answers 204. It relies on the auth middleware for the
// session and must not be placed behind RequireTOS.
func (m *Manager) AcceptHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	audit.Log(m.logger, r, audit.LevelAudit, audit.EventAcceptTOS, slog.String("email", session.Email), slog.String("tos_version", m.version))

	if err := m.setAcceptanceCookie(w, session.Email, time.Now()); err != nil {
		m.logger.Error("set tos cookie", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeJSON writes v as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
