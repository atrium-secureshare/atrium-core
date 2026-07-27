import { defineConfig, devices } from '@playwright/test'

// Drives the built SPA (same single-page fallback as production, so deep-link
// refreshes are exercised) and mocks the gateway API, so no backend is needed.
// Chromium only — the app is a standard SPA.
export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL: 'http://localhost:4173',
    trace: 'on-first-retry',
    // Pin the browser language so i18next resolves to German; the suite asserts German UI copy.
    locale: 'de-CH',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
  ],
  webServer: {
    command: 'npm run build && npm run preview -- --port 4173 --strictPort',
    url: 'http://localhost:4173',
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
})
