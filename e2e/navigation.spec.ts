import { test, expect } from './fixtures'

// Listing, nested navigation and download specs (Tier 1). These cover the
// deep-linkable in-place browser: the flat listing at /, drilling into sub-folders
// under /share/:hash/*, the breadcrumb, deep-link restore, the file detail dialog
// and the actual download event, plus the per-recipient download limit.

test.describe('Listing and navigation', () => {
  test('the landing page lists the recipient shares', async ({ authed }) => {
    await expect(authed.getByRole('heading', { name: 'Meine Freigaben' })).toBeVisible()
    await expect(authed.getByText('Quartalsbericht.pdf')).toBeVisible()
    await expect(authed.getByRole('button', { name: 'Ordner öffnen: Projekt' })).toBeVisible()
  })

  test('descends into folders and returns via the breadcrumb', async ({ authed }) => {
    await authed.getByRole('button', { name: 'Ordner öffnen: Projekt' }).click()
    await expect(authed).toHaveURL(/\/share\/folder-readall$/)
    const contents = authed.getByRole('list', { name: 'Ordnerinhalt' })
    await expect(contents.getByText('Plan.pdf')).toBeVisible()
    await expect(contents.getByText('Unterlagen')).toBeVisible()

    // Descend one level; the breadcrumb tracks the path.
    await authed.getByRole('button', { name: 'Ordner öffnen: Unterlagen' }).click()
    await expect(authed).toHaveURL(/\/share\/folder-readall\/Unterlagen$/)
    await expect(contents.getByText('Detail.txt')).toBeVisible()
    const crumbs = authed.getByRole('navigation', { name: 'Pfad' })
    await expect(crumbs.getByText('Unterlagen')).toBeVisible()

    // Jump back to the share root via the breadcrumb link.
    await crumbs.getByRole('link', { name: 'Projekt' }).click()
    await expect(authed).toHaveURL(/\/share\/folder-readall$/)
    await expect(contents.getByText('Unterlagen')).toBeVisible()
  })

  test('a deep link restores the nested folder directly', async ({ authed }) => {
    await authed.goto('/share/folder-readall/Unterlagen')
    await expect(authed.getByRole('list', { name: 'Ordnerinhalt' }).getByText('Detail.txt')).toBeVisible()
    await expect(authed.getByRole('navigation', { name: 'Pfad' }).getByText('Unterlagen')).toBeVisible()
  })

  test('the keyboard opens a folder (Enter on a row)', async ({ authed }) => {
    const row = authed.getByRole('button', { name: 'Ordner öffnen: Projekt' })
    await row.focus()
    await authed.keyboard.press('Enter')
    await expect(authed.getByRole('navigation', { name: 'Pfad' })).toBeVisible()
    await expect(authed.getByRole('button', { name: 'Ordner öffnen: Unterlagen' })).toBeVisible()
  })

  test('a file detail dialog opens and downloads', async ({ authed }) => {
    await authed.getByRole('button', { name: 'Ordner öffnen: Projekt' }).click()
    await authed.getByRole('button', { name: 'Details anzeigen: Plan.pdf' }).click()

    const dialog = authed.getByRole('dialog')
    await expect(dialog).toBeVisible()
    await expect(dialog.getByText('Plan.pdf')).toBeVisible()

    const downloadPromise = authed.waitForEvent('download')
    await dialog.getByRole('button', { name: 'Jetzt herunterladen' }).click()
    const download = await downloadPromise
    expect(download.suggestedFilename()).toBe('Plan.pdf')
  })

  test('a folder file row downloads directly', async ({ authed }) => {
    await authed.getByRole('button', { name: 'Ordner öffnen: Projekt' }).click()
    const downloadPromise = authed.waitForEvent('download')
    await authed.getByRole('button', { name: 'Herunterladen: Plan.pdf' }).click()
    expect((await downloadPromise).suggestedFilename()).toBe('Plan.pdf')
  })

  test('a single-file share downloads from the listing', async ({ authed }) => {
    const downloadPromise = authed.waitForEvent('download')
    await authed.getByRole('button', { name: 'Herunterladen: Quartalsbericht.pdf' }).click()
    expect((await downloadPromise).suggestedFilename()).toBe('Quartalsbericht.pdf')
  })

  test('the per-recipient download limit is enforced', async ({ authed }) => {
    const first = await authed.request.get('/api/shares/file-limited/content')
    expect(first.status()).toBe(200)
    const second = await authed.request.get('/api/shares/file-limited/content')
    expect(second.status()).toBe(410)
  })
})
