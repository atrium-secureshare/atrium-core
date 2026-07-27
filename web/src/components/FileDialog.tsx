import { useEffect, useRef } from 'react'
import { Download, FileText, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  expiryHint,
  expiresSoon,
  fileTypeLabel,
  formatDate,
  formatSize,
  modeLabel,
  type FolderMode,
} from '@/lib/format'
import { cn } from '@/lib/utils'

export interface ShareMeta {
  mode: FolderMode
  createdAt: string | null
  expiresAt: string | null
  downloadCount: number
  maxDownloads: number | null
}

interface Props {
  name: string
  size: number | null
  onDownload: () => void
  onClose: () => void
  /** When present, renders the full share metadata grid (single-file share). */
  meta?: ShareMeta
  /** Marks a folder file the recipient uploaded. */
  isOwn?: boolean
}

function InfoTile({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <div className="bg-card p-3.5">
      <div className="mb-1 text-[10.5px] font-semibold uppercase tracking-[0.05em] text-[var(--text-3,var(--muted-foreground))]">
        {label}
      </div>
      <div className="text-[13.5px] text-foreground">{children}</div>
    </div>
  )
}

// Modal for a single file (a single-file share or a file inside a folder).
// Accessible: role="dialog", labelled by the title, focus trapped, ESC/backdrop close.
export function FileDialog({
  name,
  size,
  onDownload,
  onClose,
  meta,
  isOwn,
}: Props) {
  const { t } = useTranslation()
  const cardRef = useRef<HTMLDivElement>(null)
  const soon = meta ? expiresSoon(meta.expiresAt) : false

  useEffect(() => {
    const card = cardRef.current
    card?.focus()

    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        onClose()
        return
      }
      if (e.key !== 'Tab' || !card) return
      const focusable = card.querySelectorAll<HTMLElement>(
        'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
      )
      if (focusable.length === 0) return
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }

    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [onClose])

  return (
    <div
      className="fixed inset-0 z-20 flex items-center justify-center bg-[rgba(8,13,24,.55)] p-6 backdrop-blur-[3px]"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div
        ref={cardRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="file-title"
        tabIndex={-1}
        className="w-full max-w-[460px] rounded-[18px] border border-border bg-card shadow-[var(--shadow-lg,0_28px_64px_-22px_rgba(15,23,42,.42))] outline-none motion-safe:animate-[dialog-pop_.22s_cubic-bezier(.2,.8,.2,1)]"
      >
        <div className="flex items-start gap-3 p-5">
          <span
            className="flex size-[52px] shrink-0 items-center justify-center rounded-[12px] bg-[var(--surface-3)] text-muted-foreground"
            aria-hidden="true"
          >
            <FileText className="size-6" />
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <h2
                id="file-title"
                className="truncate text-[18px] font-bold text-foreground"
              >
                {name}
              </h2>
              {isOwn && (
                <span className="shrink-0 rounded-full bg-accent px-2 py-0.5 text-[10.5px] font-semibold text-accent-foreground">
                  {t('fileDialog.yourUpload')}
                </span>
              )}
            </div>
            <div className="text-[12.5px] text-[var(--text-3,var(--muted-foreground))]">
              {fileTypeLabel(name)}
              {size != null ? ` · ${formatSize(size)}` : ''}
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label={t('fileDialog.close')}
            className="flex size-8 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-secondary focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring"
          >
            <X className="size-[18px]" />
          </button>
        </div>

        <div className="px-5 pb-5">
          {meta && (
            <div className="grid grid-cols-2 gap-px overflow-hidden rounded-[12px] bg-border">
              <InfoTile label={t('fileDialog.permission')}>
                {modeLabel(meta.mode)}
              </InfoTile>
              <InfoTile label={t('fileDialog.sharedOn')}>
                {formatDate(meta.createdAt)}
              </InfoTile>
              <InfoTile label={t('fileDialog.downloads')}>
                {meta.maxDownloads != null ? (
                  <div className="flex flex-col gap-1.5">
                    <span>
                      {meta.downloadCount} / {meta.maxDownloads}
                    </span>
                    <span className="h-1.5 overflow-hidden rounded-full bg-[var(--surface-3)]">
                      <span
                        className="block h-full rounded-full bg-primary"
                        style={{
                          width: `${Math.min(100, Math.round((meta.downloadCount / meta.maxDownloads) * 100))}%`,
                        }}
                      />
                    </span>
                  </div>
                ) : (
                  meta.downloadCount
                )}
              </InfoTile>
              <InfoTile label={t('fileDialog.expires')}>
                <span className={cn(soon && 'font-medium text-warn')}>
                  {formatDate(meta.expiresAt)}
                  <span className="ml-1 text-[12px] text-[var(--text-3,var(--muted-foreground))]">
                    ({expiryHint(meta.expiresAt)})
                  </span>
                </span>
              </InfoTile>
            </div>
          )}

          <button
            type="button"
            onClick={onDownload}
            className="mt-4 flex w-full items-center justify-center gap-2 rounded-[10px] bg-primary py-3 text-[14px] font-semibold text-primary-foreground transition-colors hover:bg-[var(--primary-hover)] focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring focus-visible:outline-offset-2"
          >
            <Download className="size-[18px]" />
            {t('fileDialog.download')}
          </button>
        </div>
      </div>
    </div>
  )
}
