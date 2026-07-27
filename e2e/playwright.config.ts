import { defineConfig, devices } from '@playwright/test'

// Tier-1 integration tests. Playwright is only the driver: the suite exercises
// the real core gateway end to end against an in-process mock OIDC provider and
// a stub provider (see cmd/atrium-e2e). One browser is enough — this is an
// integration suite, not a browser-compatibility matrix — so we run Chromium only.

const BASE_URL = process.env.E2E_BASE_URL ?? 'http://127.0.0.1:8080'

export default defineConfig({
  testDir: '.',
  testMatch: '**/*.spec.ts',
  // Tier 2 (real Nextcloud in Docker) has its own config and runs manually
  // pre-release, never as part of the hermetic Tier-1 run or CI.
  testIgnore: '**/tier2/**',
  // The harness holds a single shared fixture dataset (upload/download mutate it);
  // tests reset it via /_e2e/seed and run serially so they never clobber each other.
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: process.env.CI ? [['github'], ['list']] : 'list',
  timeout: 30_000,
  expect: { timeout: 7_000 },
  use: {
    baseURL: BASE_URL,
    locale: 'de-CH',
    trace: 'on-first-retry',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    // Build the SPA (embedded by the Go binary), build the harness, then run it.
    // Set E2E_SKIP_BUILD=1 to reuse a binary already built at /tmp/atrium-e2e-bin.
    command: process.env.E2E_SKIP_BUILD
      ? '/tmp/atrium-e2e-bin'
      : 'npm --prefix web run build && go build -o /tmp/atrium-e2e-bin ./cmd/atrium-e2e && /tmp/atrium-e2e-bin',
    cwd: '..',
    url: `${BASE_URL}/healthz`,
    reuseExistingServer: !process.env.CI,
    timeout: 180_000,
    stdout: 'pipe',
    stderr: 'pipe',
    env: {
      ATRIUM_ADDR: '127.0.0.1:8080',
      E2E_BASE_URL: BASE_URL,
      // Enabling the consent gate lets the ToS specs run against the same server;
      // every other spec accepts it in the fixture.
      TOS_PATH: 'e2e/testdata/tos.md',
    },
  },
})
