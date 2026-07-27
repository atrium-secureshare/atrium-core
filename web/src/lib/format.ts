// Locale-aware formatting and share-domain helpers. Formatting follows the active
// UI language; callers re-render on a language change and re-invoke with the new locale.
import i18n from '@/i18n'

// The continental languages use their Swiss variant (dd.MM.yyyy, comma decimals);
// English falls back to en-GB so dates stay day-first.
const INTL_LOCALE: Record<string, string> = {
  de: 'de-CH',
  fr: 'fr-CH',
  it: 'it-CH',
  en: 'en-GB',
}

function intlLocale(): string {
  return INTL_LOCALE[i18n.resolvedLanguage ?? 'en'] ?? 'en-GB'
}

/** Sharing mode ids, matching the provider. */
export const ShareMode = {
  ReadOnly: 0,
  WriteOwn: 1,
  WriteAll: 2,
  Dropzone: 3,
} as const

export type FolderMode = 'read-only' | 'write-own' | 'write-all' | 'dropzone'

export function modeOf(share: { mode: number }): FolderMode {
  switch (share.mode) {
    case ShareMode.WriteOwn:
      return 'write-own'
    case ShareMode.WriteAll:
      return 'write-all'
    case ShareMode.Dropzone:
      return 'dropzone'
    default:
      return 'read-only'
  }
}

export function canUpload(share: { isFolder: boolean; mode: number }): boolean {
  return (
    share.isFolder &&
    (share.mode === ShareMode.WriteOwn ||
      share.mode === ShareMode.WriteAll ||
      share.mode === ShareMode.Dropzone)
  )
}

export function canBrowse(share: { isFolder: boolean; mode: number }): boolean {
  return share.isFolder && share.mode !== ShareMode.Dropzone
}

const MODE_LABEL_KEY: Record<FolderMode, string> = {
  'read-only': 'format.modeReadOnly',
  'write-own': 'format.modeWriteOwn',
  'write-all': 'format.modeWriteAll',
  dropzone: 'format.modeDropzone',
}

export function modeLabel(mode: FolderMode): string {
  return i18n.t(MODE_LABEL_KEY[mode])
}

// Uses SI (1000), not binary, to match the handoff.
export function formatSize(bytes: number): string {
  if (bytes >= 1_000_000) {
    return (
      (bytes / 1_000_000).toLocaleString(intlLocale(), {
        minimumFractionDigits: 1,
        maximumFractionDigits: 1,
      }) + ' MB'
    )
  }
  if (bytes >= 1_000) {
    return Math.round(bytes / 1_000).toLocaleString(intlLocale()) + ' KB'
  }
  return bytes + ' B'
}

export function formatDate(iso: string | null | undefined): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleDateString(intlLocale(), {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
  })
}

export function daysUntil(iso: string | null | undefined): number | null {
  if (!iso) return null
  const target = new Date(iso)
  if (Number.isNaN(target.getTime())) return null
  const msPerDay = 86_400_000
  const startOfToday = new Date()
  startOfToday.setHours(0, 0, 0, 0)
  return Math.ceil((target.getTime() - startOfToday.getTime()) / msPerDay)
}

export function expiryHint(iso: string | null | undefined): string {
  const days = daysUntil(iso)
  if (days === null) return i18n.t('format.unlimited')
  if (days < 0) return i18n.t('format.expired')
  if (days === 0) return i18n.t('format.today')
  return i18n.t('format.daysRemaining', { count: days })
}

/** Flags an expiry within 3 days (drives the warn colour). */
export function expiresSoon(iso: string | null | undefined): boolean {
  const days = daysUntil(iso)
  return days !== null && days <= 3
}

export function fileTypeLabel(name: string): string {
  const dot = name.lastIndexOf('.')
  if (dot <= 0 || dot === name.length - 1) return i18n.t('format.file')
  return name.slice(dot + 1).toUpperCase()
}

export function formatDownloads(share: {
  isFolder: boolean
  downloadCount: number
  maxDownloads: number | null
}): string {
  if (share.isFolder) return '—'
  if (share.maxDownloads != null)
    return `${share.downloadCount} / ${share.maxDownloads}`
  return String(share.downloadCount)
}
