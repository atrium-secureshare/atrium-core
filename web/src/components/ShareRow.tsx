import { ChevronRight, Folder, FileText, Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { Share } from '@/lib/api'
import {
  expiresSoon,
  fileTypeLabel,
  formatDate,
  formatDownloads,
  formatSize,
  modeLabel,
  modeOf,
} from '@/lib/format'
import { cn } from '@/lib/utils'

// Shared grid template so the header and every row line up. On small screens only
// Name and the action column show; the metadata cells are `hidden md:block`.
export const ROW_GRID =
  'grid grid-cols-[minmax(0,1fr)_44px] items-center gap-3.5 md:grid-cols-[minmax(0,1fr)_160px_104px_136px_44px]'

interface Props {
  share: Share
  onActivate: (share: Share) => void
  onDownload: (share: Share) => void
}

export function ShareRow({ share, onActivate, onDownload }: Props) {
  const { t } = useTranslation()
  const mode = modeOf(share)
  const soon = expiresSoon(share.expiresAt)

  const meta = share.isFolder
    ? t('shareRow.folder')
    : `${fileTypeLabel(share.name)}${share.size != null ? ` · ${formatSize(share.size)}` : ''}`

  return (
    <div
      role="button"
      tabIndex={0}
      aria-label={
        share.isFolder
          ? t('shareRow.openFolder', { name: share.name })
          : t('shareRow.download', { name: share.name })
      }
      onClick={() => (share.isFolder ? onActivate(share) : onDownload(share))}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          if (share.isFolder) onActivate(share)
          else onDownload(share)
        }
      }}
      className={cn(
        ROW_GRID,
        'cursor-pointer rounded-[10px] border-b border-border px-2.5 py-3 transition-colors hover:bg-secondary focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring',
      )}
    >
      <div className="flex min-w-0 items-center gap-3">
        <span
          className={cn(
            'flex size-[38px] shrink-0 items-center justify-center rounded-[10px]',
            share.isFolder
              ? 'bg-accent text-accent-foreground'
              : 'bg-[var(--file-chip)] text-[var(--file-icon)]',
          )}
          aria-hidden="true"
        >
          {share.isFolder ? (
            <Folder className="size-5" />
          ) : (
            <FileText className="size-5" />
          )}
        </span>
        <span className="min-w-0">
          <span className="block truncate text-[14.5px] font-semibold text-foreground">
            {share.name}
          </span>
          <span className="block truncate text-[12px] text-[var(--text-3,var(--muted-foreground))]">
            {meta}
          </span>
        </span>
      </div>

      <span className="hidden truncate text-[13px] text-[var(--text-2)] md:block">
        {modeLabel(mode)}
      </span>

      <span className="hidden text-[13px] text-[var(--text-2)] md:block">
        {formatDownloads(share)}
      </span>

      <span
        className={cn(
          'hidden text-[13px] md:block',
          soon ? 'font-medium text-warn' : 'text-[var(--text-2)]',
        )}
      >
        {formatDate(share.expiresAt)}
      </span>

      <div className="flex justify-end">
        {share.isFolder ? (
          <ChevronRight
            className="size-5 text-muted-foreground"
            aria-hidden="true"
          />
        ) : (
          <button
            type="button"
            aria-label={t('shareRow.showDetails', { name: share.name })}
            onClick={(e) => {
              e.stopPropagation()
              onActivate(share)
            }}
            className="flex size-[34px] items-center justify-center rounded-[9px] text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring"
          >
            <Info className="size-[18px]" />
          </button>
        )}
      </div>
    </div>
  )
}
