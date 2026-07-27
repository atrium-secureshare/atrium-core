import { test, expect, login, resetSeed, RECIPIENT } from './fixtures'

// Targeted accessibility checks (Tier 1). Not a full audit — the two interactions
// most likely to trap keyboard and screen-reader users: the blocking ToS overlay
// (focus must stay on the only actionable control) and the file dialog (focus
// moves in, Escape closes), plus the landmark roles the listing and breadcrumb
// expose.

test.describe('Accessibility', () => {
  test('the ToS overlay keeps focus on the accept button', async ({ page }) => {
    await resetSeed(page.request)
    await login(page, RECIPIENT)

    const accept = page.getByRole('button', { name: 'Ich stimme zu' })
    await expect(accept).toBeEnabled() // ToS content has loaded
    // The overlay traps Tab on the only actionable control, so the blocked UI
    // behind it is never reachable however many times the user tabs.
    await page.keyboard.press('Tab')
    await expect(accept).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(accept).toBeFocused()
  })

  test('the file dialog opens and closes on Escape', async ({ authed }) => {
    await authed.getByRole('button', { name: 'Ordner öffnen: Projekt' }).click()
    await authed.getByRole('button', { name: 'Details anzeigen: Plan.pdf' }).click()

    const dialog = authed.getByRole('dialog')
    await expect(dialog).toBeVisible()
    await authed.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
  })

  test('the listing and breadcrumb expose landmark roles', async ({ authed }) => {
    await expect(
      authed.getByRole('table', { name: 'Freigegebene Dateien und Ordner' }),
    ).toBeVisible()

    await authed.getByRole('button', { name: 'Ordner öffnen: Projekt' }).click()
    await expect(authed.getByRole('navigation', { name: 'Pfad' })).toBeVisible()
  })
})
