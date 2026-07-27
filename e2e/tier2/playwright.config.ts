import { defineConfig, devices } from '@playwright/test'

// Tier-2 config: drives a real, already-running Nextcloud that has the provider
// enabled (see README.md). There is no webServer here — the instance is provided
// externally. Point NEXTCLOUD_URL at it (a dev instance, the deployed one, or a
// throwaway `docker run` — the official image uses SQLite, so no DB is needed).

const NEXTCLOUD_URL = process.env.NEXTCLOUD_URL ?? 'http://127.0.0.1:8090'

export default defineConfig({
  testDir: '.',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: 'list',
  timeout: 60_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL: NEXTCLOUD_URL,
    locale: 'de-CH',
    trace: 'on-first-retry',
    ignoreHTTPSErrors: true,
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
