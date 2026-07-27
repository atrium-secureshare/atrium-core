import { ChevronRight, FileText, Folder, Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { FolderEntry } from '@/lib/api'
import { fileTypeLabel, formatSize } from '@/lib/format'

interface Props {
  entries: FolderEntry[]
  /** Open an entry via a route change, so the result is deep-linkable. */
  onOpen: (entry: FolderEntry) => void
  onDownload: (entry: FolderEntry) => void
}

// The server has already applied the share's mode, so this draws whatever it is given.
export function FolderList({ entries, onOpen, onDownload }: Props) {
  const { t } = useTranslation()
  if (entries.length === 0) {
    return (
      <p className="mt-2 text-[13px] text-[var(--text-3,var(--muted-foreground))]">
        {t('folderList.empty')}
      </p>
    )
  }

  return (
    <ul className="flex flex-col" aria-label={t('folderList.aria')}>
      {entries.map((entry) => (
        <li key={entry.id} className="border-b border-border last:border-b-0">
          <div
            role="button"
            tabIndex={0}
            aria-label={
              entry.isFolder
                ? t('folderList.openFolder', { name: entry.name })
                : t('folderList.download', { name: entry.name })
            }
            onClick={() => (entry.isFolder ? onOpen(entry) : onDownload(entry))}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                if (entry.isFolder) onOpen(entry)
                else onDownload(entry)
              }
            }}
            className="flex cursor-pointer items-center gap-3 rounded-[10px] px-2 py-2.5 transition-colors hover:bg-secondary focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring"
          >
            <span
              className="flex size-9 shrink-0 items-center justify-center rounded-[9px] bg-[var(--surface-3)] text-muted-foreground"
              aria-hidden="true"
            >
              {entry.isFolder ? (
                <Folder className="size-[18px]" />
              ) : (
                <FileText className="size-[18px]" />
              )}
            </span>
            <span className="min-w-0 flex-1">
              <span className="flex items-center gap-2">
                <span className="truncate text-[14px] font-medium text-foreground">
                  {entry.name}
                </span>
                {entry.isOwn && (
                  <span className="shrink-0 rounded-full bg-accent px-2 py-0.5 text-[10.5px] font-semibold text-accent-foreground">
                    {t('folderList.yourUpload')}
                  </span>
                )}
              </span>
              <span className="block truncate text-[12px] text-[var(--text-3,var(--muted-foreground))]">
                {entry.isFolder
                  ? t('folderList.folder')
                  : `${fileTypeLabel(entry.name)}${entry.size != null ? ` · ${formatSize(entry.size)}` : ''}`}
              </span>
            </span>
            {entry.isFolder ? (
              <ChevronRight
                className="size-5 shrink-0 text-muted-foreground"
                aria-hidden="true"
              />
            ) : (
              <button
                type="button"
                aria-label={t('folderList.showDetails', { name: entry.name })}
                onClick={(e) => {
                  e.stopPropagation()
                  onOpen(entry)
                }}
                className="flex size-[34px] shrink-0 items-center justify-center rounded-[9px] text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring"
              >
                <Info className="size-[18px]" />
              </button>
            )}
          </div>
        </li>
      ))}
    </ul>
  )
}
