# Tier-2 integration test (manual, pre-release)

Tier 2 verifies the one thing the hermetic Tier-1 suite cannot: that the
**Nextcloud provider** really creates an external share from the Files sharing
sidebar. It drives a real Nextcloud that has `atrium_secureshare` enabled, with
Playwright. It is **manual, pre-release, and never in CI**.

All it needs is a **URL to a running Nextcloud with the provider enabled** — no
database service, no docker-compose. Point the spec at one of the following.

> Not executed in CI or the dev sandbox. The provider section's own strings are
> stable, but the Nextcloud **shell** selectors (login, opening the sharing
> sidebar) can shift between Nextcloud versions — confirm them on the first run.

Install the browser once: `cd e2e && npm ci && npx playwright install chromium`.

## Preferred: a Nextcloud you already have (no Docker)

Any instance with the provider enabled works — a local dev Nextcloud, or the
project's deployed one. From `atrium-core/e2e`:

```bash
NEXTCLOUD_URL=https://nextcloud.example.org \
NC_USER=<user> NC_PASSWORD=<pass> \
npx playwright test --config tier2/playwright.config.ts
```

Use a throwaway test file and recipient, and clean up the created share
afterwards.

## Fallback: a throwaway local Nextcloud (single container, SQLite)

The official image uses **SQLite by default**, so a throwaway instance is a
single `docker run` — no separate database, no compose. Build the provider first
so the mount contains the built app, then run from the `atrium-core` repo root:

```bash
# 1. Build the provider (see the provider repo's AGENTS.md for the toolchain)
( cd ../atrium-plugin-nextcloud && composer install --no-dev --optimize-autoloader && npm ci && npm run build )

# 2. Start Nextcloud (SQLite) with the provider mounted
docker run -d --name atrium-nc -p 8090:80 \
  -e NEXTCLOUD_ADMIN_USER=admin -e NEXTCLOUD_ADMIN_PASSWORD=admin \
  -e NEXTCLOUD_TRUSTED_DOMAINS=127.0.0.1 \
  -v "$PWD/../atrium-plugin-nextcloud:/var/www/html/custom_apps/atrium_secureshare:ro" \
  nextcloud:30-apache
# wait for first boot (~1 min), then enable + configure the app:
docker exec -u www-data atrium-nc php occ app:enable atrium_secureshare
# configure the admin settings (core URL, signing public key, allowed modes) per
# the provider repo's README.md.

# 3. Run the spec, then tear down
( cd e2e && NEXTCLOUD_URL=http://127.0.0.1:8090 npx playwright test --config tier2/playwright.config.ts )
docker rm -f atrium-nc
```

Overrides: `NC_USER`, `NC_PASSWORD` (default `admin`/`admin`), `NC_TEST_FILE`
(default `Readme.md`), `NC_RECIPIENT`.

## What it asserts

Logs into Nextcloud, opens the sharing sidebar for a file, finds the Atrium
section, creates an external share for a recipient email, and checks the share
appears in the active-shares list — the real provider path end to end.
