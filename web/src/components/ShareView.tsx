import { useEffect, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  listFolder,
  triggerDownload,
  triggerFolderFileDownload,
  type FolderEntry,
  type Share,
} from '@/lib/api'
import { canBrowse, canUpload, modeOf } from '@/lib/format'
import { joinPath, parentPath, segments, shareUrl } from '@/lib/sharePath'
import { Breadcrumb, type Crumb } from './Breadcrumb'
import { FileDialog } from './FileDialog'
import { FolderList } from './FolderList'
import { ModeBanner } from './ModeBanner'
import { UploadDropzone } from './UploadDropzone'

interface Props {
  shares: Share[]
  sharesLoading: boolean
  onToast: (msg: string) => void
  /** Refreshes the listing so the download counter updates in place. */
  onDownloaded: () => void
}

// In-place folder browser for one share, driven entirely by the URL
// (/share/:hash/*), so a deep-link and a refresh land on the same place. The
// share token in :hash is the only access anchor — sub-folders carry no id.
export function ShareView({
  shares,
  sharesLoading,
  onToast,
  onDownloaded,
}: Props) {
  const { hash = '' } = useParams()
  const subPath = useParams()['*'] ?? ''
  const navigate = useNavigate()
  const { t } = useTranslation()
  // The detail popup is addressed by file id (?file=<id>), not by name, since names are not unique.
  const [searchParams] = useSearchParams()
  const fileId = searchParams.get('file')
  const share = shares.find((s) => s.id === hash)

  const isFolder = share?.isFolder ?? false
  const browsable = share ? canBrowse(share) : false
  const uploadable = share ? canUpload(share) : false

  const [folderPath, setFolderPath] = useState('')
  const [entries, setEntries] = useState<FolderEntry[] | null>(null)
  const [popup, setPopup] = useState<FolderEntry | null>(null)
  const [error, setError] = useState(false)
  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    if (!share || !isFolder || !browsable) return
    let active = true
    setError(false)
    ;(async () => {
      try {
        const res = await listFolder(hash, subPath)
        if (!active) return
        if (res.isFile) {
          // Keep the containing folder behind the popup (entries not cleared) so opening a file causes no flash.
          const parent = parentPath(subPath)
          const listing = await listFolder(hash, parent)
          if (!active) return
          setEntries(listing.entries)
          setFolderPath(parent)
          setPopup(res.entry ?? null)
        } else {
          setEntries(res.entries)
          setFolderPath(subPath)
          setPopup(null)
        }
      } catch {
        if (active) setError(true)
      }
    })()
    return () => {
      active = false
    }
  }, [hash, subPath, share, isFolder, browsable, reloadKey])

  if (!share) {
    return sharesLoading ? (
      <p className="text-[14px] text-muted-foreground">
        {t('shareView.loading')}
      </p>
    ) : (
      <NotFound onBack={() => navigate('/')} />
    )
  }

  // A single-file share: no listing, just its detail popup.
  if (!isFolder) {
    return (
      <>
        <Breadcrumb
          items={[{ label: t('shares.title'), to: '/' }, { label: share.name }]}
        />
        <FileDialog
          name={share.name}
          size={share.size}
          meta={{
            mode: modeOf(share),
            createdAt: share.createdAt,
            expiresAt: share.expiresAt,
            downloadCount: share.downloadCount,
            maxDownloads: share.maxDownloads,
          }}
          onDownload={() => {
            triggerDownload(share.id)
            onToast(t('shares.downloadStarted', { name: share.name }))
            onDownloaded()
          }}
          onClose={() => navigate('/')}
        />
      </>
    )
  }

  // The open file popup: either a `?file=<id>` from the listing, or the entry the server resolved into `popup`.
  const activePopup =
    popup ??
    (fileId && entries ? (entries.find((e) => e.id === fileId) ?? null) : null)

  const crumbs: Crumb[] = [
    { label: t('shares.title'), to: '/' },
    { label: share.name, to: shareUrl(hash) },
  ]
  let acc = ''
  for (const seg of segments(folderPath)) {
    acc = joinPath(acc, seg)
    crumbs.push({ label: seg, to: shareUrl(hash, acc) })
  }
  if (activePopup) crumbs.push({ label: activePopup.name })

  return (
    <>
      <Breadcrumb items={crumbs} />

      <div className="flex flex-col gap-4">
        <ModeBanner mode={modeOf(share)} />

        {browsable &&
          (error ? (
            <p className="text-[13px] text-destructive">
              {t('shareView.folderLoadError')}
            </p>
          ) : entries === null ? (
            <p className="text-[13px] text-muted-foreground">
              {t('shareView.contentLoading')}
            </p>
          ) : (
            <FolderList
              entries={entries}
              onOpen={(entry) =>
                navigate(
                  entry.isFolder
                    ? shareUrl(hash, joinPath(folderPath, entry.name))
                    : `${shareUrl(hash, folderPath)}?file=${encodeURIComponent(entry.id)}`,
                )
              }
              onDownload={(entry) => {
                triggerFolderFileDownload(hash, entry.id)
                onToast(t('shares.downloadStarted', { name: entry.name }))
                onDownloaded()
              }}
            />
          ))}

        {uploadable && (
          <UploadDropzone
            shareId={hash}
            path={folderPath}
            variant={browsable ? 'inline' : 'card'}
            onUploaded={() => setReloadKey((k) => k + 1)}
            onToast={onToast}
          />
        )}
      </div>

      {activePopup && (
        <FileDialog
          name={activePopup.name}
          size={activePopup.size}
          isOwn={activePopup.isOwn}
          onDownload={() => {
            triggerFolderFileDownload(hash, activePopup.id)
            onToast(t('shares.downloadStarted', { name: activePopup.name }))
            onDownloaded()
          }}
          onClose={() => navigate(shareUrl(hash, folderPath))}
        />
      )}
    </>
  )
}

function NotFound({ onBack }: { onBack: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="rounded-[14px] border border-border bg-card px-6 py-16 text-center">
      <p className="text-[15px] font-semibold text-foreground">
        {t('notFound.title')}
      </p>
      <p className="mt-1 text-[13.5px] text-muted-foreground">
        {t('notFound.body')}
      </p>
      <button
        type="button"
        onClick={onBack}
        className="mt-4 text-[13.5px] font-semibold text-primary hover:underline"
      >
        {t('notFound.back')}
      </button>
    </div>
  )
}
