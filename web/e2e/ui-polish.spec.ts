import { expect, test, type Route } from '@playwright/test'

// UI polish regressions: the download counter refreshes after a download, the
// identity chip appears the moment the ToS is accepted, the persisted dark theme
// is applied before first paint, no fonts load from an external CDN, and the
// header shows the handover logo. The gateway API is mocked (no backend).

const json = (route: Route, body: unknown, status = 200) =>
  route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body),
  })

const tosRequired = (route: Route) =>
  json(route, { error: 'tos_required' }, 403)

test('a download refreshes the download counter in place', async ({ page }) => {
  // The counter lives server-side; the app must refetch after the download to
  // reflect it. Start at 1/5 and bump when the file is fetched.
  let downloadCount = 1

  await page.route('**/api/me', (route) =>
    json(route, { email: 'recipient@example.com' }),
  )
  await page.route('**/api/shares', (route) =>
    json(route, [
      {
        id: 'filehash',
        name: 'vertrag.pdf',
        size: 2048,
        isFolder: false,
        mode: 0,
        downloadCount,
        maxDownloads: 5,
        expiresAt: '2026-12-31T00:00:00Z',
        createdAt: '2026-07-01T00:00:00Z',
      },
    ]),
  )
  await page.route('**/api/shares/filehash/content', (route) => {
    downloadCount += 1
    return route.fulfill({
      status: 200,
      headers: { 'content-disposition': 'attachment; filename="vertrag.pdf"' },
      contentType: 'application/pdf',
      body: 'pdf',
    })
  })

  await page.goto('/share/filehash')
  const dialog = page.getByRole('dialog')
  await expect(dialog).toContainText('1 / 5')

  await dialog.getByRole('button', { name: 'Jetzt herunterladen' }).click()

  // The listing refetches shortly after the trigger, so the counter updates
  // without a manual page reload.
  await expect(dialog).toContainText('2 / 5')
})

test('accepting the ToS reveals the identity chip immediately', async ({
  page,
}) => {
  // Both /api/me and /api/shares are ToS-gated: they 403 until consent, then
  // succeed. The email must appear right after acceptance, no reload.
  let accepted = false

  await page.route('**/api/me', (route) =>
    accepted
      ? json(route, { email: 'recipient@example.com' })
      : tosRequired(route),
  )
  await page.route('**/api/shares', (route) =>
    accepted ? json(route, []) : tosRequired(route),
  )
  await page.route('**/api/tos', (route) =>
    json(route, {
      version: 'v1',
      content: '# Bedingungen\n\nBitte zustimmen.',
    }),
  )
  await page.route('**/api/tos/accept', (route) => {
    accepted = true
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: '{}',
    })
  })

  await page.goto('/')

  await expect(
    page.getByRole('dialog', { name: 'Nutzungsbedingungen' }),
  ).toBeVisible()
  await expect(page.getByText('recipient@example.com')).toHaveCount(0)

  await page.getByRole('button', { name: 'Ich stimme zu' }).click()

  // Acceptance pulls /api/me in addition to /api/shares, so the chip appears.
  await expect(page.getByText('recipient@example.com')).toBeVisible()
})

test('the persisted dark theme is applied before React mounts', async ({
  page,
}) => {
  await page.route('**/api/me', (route) =>
    json(route, { email: 'recipient@example.com' }),
  )
  await page.route('**/api/shares', (route) => json(route, []))

  // Persist a dark preference the way the app does, before any page script runs.
  await page.addInitScript(() => localStorage.setItem('atrium-theme', 'dark'))

  await page.goto('/')

  // The pre-paint script (index.html) set the class and color-scheme; both are
  // present without waiting for the React effect.
  await expect(page.locator('html')).toHaveClass(/dark/)
  const colorScheme = await page.evaluate(
    () => document.documentElement.style.colorScheme,
  )
  expect(colorScheme).toBe('dark')
})

test('no fonts are loaded from an external CDN', async ({ page }) => {
  await page.route('**/api/me', (route) =>
    json(route, { email: 'recipient@example.com' }),
  )
  await page.route('**/api/shares', (route) => json(route, []))

  const external: string[] = []
  page.on('request', (req) => {
    const url = req.url()
    if (
      url.includes('fonts.googleapis.com') ||
      url.includes('fonts.gstatic.com')
    )
      external.push(url)
  })

  await page.goto('/')
  await expect(
    page.getByRole('heading', { name: 'Meine Freigaben' }),
  ).toBeVisible()

  expect(external).toEqual([])
})

test('the header shows the handover logo', async ({ page }) => {
  await page.route('**/api/me', (route) =>
    json(route, { email: 'recipient@example.com' }),
  )
  await page.route('**/api/shares', (route) => json(route, []))

  await page.goto('/')

  const logo = page.getByRole('banner').getByRole('img', {
    name: 'ATRIUM Secure Share',
  })
  await expect(logo).toBeVisible()
  // Vite may inline a small SVG as a data URI or emit a hashed .svg asset; both
  // are the bundled handover mark, never an external URL.
  await expect(logo).toHaveAttribute('src', /(\.svg$|^data:image\/svg\+xml)/)
})
