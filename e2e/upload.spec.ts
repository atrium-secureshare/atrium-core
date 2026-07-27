import { test, expect } from './fixtures'

// Upload and permission-mode specs (Tier 1). Each folder mode has distinct
// affordances: read-only shows a listing but no dropzone; write/read-all shows
// uploads to everyone; write/read-own shows only the recipient's own uploads
// (badged); a dropzone accepts uploads but never reveals its contents. The
// ModeBanner explains each, and uploads stream through the real gateway/proxy.

const file = { name: 'bericht.txt', mimeType: 'text/plain', buffer: Buffer.from('inhalt') }

async function upload(page: import('@playwright/test').Page) {
  await page.locator('input[type="file"]').setInputFiles(file)
}

test.describe('Upload and permission modes', () => {
  test('read-only: a listing but no upload affordance', async ({ authed }) => {
    await authed.getByRole('button', { name: 'Ordner öffnen: NurLesen' }).click()
    await expect(authed.getByRole('note')).toContainText('Nur Lesen')
    await expect(authed.getByRole('list', { name: 'Ordnerinhalt' }).getByText('Liste.csv')).toBeVisible()
    await expect(authed.getByText('Dateien hier ablegen oder klicken zum Hochladen')).toHaveCount(0)
  })

  test('write/read-all: an upload appears in the shared listing', async ({ authed }) => {
    await authed.getByRole('button', { name: 'Ordner öffnen: Projekt' }).click()
    await expect(authed.getByRole('note')).toContainText('Lesen & Hochladen')
    await upload(authed)
    await expect(
      authed.getByRole('list', { name: 'Ordnerinhalt' }).getByText('bericht.txt'),
    ).toBeVisible()
  })

  test('write/read-own: only the own upload is shown, badged', async ({ authed }) => {
    await authed.getByRole('button', { name: 'Ordner öffnen: MeineUploads' }).click()
    await expect(authed.getByRole('note')).toContainText('Hochladen / Eigene lesen')
    await expect(authed.getByText('Dieser Ordner ist leer.')).toBeVisible()

    await upload(authed)
    const contents = authed.getByRole('list', { name: 'Ordnerinhalt' })
    await expect(contents.getByText('bericht.txt')).toBeVisible()
    await expect(contents.getByText('Ihr Upload')).toBeVisible()
  })

  test('dropzone: upload accepted, contents never listed', async ({ authed }) => {
    await authed.getByRole('button', { name: 'Ordner öffnen: Briefkasten' }).click()
    await expect(authed.getByRole('note')).toContainText('Dropzone')
    // A dropzone is not browsable: no listing is rendered.
    await expect(authed.getByRole('list', { name: 'Ordnerinhalt' })).toHaveCount(0)

    await upload(authed)
    await expect(authed.getByText('«bericht.txt» hochgeladen')).toBeVisible()
  })
})
