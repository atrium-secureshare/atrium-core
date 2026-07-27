// API layer for the recipient frontend. Every call is same-origin and relies on
// the gateway's HMAC session cookie, so no tokens are handled here; a 401 sends
// the browser to OIDC login.

export interface Share {
  id: string
  name: string
  size: number | null
  isFolder: boolean
  /** Sharing mode (0=read-only, 1=write/read-own, 2=write/read-all, 3=dropzone), not a bitmask. */
  mode: number
  downloadCount: number
  maxDownloads: number | null
  expiresAt: string | null
  createdAt: string | null
}

export interface FolderEntry {
  id: string
  name: string
  size: number | null
  mimeType: string
  isFolder: boolean
  /** isOwn marks a file the current recipient uploaded (drives an "own" badge). */
  isOwn: boolean
  uploadedAt: string | null
}

// Either a folder's entries, or — when the path resolves to a file (isFile) —
// that single entry, so a file deep-link resolves in one call.
export interface FolderListing {
  isFile: boolean
  entries: FolderEntry[]
  entry?: FolderEntry
}

export interface Tos {
  version: string
  content: string
}

/** Carries the HTTP status so callers can branch. */
export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

// Thrown on 403 {"error":"tos_required"}: authenticated but consent missing.
// Callers show the blocking consent overlay and retry after acceptance.
export class TosRequiredError extends Error {
  constructor() {
    super('tos_required')
    this.name = 'TosRequiredError'
  }
}

function redirectToLogin(): never {
  window.location.assign('/auth/login')
  // Nothing after the navigation runs meaningfully.
  throw new ApiError(401, 'redirecting to login')
}

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(path, { headers: { Accept: 'application/json' } })
  if (res.status === 401) redirectToLogin()
  if (res.status === 403 && (await isTosRequired(res)))
    throw new TosRequiredError()
  if (!res.ok) throw new ApiError(res.status, `${path} failed: ${res.status}`)
  return (await res.json()) as T
}

async function isTosRequired(res: Response): Promise<boolean> {
  const body = (await res.json().catch(() => null)) as { error?: string } | null
  return body?.error === 'tos_required'
}

export function getMe(): Promise<{ email: string }> {
  return getJSON('/api/me')
}

export function fetchTos(): Promise<Tos> {
  return getJSON('/api/tos')
}

// On success the gateway sets the long-lived tos_accepted cookie and the caller retries.
export async function acceptTos(): Promise<void> {
  const res = await fetch('/api/tos/accept', {
    method: 'POST',
    headers: { Accept: 'application/json' },
  })
  if (res.status === 401) redirectToLogin()
  if (!res.ok)
    throw new ApiError(res.status, `accept tos failed: ${res.status}`)
}

export function listShares(): Promise<Share[]> {
  return getJSON('/api/shares')
}

// Browses a shared folder at a relative path. The provider already applied the
// share's mode and resolved the path traversal-safely; a file path yields isFile.
export function listFolder(shareId: string, path = ''): Promise<FolderListing> {
  const query = path ? `?path=${encodeURIComponent(path)}` : ''
  return getJSON(`/api/shares/${encodeURIComponent(shareId)}/folder${query}`)
}

export function contentUrl(shareId: string): string {
  return `/api/shares/${encodeURIComponent(shareId)}/content`
}

export function folderFileUrl(shareId: string, fileId: string): string {
  return `/api/shares/${encodeURIComponent(shareId)}/folder/${encodeURIComponent(fileId)}/content`
}

// Starts a download without navigating away; the response is Content-Disposition: attachment.
export function triggerDownload(shareId: string): void {
  startDownload(contentUrl(shareId))
}

export function triggerFolderFileDownload(
  shareId: string,
  fileId: string,
): void {
  startDownload(folderFileUrl(shareId, fileId))
}

// Clicks a hidden link so the browser saves the file in place.
function startDownload(href: string): void {
  const a = document.createElement('a')
  a.href = href
  a.rel = 'noopener'
  document.body.appendChild(a)
  a.click()
  a.remove()
}

// Streams a file into a writable share with progress. Uses XMLHttpRequest because
// fetch cannot report upload progress; the body is the raw file, named via header.
export function uploadFile(
  shareId: string,
  file: File,
  path = '',
  onProgress?: (fraction: number) => void,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    const query = path ? `?path=${encodeURIComponent(path)}` : ''
    xhr.open(
      'POST',
      `/api/shares/${encodeURIComponent(shareId)}/upload${query}`,
    )
    // HTTP headers are latin1; percent-encode so non-ASCII filenames survive.
    xhr.setRequestHeader('X-Atrium-Filename', encodeURIComponent(file.name))
    xhr.setRequestHeader(
      'Content-Type',
      file.type || 'application/octet-stream',
    )

    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) onProgress(e.loaded / e.total)
    }
    xhr.onload = () => {
      if (xhr.status === 401) {
        redirectToLogin()
        return
      }
      if (xhr.status >= 200 && xhr.status < 300) resolve()
      else reject(new ApiError(xhr.status, `upload failed: ${xhr.status}`))
    }
    xhr.onerror = () => reject(new ApiError(0, 'upload network error'))
    xhr.send(file)
  })
}
