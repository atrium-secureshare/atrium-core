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

  test('logout ends the session and confirms it', async ({ authed }) => {
    await authed.getByRole('button', { name: 'Kontomenü' }).click()
    await authed.getByRole('menuitem', { name: 'Abmelden' }).click()

    // The provider returns here, not to the app root where the shell would 401.
    await expect(authed).toHaveURL(/\/auth\/logged-out$/)
    await expect(authed.getByRole('status')).toContainText('abgemeldet')
    await expect(authed.getByRole('link', { name: 'Erneut anmelden' })).toBeVisible()

    expect((await authed.request.get('/api/shares')).status()).toBe(401)
  })

  test('the confirmation page loads no authenticated endpoint, so it cannot bounce', async ({
    page,
  }) => {
    // Only a 401 can trigger the bounce, so assert its cause is absent rather
    // than waiting out a redirect that may never come.
    const apiCalls: string[] = []
    page.on('request', (req) => {
      if (new URL(req.url()).pathname.startsWith('/api/')) apiCalls.push(req.url())
    })

    await page.goto('/auth/logged-out')
    await page.waitForLoadState('networkidle')

    expect(apiCalls).toHaveLength(0)
    expect(new URL(page.url()).pathname).toBe('/auth/logged-out')
    await expect(page.getByRole('status')).toBeVisible()
  })

  test('signing in again from the confirmation returns to the IdP login', async ({
    authed,
  }) => {
    await authed.getByRole('button', { name: 'Kontomenü' }).click()
    await authed.getByRole('menuitem', { name: 'Abmelden' }).click()
    await authed.getByRole('link', { name: 'Erneut anmelden' }).click()

    await expect(authed.getByRole('button', { name: 'Anmelden' })).toBeVisible()
  })

  test('a deadline left over from an idle logout does not bounce the next visit', async ({
    authed,
  }) => {
    // What an idle logout leaves in localStorage. Left in place it fires on the
    // next mount, so even the load right after signing in again bounced straight
    // back to the confirmation page.
    await authed.evaluate(() =>
      localStorage.setItem('atrium-session-expires', String(Date.now() - 1_000)),
    )
    await authed.reload()

    await expect(
      authed.getByRole('heading', { name: 'Meine Freigaben' }),
    ).toBeVisible()
    expect(new URL(authed.url()).pathname).toBe('/')
  })

  test('with no session a stale deadline still leads to login, not the confirmation', async ({
    page,
  }) => {
    // Seeded from the confirmation page, which loads nothing authenticated: that
    // is where an idled-out recipient sits before opening the app again.
    await page.goto('/auth/logged-out')
    await page.evaluate(() =>
      localStorage.setItem('atrium-session-expires', String(Date.now() - 1_000)),
    )

    await page.goto('/')
    await expect(page.getByRole('button', { name: 'Anmelden' })).toBeVisible()
    expect(page.url()).toContain('/auth')
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
