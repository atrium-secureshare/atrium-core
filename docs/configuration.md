# Configuration & operations

Atrium Core is configured entirely through environment variables and fails fast
at startup if a required value is missing or malformed. This document is the
operator reference; for the security design behind these settings see
[architecture.md](architecture.md).

## Environment variables

| Variable             | Default | Purpose                                                  |
| -------------------- | ------- | -------------------------------------------------------- |
| `ATRIUM_ADDR`        | `:8080` | TCP listen address (`host:port`)                         |
| `OIDC_ISSUER`        | none       | OIDC issuer URL (discovery via `.well-known`); required  |
| `OIDC_CLIENT_ID`     | none       | OIDC client id; required                                 |
| `OIDC_CLIENT_SECRET` | none       | Confidential-client secret (token endpoint); required    |
| `OIDC_REDIRECT_URI`  | none       | Absolute callback URL; required; https enables `Secure`  |
| `OIDC_REQUIRE_EMAIL_VERIFIED` | `true` | Reject logins without `email_verified`; set `false` only for a dev IdP |
| `OIDC_MFA_ACR_VALUES` | none      | Comma-separated `acr` values that count as MFA; empty disables the check |
| `SESSION_KEY`        | none       | Base64 key (≥32 bytes) for HMAC-signed sessions; required |
| `SESSION_TTL`        | `12h`   | Absolute session lifetime (Go duration)                  |
| `TOS_ENABLED`        | `false` | Enable the Terms-of-Service consent gate                 |
| `TOS_PATH`           | none       | Path to the ToS Markdown file; required when enabled     |
| `TOS_VERSION`        | hash    | Explicit ToS version label; defaults to a content-hash prefix |
| `PROVIDER_TYPE`       | none       | Storage backend to bind at startup; required. Currently supported: `nextcloud` |
| `PROVIDER_BASE_URL`   | none       | Absolute base URL of the storage plugin app; required        |
| `PROVIDER_JWT_PRIVATE_KEY` | none  | PEM ECDSA **P-256** private key for signing plugin tokens; required |
| `MAX_UPLOAD_SIZE`    | `104857600` | Max upload size in bytes (100 MiB); larger uploads get 413 |
| `PROVIDER_TIMEOUT`    | `30m`   | Bounds a single download/upload transfer to the plugin    |
| `AUDIT_PSEUDONYMIZE` | `true`  | Hash recipient emails in audit events; `false` only for local dev |
| `AUDIT_SALT`         | none       | Optional salt strengthening the audit email hash against enumeration |
| `LOG_LEVEL`          | `info`  | Technical (non-audit) log level: `debug`\|`info`\|`warn`\|`error` |
| `BRANDING_DIR`       | none       | Optional dir whose files override the embedded white-label assets, served at `/branding/` |
| `BRAND_NAME`         | `ATRIUM`| White-label brand label (injected as `window.__ATRIUM__`)        |
| `BRAND_SUB`          | `Secure Share` | White-label brand sub-label                               |
| `BRAND_ACCENT_COLOR` | none       | CSS accent colour (e.g. `#2563eb`); empty keeps the theme accent |
| `BRAND_DEFAULT_THEME`| `light` | Initial theme for first-time visitors: `light`\|`dark`           |

## Terms-of-Service consent gate

The consent gate is optional (off unless `TOS_ENABLED=true`, which then requires
`TOS_PATH`). When on, the ToS Markdown is loaded once at startup (fail-fast on a
bad path) and a SHA-256 content hash drives versioning. An authenticated
recipient who has not accepted the current ToS gets `403 {"error":"tos_required"}`
from the API; the frontend fetches the document from `GET /api/tos`, renders a
blocking consent overlay, and `POST`s to `/api/tos/accept`. Acceptance is stored
in a long-lived, HMAC-signed `tos_accepted` cookie (bound to the email hash and
the ToS hash, so editing the document forces re-consent) and recorded in the
audit trail (`accept-tos`). When off, consent is delegated to the identity
provider and the gate is a pass-through.

## White-label branding

Two independent seams, both applied at runtime. The shipped build carries
neutral Atrium defaults:

- **Assets** (`/branding/`). The logos ship as embedded defaults (`logo.svg`,
  `logo-dark.svg`). Mount a file of the same name into `BRANDING_DIR` to override
  one; anything absent falls back to the embedded default, so nothing 404s. The
  mount is read per request via `os.Root` (path traversal outside it is
  structurally impossible) and a swapped file is picked up without a restart.
- **Text & theme**. `BRAND_NAME`, `BRAND_SUB` and `BRAND_DEFAULT_THEME` are
  injected into `index.html` at startup as a `window.__ATRIUM__` object;
  `BRAND_ACCENT_COLOR` is validated as a hex colour (fail-fast) and injected as a
  `:root` CSS override. See [architecture.md](architecture.md) for how this stays
  within the Content-Security-Policy.

## Provider trust setup (one-time, manual)

The core reaches the storage plugin as a trusted client, signing every request
with a per-request ES256 JWT. The trust relationship is anchored by the core's
**public** key, installed on the plugin side. Generate a P-256 keypair:

```bash
openssl ecparam -name prime256v1 -genkey -noout -out provider-signing.key   # private (PROVIDER_JWT_PRIVATE_KEY)
openssl ec -in provider-signing.key -pubout -out provider-signing.pub        # public (install on the plugin)
```

Provide the **private** key to the core via `PROVIDER_JWT_PRIVATE_KEY` (from a
Kubernetes secret). On startup the core logs the derived public key and probes
the plugin with a signed healthcheck. If the plugin has no (or a wrong) public
key it answers `403`; the core stays up **degraded** and `GET /readyz` returns
`503` until trust is established (liveness `/healthz` is independent, so a
degraded core is not restarted). The command to install the key on the plugin
lives in that plugin's own docs, so this message stays storage-agnostic.
