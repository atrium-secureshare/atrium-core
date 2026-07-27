import { test, expect } from '@playwright/test'

// Tier-2 integration spec (MANUAL, pre-release). It drives a real Nextcloud with
// the atrium_secureshare provider installed and verifies the core provider flow the
// hermetic Tier-1 suite cannot: creating an external share from the Files sharing
// sidebar and seeing it listed. Bring the stack up and configure the app first
// (see README.md); this spec is never part of CI.
//
// The provider section's own strings are stable (from our Vue components); the
// Nextcloud shell selectors (login, opening the sharing sidebar) can vary by
// Nextcloud version and should be confirmed on the first run against the pinned
// image in docker-compose.yml.

const NC_USER = process.env.NC_USER ?? 'admin'
const NC_PASSWORD = process.env.NC_PASSWORD ?? 'admin'
// A file that exists in the account (Nextcloud ships Readme.md by default).
const FILE_NAME = process.env.NC_TEST_FILE ?? 'Readme.md'
const RECIPIENT = process.env.NC_RECIPIENT ?? 'external@example.test'

async function login(page: import('@playwright/test').Page) {
  await page.goto('/login')
  await page.locator('#user').fill(NC_USER)
  await page.locator('#password').fill(NC_PASSWORD)
  await page.locator('button[type="submit"], #submit-wrapper button').first().click()
  await expect(page).toHaveURL(/\/apps\//)
}

test.describe('Nextcloud provider — external sharing sidebar', () => {
  test('creates an external share and lists it', async ({ page }) => {
    await login(page)
    await page.goto('/apps/files')

    // Open the sharing sidebar for the test file, then its "Sharing" tab.
    const row = page.getByRole('row', { name: new RegExp(FILE_NAME) })
    await row.getByRole('button', { name: /Actions|Aktionen/ }).click()
    await page.getByRole('menuitem', { name: /Details|Open sidebar|Details öffnen/ }).click()
    await page.getByRole('tab', { name: /Sharing|Teilen/ }).click()

    // The Atrium section (title comes from our AtriumSection.vue).
    const section = page.locator('.atrium-section')
    await expect(section.getByText(/External sharing via/)).toBeVisible()

    // Fill the recipient and create the share.
    await section.getByLabel('Recipient email').fill(RECIPIENT)
    await section.getByRole('button', { name: 'Create share' }).click()

    // It appears in the active-shares list.
    await expect(section.getByText('Active external shares')).toBeVisible()
    await expect(section.getByText(RECIPIENT)).toBeVisible()
  })
})
