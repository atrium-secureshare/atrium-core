# Architecture & security invariants

This document records the security-relevant design decisions that must not
regress. For how to configure and run the core, see
[configuration.md](configuration.md).

The core is a Go service (stdlib-first: `net/http`, `log/slog`) that serves a
React/TypeScript SPA and a JSON API, authenticates external recipients over
OIDC, and streams files to/from a storage plugin. It depends on a single
backend-neutral contract, `provider.Service`; consumers take a narrow view of it
(`internal/api`'s per-capability interfaces, `internal/proxy.Streamer`). The
concrete backend is selected at startup by the required `PROVIDER_TYPE`,
so the core stays storage-agnostic: a new backend is one file in
`internal/provider` plus one `switch` case in `main.go`, never a change to the
core's own logic.

A backend is a thin constructor over the shared REST client (`internal/provider`,
`client.go`), which speaks the Atrium plugin REST API and mints the trust token.
The trust audience (`aud`) is a per-backend field, set by the constructor
(`NewNextcloud` pins `atrium-plugin-nextcloud`), not a shared constant — so each
backend can require its own identity. A backend whose wire calls differ embeds
`*client` and overrides only the diverging method; the `provider.Service` value
the core holds dispatches dynamically to that override.

## Routing boundary

`api.NewRouter` makes the security boundary the `/api/` subtree, not `/`:

- **Public:** `GET /healthz`, `GET /readyz` (plugin-trust readiness), `/auth/*`,
  and the static SPA shell and its assets under `/`. Assets load with no per-file
  exceptions.
- **Gated (`/api/*`):** mounted once as `RequireAuth → RequireTOS`, so every
  route added under `/api/` is deny-by-default. A valid session alone never
  reaches it, and the gate cannot be forgotten per route. Responses are JSON
  (`401 unauthenticated`, `403 tos_required`).
- **Pre-consent exceptions:** `GET /api/tos` and `POST /api/tos/accept` are the
  only two endpoints behind auth but in front of the consent gate. Without
  them a recipient could never reach consent. Nothing else is excepted.

## Authentication & sessions

Auth uses the OIDC authorization-code flow with PKCE (S256) and a confidential
client; sessions are stateless, HMAC-SHA256-signed cookies (no server state).
Required values are validated at startup. The process fails fast if any is
missing or malformed. Authentication endpoints are grouped under `/auth/*`
(`/auth/login`, `/auth/callback`, `/auth/logout`); `OIDC_REDIRECT_URI` must
point at `/auth/callback`.

## Security headers

A single middleware (`internal/api/security.go`) wraps the whole handler, so
every response (shell, API and streamed downloads alike) carries the same
hardening headers and none can be forgotten per route:
`X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
`Referrer-Policy: no-referrer`, a locked-down `Content-Security-Policy`, and,
only when the deployment is served over TLS (`SecureCookies`, passed to
`api.Handler`), `Strict-Transport-Security` (one year, `includeSubDomains`).
HSTS is gated on the transport because it is meaningless (and undesirable) over
plain http, e.g. local `go run`.

The CSP is `default-src 'self'` hardened with `object-src 'none'`,
`base-uri 'none'` and `frame-ancestors 'none'` (the anti-clickjacking anchor
alongside `X-Frame-Options`). Everything the app loads is same-origin (bundle,
CSS, bundled fonts, `/branding/` logos, the JSON API), so no external origins
are allowed. `script-src` stays free of `'unsafe-inline'`: `webui.Handler`
returns the SHA-256 hashes of the shell's inline `<script>` blocks (the pre-paint
theme script and, when a brand is set, the injected `window.__ATRIUM__` script),
which the CSP lists as `'sha256-...'` sources. The hashes are derived from the
actually served `index.html` at startup, so they can never drift from the
markup. `style-src` keeps `'unsafe-inline'` because the app sets dynamic style
attributes (e.g. the upload progress width) that no static hash can cover, and
the accent override is injected as an inline `<style>`.

The `window.__ATRIUM__` object is placed right after `<head>`, so it exists
before the pre-paint theme script and the app bundle; only set values are emitted
(`omitempty`), and the JSON is produced by `encoding/json`, whose default HTML
escaping renders `<`/`>`/`&` as `\uXXXX`, so a brand value containing
`</script>` cannot break out of the tag. `BRAND_ACCENT_COLOR` is validated as a
hex colour at startup, which keeps it safe to interpolate into the injected
style.

## Audit trail

An audit event is an ordinary structured log record, emitted at
`audit.LevelAudit` (message = event name, e.g. `download-start`). There is no
separate logger: `audit.New` builds the one `*slog.Logger` used everywhere, so
audit and technical records share a single JSON stream. Audit records always
pass; technical records are filtered by `LOG_LEVEL`. Call sites emit via
`audit.Log(logger, r, level, event, attrs...)`, which attaches the request
context (ip, xff, user_agent); the explicit level makes each call site declare
whether it is an audit record (`audit.LevelAudit`) or an operational one
(`slog.LevelInfo`).

The audit trail (`LevelAudit`): login, login-failed, accept-tos, download-start,
upload-start, access-denied, share-expired. Logged operationally at INFO instead
(subject to `LOG_LEVEL`, off the trail): logout, the listings (list-shares,
list-folder) and the transfer completions (download-complete, upload-complete).
A listing is not data access, and a completion only annotates its start event
with the bytes transferred. An operator who needs listings in the WORM trail
runs `LOG_LEVEL=info` and forwards the whole stream, not only the `AUDIT` lines.
Atrium keeps no audit database; revision-safety is an operational concern
(forward the stream to a WORM target), not Atrium's job.

Two policies are enforced centrally in the handler (`ReplaceAttr`), so no call
site can bypass them: any `email` attribute is pseudonymized (NFKC-normalized,
lower-cased, SHA-256, 16 hex chars, at **every** level; technical logs never
carry a plaintext address either), and `LevelAudit` renders as `"AUDIT"`.
`AUDIT_SALT` strengthens the hash against enumeration (at the cost of losing
lookups for already-logged addresses if the salt is lost);
`AUDIT_PSEUDONYMIZE=false` disables hashing for local development. The ToS
acceptance cookie shares `audit.Canonical` for its own (full-length) binding
hash.

## Core ↔ plugin trust boundary

The core never exposes the storage backend; it reaches the storage plugin
(`internal/provider`) as a trusted client. Every request carries a fresh,
per-request **ES256** JWT the core signs with `PROVIDER_JWT_PRIVATE_KEY`; the
plugin verifies it against the matching public key (algorithm-pinned to ES256,
so HS256/`none` downgrades are structurally impossible). Tokens are short-lived
(30s), pin `iss=atrium-core` and a per-backend `aud` (the Nextcloud backend uses
`aud=atrium-plugin-nextcloud`), and carry the
action plus (where applicable) `share_id`, `email`, and `ip`/`xff` audit
context. Empty claims are omitted (e.g. `list-shares` has no `share_id`). Plugin
error statuses map to sentinels (`403 → ErrForbidden`, `404 → ErrShareNotFound`)
without leaking the backend response. Actions: `list-shares`, `download`,
`list-folder`, `download-file`, `upload`, `healthcheck`.

Folder shares surface under `/api/shares/{id}/folder` (mode-filtered browsing),
`/api/shares/{id}/folder/{fileID}/content` (child download, streamed through the
proxy) and `/api/shares/{id}/upload` (upload). The `/folder` and `/upload`
endpoints take an optional `?path=` query naming a relative sub-folder (or file)
within the share; the plugin resolves it traversal-safely (a `..` escape or an
id outside the folder is a clean 404), so recipients browse the tree recursively
with the share token as the only access anchor. Sub-folders carry no token of
their own. `/folder` answers a self-describing object: `{isFile:false,entries}`
for a folder, `{isFile:true,entry}` when the path points at a file (so a file
deep-link resolves in one call). The recipient view carries `mode`
(0=read-only, 1=write/read-own, 2=write/read-all, 3=dropzone), an explicit
sharing mode, not a permission bitmask; the frontend derives visibility and
upload affordances from it. The plugin applies the mode, so the core forwards
its listing verbatim.

The one-time key exchange that anchors this boundary is described in
[configuration.md](configuration.md#provider-trust-setup-one-time-manual).
