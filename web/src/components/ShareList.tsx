import { useTranslation } from 'react-i18next'
import type { Share } from '@/lib/api'
import { ROW_GRID, ShareRow } from './ShareRow'

interface Props {
  shares: Share[]
  onActivate: (share: Share) => void
  onDownload: (share: Share) => void
}

// Flat "shared with me" listing. No card/border by design; the backend exposes
// shares as a flat set, not a browsable tree.
export function ShareList({ shares, onActivate, onDownload }: Props) {
  const { t } = useTranslation()
  return (
    <div role="table" aria-label={t('shareList.aria')}>
      <div
        role="row"
        className={`${ROW_GRID} border-b border-border px-2.5 pb-2.5 pt-1 text-[10.5px] font-semibold uppercase tracking-[0.05em] text-[var(--text-3,var(--muted-foreground))]`}
      >
        <span role="columnheader">{t('shareList.name')}</span>
        <span role="columnheader" className="hidden md:block">
          {t('shareList.access')}
        </span>
        <span role="columnheader" className="hidden md:block">
          {t('shareList.downloads')}
        </span>
        <span role="columnheader" className="hidden md:block">
          {t('shareList.expires')}
        </span>
        <span role="columnheader" aria-label={t('shareList.action')} />
      </div>
      {shares.map((share) => (
        <ShareRow
          key={share.id}
          share={share}
          onActivate={onActivate}
          onDownload={onDownload}
        />
      ))}
    </div>
  )
}
