import { expect, test, type Page, type Route } from '@playwright/test'

// i18n: browser-language detection on first visit, the header language switcher
// (manual override + persistence) and that a switch re-renders the UI copy.
// The gateway API is mocked — no backend/OIDC/provider.

async function mockGateway(page: Page) {
  const json = (route: Route, body: unknown) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(body),
    })
  await page.route('**/api/me', (route) =>
    json(route, { email: 'recipient@example.com' }),
  )
  await page.route('**/api/shares', (route) => json(route, []))
}

test.beforeEach(async ({ page }) => {
  await mockGateway(page)
})

test('detects the browser language (de-CH → German UI)', async ({ page }) => {
  await page.goto('/')
  await expect(
    page.getByRole('heading', { name: 'Meine Freigaben' }),
  ).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'de')
})

test('the switcher changes the language and persists it across a reload', async ({
  page,
}) => {
  await page.goto('/')
  await expect(
    page.getByRole('heading', { name: 'Meine Freigaben' }),
  ).toBeVisible()

  // The switcher button carries the localized "Language" label — "Sprache" in German.
  await page.getByRole('button', { name: 'Sprache' }).click()
  await page.getByRole('menuitemradio', { name: 'English' }).click()

  await expect(page.getByRole('heading', { name: 'My shares' })).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'en')
  await expect(
    page.evaluate(() => localStorage.getItem('atrium-locale')),
  ).resolves.toBe('en')

  // The choice survives a reload (localStorage wins over the browser language).
  await page.reload()
  await expect(page.getByRole('heading', { name: 'My shares' })).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'en')
})

test.describe('with a French browser', () => {
  test.use({ locale: 'fr-FR' })

  test('detects French on first visit', async ({ page }) => {
    await page.goto('/')
    await expect(
      page.getByRole('heading', { name: 'Mes partages' }),
    ).toBeVisible()
    await expect(page.locator('html')).toHaveAttribute('lang', 'fr')
  })
})
