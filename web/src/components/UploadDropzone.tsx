import { useRef, useState } from 'react'
import { UploadCloud } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { uploadFile } from '@/lib/api'
import { cn } from '@/lib/utils'

interface Props {
  shareId: string
  /** Relative sub-path of the folder to upload into (empty for the share root). */
  path?: string
  /** 'inline' is the slim bar above a list; 'card' is the large dropzone-only view. */
  variant?: 'inline' | 'card'
  onUploaded: () => void
  onToast: (msg: string) => void
}

// Streams files into a writable share one at a time with per-file progress, into
// the currently-browsed sub-folder (path); success triggers a reload.
export function UploadDropzone({
  shareId,
  path = '',
  variant = 'inline',
  onUploaded,
  onToast,
}: Props) {
  const { t } = useTranslation()
  const inputRef = useRef<HTMLInputElement>(null)
  const [dragging, setDragging] = useState(false)
  const [busy, setBusy] = useState<{ name: string; fraction: number } | null>(
    null,
  )

  async function handleFiles(files: FileList | null) {
    if (!files || files.length === 0 || busy) return
    for (const file of Array.from(files)) {
      setBusy({ name: file.name, fraction: 0 })
      try {
        await uploadFile(shareId, file, path, (fraction) =>
          setBusy({ name: file.name, fraction }),
        )
        onToast(t('upload.uploaded', { name: file.name }))
      } catch {
        onToast(t('upload.uploadFailed', { name: file.name }))
      }
    }
    setBusy(null)
    onUploaded()
  }

  const card = variant === 'card'

  return (
    <div
      onDragOver={(e) => {
        e.preventDefault()
        setDragging(true)
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={(e) => {
        e.preventDefault()
        setDragging(false)
        void handleFiles(e.dataTransfer.files)
      }}
      className={cn(
        'rounded-[14px] border border-dashed border-[var(--input)] bg-background text-center transition-colors',
        card ? 'px-8 py-12' : 'px-6 py-5',
        dragging && 'border-primary bg-secondary',
      )}
    >
      <button
        type="button"
        onClick={() => inputRef.current?.click()}
        disabled={!!busy}
        className="flex w-full flex-col items-center gap-3 disabled:opacity-70"
      >
        <span
          className={cn(
            'flex items-center justify-center rounded-[12px] bg-accent text-accent-foreground',
            card ? 'size-14' : 'size-10',
          )}
          aria-hidden="true"
        >
          <UploadCloud className={card ? 'size-7' : 'size-5'} />
        </span>
        {card && (
          <span className="text-[16px] font-semibold text-foreground">
            {t('upload.title')}
          </span>
        )}
        <span
          className={cn(
            'text-[var(--text-3,var(--muted-foreground))]',
            card ? 'text-[13.5px]' : 'text-[13px]',
          )}
        >
          {busy ? t('upload.busy', { name: busy.name }) : t('upload.idle')}
        </span>
      </button>

      {busy && (
        <div className="mx-auto mt-4 h-1.5 w-full max-w-sm overflow-hidden rounded-full bg-[var(--surface-3)]">
          <div
            className="h-full rounded-full bg-primary transition-[width]"
            style={{ width: `${Math.round(busy.fraction * 100)}%` }}
          />
        </div>
      )}

      <input
        ref={inputRef}
        type="file"
        multiple
        className="hidden"
        onChange={(e) => void handleFiles(e.target.files)}
      />
    </div>
  )
}
