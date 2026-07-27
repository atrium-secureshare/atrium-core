import { test, expect, login, acceptTos, resetSeed, RECIPIENT } from './fixtures'

// ToS specs (Tier 1). The harness runs with the consent gate enabled, so a fresh
// login lands on the blocking overlay until it is accepted. Re-consent on a ToS
// version change is not re-driven here: it is a cookie-binding concern already
// covered by internal/tos unit tests (the acceptance cookie is bound to the ToS
// content hash, so a changed document invalidates it).

test.describe('Terms of Service', () => {
  test('the overlay appears and blocks the app until accepted', async ({
    page,
  }) => {
    await resetSeed(page.request)
    await login(page, RECIPIENT)

    // Blocking consent dialog, no share listing behind it yet.
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    await expect(dialog.locator('#tos-title')).toHaveText('Nutzungsbedingungen')
    await expect(dialog.getByRole('button', { name: 'Ich stimme zu' })).toBeVisible()
    await expect(page.getByRole('table', { name: 'Freigegebene Dateien und Ordner' })).toHaveCount(0)

    // The block is enforced server-side, not just hidden in the UI: with the
    // real session cookie but consent still outstanding, a protected resource
    // answers 403 tos_required through the gate (RequireAuth -> RequireTOS).
    const gated = await page.request.get('/api/shares')
    expect(gated.status()).toBe(403)
    expect((await gated.json()).error).toBe('tos_required')

    await acceptTos(page)

    // After consent the listing is reachable.
    await expect(page.getByRole('heading', { name: 'Meine Freigaben' })).toBeVisible()
    await expect(page.getByText('Quartalsbericht.pdf')).toBeVisible()
  })

  test('consent persists across a reload', async ({ page }) => {
    await resetSeed(page.request)
    await login(page, RECIPIENT)
    await acceptTos(page)

    await page.reload()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect(page.getByRole('heading', { name: 'Meine Freigaben' })).toBeVisible()
  })
})
