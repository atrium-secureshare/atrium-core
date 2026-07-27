import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { triggerDownload, type Share } from '@/lib/api'
import { shareUrl } from '@/lib/sharePath'
import { ShareList } from './ShareList'

interface Props {
  shares: Share[]
  loading: boolean
  error: string | null
  onToast: (msg: string) => void
  /** Refreshes the listing so the download counter updates in place. */
  onDownloaded: () => void
}

export function SharesView({
  shares,
  loading,
  error,
  onToast,
  onDownloaded,
}: Props) {
  const navigate = useNavigate()
  const { t } = useTranslation()

  function download(share: Share) {
    triggerDownload(share.id)
    onToast(t('shares.downloadStarted', { name: share.name }))
    onDownloaded()
  }

  return (
    <>
      <h1 className="mb-6 text-[26px] font-bold tracking-tight text-foreground">
        {t('shares.title')}
      </h1>

      {loading ? (
        <p className="text-[14px] text-muted-foreground">
          {t('shares.loading')}
        </p>
      ) : error ? (
        <p className="text-[14px] text-destructive">{error}</p>
      ) : shares.length === 0 ? (
        <div className="rounded-[14px] border border-border bg-card px-6 py-16 text-center">
          <p className="text-[15px] font-semibold text-foreground">
            {t('shares.emptyTitle')}
          </p>
          <p className="mt-1 text-[13.5px] text-muted-foreground">
            {t('shares.emptyBody')}
          </p>
        </div>
      ) : (
        <ShareList
          shares={shares}
          onActivate={(share) => navigate(shareUrl(share.id))}
          onDownload={download}
        />
      )}
    </>
  )
}
