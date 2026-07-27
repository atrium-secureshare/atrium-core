# atrium-core agent guide

Atrium Secureshare Core is an identity-aware gateway that proxies external file
sharing for on-prem storage. External, OIDC-authenticated recipients access files
shared with them, and the storage backend need not be exposed to the internet.
Published open source under **MIT**.

[README.md](README.md) is the product overview and has the network-zone diagram.
[docs/configuration.md](docs/configuration.md) covers the environment variables,
the Terms-of-Service gate, branding and the trust setup.
[docs/architecture.md](docs/architecture.md) records the security invariants (the
routing boundary, headers and CSP, the audit trail, the trust protocol). Read the
relevant one before changing behaviour in that area, since they record decisions
that must not silently regress. This file covers only how to navigate, build and
contribute.

## Tech stack

- **Backend:** Go, stdlib-first (`net/http`, `log/slog`). Add an external
  dependency only after the minimalism ladder rules out the standard library.
- **Frontend:** React + TypeScript (Vite), Tailwind CSS v4, shadcn/ui, in
  `web/`. Design tokens (light/dark, white-label) are CSS variables in
  `web/src/index.css`.
- **Container:** multi-stage `Dockerfile` → distroless, non-root.

## Layout

```
cmd/atrium/main.go     # entry point: server wiring + graceful shutdown
internal/config/       # configuration from environment
internal/api/          # HTTP handlers (healthz, readyz, /me, route wiring, security headers)
internal/auth/         # OIDC login flow + signed session cookies
internal/tos/          # optional Terms-of-Service consent gate
internal/provider/     # trusted client to the storage plugin (signed ES256 JWTs)
internal/proxy/        # stream proxy for file download/upload (no buffering)
internal/branding/     # white-label assets at /branding/ (BRANDING_DIR + embedded defaults)
internal/audit/        # slog setup: audit level, email pseudonymization, audit.Log helper
web/                   # React/TS frontend (Vite)
e2e/                   # Playwright integration suite (see e2e/ + e2e/tier2/README.md)
Dockerfile             # multi-stage build → distroless
```

## Build, run, test

Backend:

```bash
go build ./cmd/atrium      # compile
go vet ./...               # static checks
go test ./...              # unit tests
gofmt -l .                 # must print nothing

ATRIUM_ADDR=:8080 go run ./cmd/atrium   # run (default :8080)
curl localhost:8080/healthz             # -> ok
```

Frontend (in `web/`):

```bash
npm install
npm run dev          # dev server with HMR
npm run build        # type-check + production build to web/dist
npm run lint         # oxlint
npm run format       # prettier --write
```

End-to-end tests (`e2e/`) run in two tiers: **Tier 1** is hermetic (real
`api.Handler` against an in-process mock OIDC + stub provider) and is the CI
merge gate. Run it with `cd e2e && npm ci && npx playwright install chromium && npm test`.
**Tier 2** runs against a real Nextcloud, manually, pre-release only. Details in
`e2e/tier2/README.md`.

## Go conventions

Write simple, idiomatic Go (Effective Go / Go Code Review Comments):

- Keep the happy path left-aligned; return early, avoid deep nesting.
- Check errors immediately; wrap with `fmt.Errorf("...: %w", err)` for context.
- Accept interfaces, return concrete types; keep interfaces small.
- Document exported symbols, starting with the symbol name.
- Comments and identifiers in English; no emoji.
- Use the enhanced `net/http` `ServeMux` (method + pattern routing).
- Format with `gofmt`; run `go vet` before committing.

## Minimalism ladder

Before implementing anything, walk this ladder top-down and stop at the first
step that applies:

1. Does it need to exist at all? If not, leave it out (YAGNI).
2. Already present in the codebase? Reuse it.
3. Can the standard library do it? Use stdlib.
4. Native platform feature? Use it.
5. Already-installed dependency? Use it.
6. One line? Write one line.
7. Only then: the minimum that works.

## Non-goals

Single-file shares stay read-only. A writable replace mode is not supported because the content type of a replacement cannot be verified on the server.
