import { test, expect, login, acceptTos, resetSeed, RECIPIENT, OTHER } from './fixtures'

// Auth specs (Tier 1, mock OIDC). These exercise the real authorization-code +
// PKCE flow and the session/identity binding. Real Keycloak and MFA/TOTP are
// deliberately NOT automated here: MFA is a Keycloak concern covered by a manual
// pre-release/cluster smoke; the OIDC flow logic itself is unit-tested in
// internal/auth. Here we only need identity control, which the mock IdP provides.

test.describe('Authentication', () => {
  test('unauthenticated API returns 401 and the app sends the user to login', async ({
    page,
    request,
  }) => {
    const res = await request.get('/api/shares')
    expect(res.status()).toBe(401)

    // Loading the app with no session bounces through /auth/login to the IdP.
    await page.goto('/')
    await expect(page.getByRole('button', { name: 'Anmelden' })).toBeVisible()
    expect(page.url()).toContain('/auth')
  })

  test('a verified login reaches the app', async ({ page }) => {
    await resetSeed(page.request)
    await login(page, RECIPIENT)
    await acceptTos(page)

    await expect(page.getByRole('heading', { name: 'Meine Freigaben' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Kontomenü' })).toBeVisible()
    expect(new URL(page.url()).pathname).toBe('/')
  })

  test('the session persists across a reload', async ({ authed }) => {
    await authed.reload()
    await expect(authed.getByRole('heading', { name: 'Meine Freigaben' })).toBeVisible()
    await expect(authed.getByRole('button', { name: 'Anmelden' })).toHaveCount(0)
  })

  test('identity binding: a recipient sees only their own shares', async ({
    page,
  }) => {
    await resetSeed(page.request)
    await login(page, OTHER)
    await acceptTos(page)

    // OTHER owns only "Fremd.pdf"; the canonical recipient's shares stay hidden.
    await expect(page.getByText('Fremd.pdf')).toBeVisible()
    await expect(page.getByText('Quartalsbericht.pdf')).toHaveCount(0)
  })
})
