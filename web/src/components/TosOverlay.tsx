import { Fragment, useEffect, useRef, useState, type ReactNode } from 'react'
import { ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { acceptTos, fetchTos } from '@/lib/api'
import { renderMarkdown } from '@/lib/markdown'

interface Props {
  /** Fires after consent is recorded so the app retries its request. */
  onAccepted: () => void
}

// Blocking consent gate shown on 403 tos_required. Deliberately not dismissible
// (no ESC/backdrop close) and focus is trapped on the accept button, since the
// recipient cannot reach the app without accepting.
export function TosOverlay({ onAccepted }: Props) {
  const { t } = useTranslation()
  const acceptRef = useRef<HTMLButtonElement>(null)
  const [content, setContent] = useState<ReactNode[] | null>(null)
  const [accepting, setAccepting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetchTos()
      .then((tos) => setContent(renderMarkdown(tos.content)))
      .catch(() => setError(t('tos.loadError')))
  }, [t])

  useEffect(() => {
    acceptRef.current?.focus()
    function onKeyDown(e: KeyboardEvent) {
      // Keep focus on the only actionable control; nothing else is reachable.
      if (e.key === 'Tab') {
        e.preventDefault()
        acceptRef.current?.focus()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])

  async function accept() {
    setAccepting(true)
    setError(null)
    try {
      await acceptTos()
      onAccepted()
    } catch {
      setError(t('tos.saveError'))
      setAccepting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-30 flex items-center justify-center bg-[rgba(8,13,24,.55)] p-6 backdrop-blur-[3px]">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="tos-title"
        tabIndex={-1}
        className="flex max-h-[calc(100svh-3rem)] w-full max-w-[560px] flex-col rounded-[18px] border border-border bg-card shadow-[var(--shadow-lg,0_28px_64px_-22px_rgba(15,23,42,.42))] outline-none motion-safe:animate-[dialog-pop_.22s_cubic-bezier(.2,.8,.2,1)]"
      >
        <div className="flex items-start gap-3 border-b border-border p-5">
          <span
            className="flex size-[52px] shrink-0 items-center justify-center rounded-[12px] bg-accent text-accent-foreground"
            aria-hidden="true"
          >
            <ShieldCheck className="size-6" />
          </span>
          <div className="min-w-0 flex-1">
            <h2
              id="tos-title"
              className="text-[18px] font-bold text-foreground"
            >
              {t('tos.title')}
            </h2>
            <div className="text-[12.5px] text-[var(--text-3,var(--muted-foreground))]">
              {t('tos.subtitle')}
            </div>
          </div>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {content ? (
            <div className="flex flex-col gap-3 text-[14px] leading-relaxed text-foreground">
              {content.map((node, i) => (
                <Fragment key={i}>{node}</Fragment>
              ))}
            </div>
          ) : error ? (
            <p className="text-[14px] text-destructive">{error}</p>
          ) : (
            <p className="text-[14px] text-muted-foreground">
              {t('tos.loading')}
            </p>
          )}
        </div>

        <div className="flex flex-col gap-2 border-t border-border p-5">
          {content && error && (
            <p className="text-[13px] text-destructive">{error}</p>
          )}
          <button
            ref={acceptRef}
            type="button"
            onClick={accept}
            disabled={!content || accepting}
            className="flex w-full items-center justify-center gap-2 rounded-[10px] bg-primary py-3 text-[14px] font-semibold text-primary-foreground transition-colors hover:bg-[var(--primary-hover)] focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring focus-visible:outline-offset-2 disabled:cursor-not-allowed disabled:opacity-60"
          >
            {accepting ? t('tos.saving') : t('tos.accept')}
          </button>
        </div>
      </div>
    </div>
  )
}
