import { expect, test, type Page, type Route } from '@playwright/test'

// A shared folder with one sub-folder and one file at the root; the sub-folder
// holds one file. Deep-links, refresh, navigation and upload are checked against
// this fixed tree, with the gateway API mocked (no backend/OIDC/provider).

type Entry = {
  id: string
  name: string
  size: number | null
  mimeType: string
  isFolder: boolean
  isOwn: boolean
  uploadedAt: string | null
}

function file(id: string, name: string, size = 12): Entry {
  return { id, name, size, mimeType: 'text/plain', isFolder: false, isOwn: false, uploadedAt: null }
}
function folder(id: string, name: string): Entry {
  return { id, name, size: null, mimeType: 'httpd/unix-directory', isFolder: true, isOwn: false, uploadedAt: null }
}

const ROOT_ENTRIES: Record<string, Entry[]> = {
  '': [folder('d1', 'Unterordner'), file('f1', 'liesmich.txt')],
  Unterordner: [file('f2', 'tief.txt')],
}
const FILE_PATHS: Record<string, Entry> = {
  'liesmich.txt': file('f1', 'liesmich.txt'),
  'Unterordner/tief.txt': file('f2', 'tief.txt'),
}

/**
 * mockGateway installs request interception for the endpoints the app calls. The
 * `uploaded` map makes uploads visible in a subsequent listing, so the
 * "upload lands in the listing" flow is testable end to end.
 */
async function mockGateway(page: Page) {
  const uploaded: Record<string, Entry[]> = {}

  const json = (route: Route, body: unknown, status = 200) =>
    route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })

  await page.route('**/api/me', (route) => json(route, { email: 'recipient@example.com' }))

  await page.route('**/api/shares', (route) =>
    json(route, [
      { id: 'folderhash', name: 'Projekt', size: null, isFolder: true, mode: 2, downloadCount: 0, maxDownloads: null, expiresAt: null, createdAt: '2026-07-01T00:00:00Z' },
      { id: 'filehash', name: 'vertrag.pdf', size: 2048, isFolder: false, mode: 0, downloadCount: 1, maxDownloads: 5, expiresAt: '2026-12-31T00:00:00Z', createdAt: '2026-07-01T00:00:00Z' },
    ]),
  )

  await page.route('**/api/shares/folderhash/upload**', (route) => {
    const path = new URL(route.request().url()).searchParams.get('path') ?? ''
    ;(uploaded[path] ??= []).push({ ...file('u1', 'neu.txt'), isOwn: true })
    return json(route, [], 201)
  })

  await page.route('**/api/shares/folderhash/folder**', (route) => {
    const path = new URL(route.request().url()).searchParams.get('path') ?? ''
    if (path in FILE_PATHS) return json(route, { isFile: true, entries: [], entry: FILE_PATHS[path] })
    if (path in ROOT_ENTRIES) {
      return json(route, { isFile: false, entries: [...ROOT_ENTRIES[path], ...(uploaded[path] ?? [])] })
    }
    return json(route, { error: 'path_not_found' }, 404)
  })

  // A file download responds with an attachment, so the browser fires a
  // `download` event and stays on the page. Registered after the /folder route
  // so Playwright (last-registered-first) checks this more specific one first.
  await page.route('**/api/shares/folderhash/folder/*/content', (route) =>
    route.fulfill({
      status: 200,
      headers: { 'content-disposition': 'attachment; filename="liesmich.txt"' },
      contentType: 'text/plain',
      body: 'hello',
    }),
  )
}

test.beforeEach(async ({ page }) => {
  await mockGateway(page)
})

test('deep-link to a sub-folder loads that folder directly', async ({ page }) => {
  await page.goto('/share/folderhash/Unterordner')

  await expect(page.getByRole('navigation', { name: 'Pfad' })).toContainText('Unterordner')
  await expect(page.getByRole('list', { name: 'Ordnerinhalt' })).toContainText('tief.txt')
})

test('a page refresh stays on the deep-linked sub-folder', async ({ page }) => {
  await page.goto('/share/folderhash/Unterordner')
  await expect(page.getByText('tief.txt')).toBeVisible()

  await page.reload()

  // Still the sub-folder, not bounced to the start page.
  await expect(page).toHaveURL(/\/share\/folderhash\/Unterordner$/)
  await expect(page.getByText('tief.txt')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Meine Freigaben' })).toHaveCount(0)
})

test('navigating into a sub-folder happens in place via the route', async ({ page }) => {
  await page.goto('/share/folderhash')
  await expect(page.getByText('Unterordner')).toBeVisible()

  await page.getByRole('button', { name: 'Ordner öffnen: Unterordner' }).click()

  await expect(page).toHaveURL(/\/share\/folderhash\/Unterordner$/)
  await expect(page.getByText('tief.txt')).toBeVisible()
})

test('a file deep-link opens the file popup with a download action', async ({ page }) => {
  await page.goto('/share/folderhash/liesmich.txt')

  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('liesmich.txt')
  await expect(dialog.getByRole('button', { name: 'Jetzt herunterladen' })).toBeVisible()
})

test('an upload appears in the current listing', async ({ page }) => {
  await page.goto('/share/folderhash')
  await expect(page.getByText('liesmich.txt')).toBeVisible()
  await expect(page.getByText('neu.txt')).toHaveCount(0)

  // The dropzone hides a file input; set files on it to trigger the upload.
  await page.locator('input[type="file"]').setInputFiles({
    name: 'neu.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('fresh upload'),
  })

  await expect(page.getByText('neu.txt')).toBeVisible()
})

test('a single-file share opens its detail popup at its route', async ({ page }) => {
  await page.goto('/share/filehash')

  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('vertrag.pdf')
  await expect(dialog.getByRole('button', { name: 'Jetzt herunterladen' })).toBeVisible()
})

test('clicking a file row starts the download in place (no popup)', async ({ page }) => {
  await page.goto('/share/folderhash')
  await expect(page.getByText('liesmich.txt')).toBeVisible()

  const download = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Herunterladen: liesmich.txt' }).click()
  await (await download).cancel()

  // The listing stays put; no detail dialog opened.
  await expect(page).toHaveURL(/\/share\/folderhash$/)
  await expect(page.getByRole('dialog')).toHaveCount(0)
})

test('a narrow viewport hides the metadata columns but keeps the name', async ({
  page,
}) => {
  // On phones the fixed-width Zugriff/Downloads/Läuft-ab columns would squeeze
  // the name to nothing, so they are hidden below the md breakpoint.
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/')
  await expect(page.getByText('vertrag.pdf')).toBeVisible()
  await expect(page.getByText('Zugriff')).toBeHidden()
  await expect(page.getByText('Downloads')).toBeHidden()
  await expect(page.getByText('Läuft ab')).toBeHidden()

  await page.setViewportSize({ width: 1280, height: 800 })
  await expect(page.getByText('Zugriff')).toBeVisible()
  await expect(page.getByText('Downloads')).toBeVisible()
  await expect(page.getByText('Läuft ab')).toBeVisible()
})

test('the file info icon opens the detail popup', async ({ page }) => {
  await page.goto('/share/folderhash')
  await expect(page.getByText('liesmich.txt')).toBeVisible()

  await page.getByRole('button', { name: 'Details anzeigen: liesmich.txt' }).click()

  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('liesmich.txt')
})

test('the file info popup is addressed by id, not by name', async ({ page }) => {
  // The popup opens via ?file=<id> (the id downloads already use), so a listed
  // file resolves unambiguously even when display names collide.
  await page.goto('/share/folderhash/Unterordner')
  await page.getByRole('button', { name: 'Details anzeigen: tief.txt' }).click()

  await expect(page).toHaveURL(/\/share\/folderhash\/Unterordner\?file=f2$/)
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('tief.txt')
})

test('clicking the header logo returns to the start page', async ({ page }) => {
  await page.goto('/share/folderhash/Unterordner')
  await expect(page.getByText('tief.txt')).toBeVisible()

  await page.getByRole('link', { name: 'Zur Startseite' }).click()

  await expect(page).toHaveURL(/\/$/)
  await expect(
    page.getByRole('heading', { name: 'Meine Freigaben' }),
  ).toBeVisible()
})

test('the file info icon opens the detail popup inside a sub-folder', async ({ page }) => {
  // Reproduce the *navigation* flow (not a deep-link): open the share root,
  // click into the sub-folder, then click a file's info icon there.
  await page.goto('/share/folderhash')
  await page.getByRole('button', { name: 'Ordner öffnen: Unterordner' }).click()
  await expect(page.getByText('tief.txt')).toBeVisible()

  await page.getByRole('button', { name: 'Details anzeigen: tief.txt' }).click()

  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('tief.txt')
})
