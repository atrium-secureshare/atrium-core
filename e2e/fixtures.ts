import {
  test as base,
  expect,
  type APIRequestContext,
  type Page,
} from '@playwright/test'

// Canonical identities, matching the harness fixture dataset (cmd/atrium-e2e).
export const RECIPIENT = 'recipient@example.test'
export const OTHER = 'other@example.test'

/** resetSeed restores the stub provider's fixture dataset to its initial state. */
export async function resetSeed(request: APIRequestContext): Promise<void> {
  const res = await request.post('/_e2e/seed')
  expect(res.ok(), 'seed reset failed').toBeTruthy()
}

/**
 * login drives the mock IdP's login form for the given email, completing the
 * real authorization-code + PKCE flow the core runs in production. It stops once
 * the browser is back on the app; it does not accept the ToS (see acceptTos), so
 * the consent specs can observe the overlay. Pass verified:false to log in with
 * an unverified email (the core then rejects the callback).
 */
export async function login(
  page: Page,
  email: string,
  opts: { verified?: boolean } = {},
): Promise<void> {
  const { verified = true } = opts
  await page.goto('/auth/login')
  await page.getByLabel('E-Mail').fill(email)
  if (!verified) await page.getByRole('checkbox').uncheck()
  await page.getByRole('button', { name: 'Anmelden' }).click()
}

/** acceptTos clicks through the blocking consent overlay and waits for it to close. */
export async function acceptTos(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Ich stimme zu' }).click()
  await expect(page.getByRole('dialog')).toBeHidden()
}

/**
 * The `authed` fixture yields a page that is logged in as the canonical
 * recipient with the ToS accepted and a freshly seeded dataset — the starting
 * point for the listing, navigation, download and upload specs.
 */
export const test = base.extend<{ authed: Page }>({
  authed: async ({ page, request }, use) => {
    await resetSeed(request)
    await login(page, RECIPIENT)
    await acceptTos(page)
    await expect(
      page.getByRole('heading', { name: 'Meine Freigaben' }),
    ).toBeVisible()
    await use(page)
  },
})

export { expect }
