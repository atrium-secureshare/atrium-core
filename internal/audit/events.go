// Package audit configures the application's structured logger so audit events
// and technical logs share one slog.Logger and one JSON stream. Emails are
// pseudonymized centrally (see New) so none reaches the log in clear.
package audit

import "log/slog"

// LevelAudit is the slog level for audit records; it sits above slog.LevelError
// so a LOG_LEVEL floor never suppresses it.
const LevelAudit slog.Level = slog.LevelError + 4

// Audit event names, used as the log message of a LevelAudit record. This block
// is the single catalog of what the audit trail contains.
const (
	EventLogin         = "login"
	EventLoginFailed   = "login-failed"
	EventAcceptTOS     = "accept-tos"
	EventDownloadStart = "download-start"
	EventUploadStart   = "upload-start"
	EventAccessDenied  = "access-denied"
	EventShareExpired  = "share-expired"
)

// Operational event names: emitted at slog.LevelInfo, not audit records, so they
// stay out of the compliance trail — a listing is not data access, and a
// completion only annotates its start event with the bytes transferred.
const (
	EventLogout           = "logout"
	EventListShares       = "list-shares"
	EventListFolder       = "list-folder"
	EventDownloadComplete = "download-complete"
	EventUploadComplete   = "upload-complete"
)
