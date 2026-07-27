import { Eye, Pencil, Upload, UploadCloud } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { FolderMode } from '@/lib/format'

interface ModeConfig {
  icon: LucideIcon
  className: string
}

// Per-mode icon and colour. The dropzone uses the warn tone to signal that its
// contents are not visible to the recipient.
const MODE_CONFIG: Record<FolderMode, ModeConfig> = {
  'read-only': {
    icon: Eye,
    className: 'bg-[var(--surface-3)] text-[var(--text-2)]',
  },
  'write-all': {
    icon: Pencil,
    className: 'bg-accent text-accent-foreground',
  },
  'write-own': {
    icon: Upload,
    className: 'bg-accent text-accent-foreground',
  },
  dropzone: {
    icon: UploadCloud,
    className: 'bg-[var(--warn-weak)] text-warn',
  },
}

export function ModeBanner({ mode }: { mode: FolderMode }) {
  const { t } = useTranslation()
  const config = MODE_CONFIG[mode]
  const Icon = config.icon
  return (
    <div
      role="note"
      className={`flex items-center gap-2.5 rounded-[10px] px-3.5 py-2.5 text-[13px] font-medium ${config.className}`}
    >
      <Icon className="size-[18px] shrink-0" aria-hidden="true" />
      <span>{t(`modeBanner.${mode}`)}</span>
    </div>
  )
}
