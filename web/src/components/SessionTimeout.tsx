import { useCallback, useEffect, useRef, useState } from 'react'
import { Clock } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getMe } from '@/lib/api'
import { useSessionExpiry } from '@/hooks/useSessionExpiry'

// Where an idle logout lands: a client route that loads no authenticated
// endpoint, so it confirms the logout instead of bouncing into a fresh login.
const LOGGED_OUT_URL = '/auth/logged-out?reason=idle'

function countdown(seconds: number): string {
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`
}

// Warns before the gateway ends an idle session, and hands over to the
// logged-out page once the deadline passes.
export function SessionTimeout() {
  const { t } = useTranslation()
  const secondsLeft = useSessionExpiry()
  const stayRef = useRef<HTMLButtonElement>(null)
  const [extending, setExtending] = useState(false)
  const warning = Number.isFinite(secondsLeft) && secondsLeft > 0

  useEffect(() => {
    if (secondsLeft <= 0) window.location.assign(LOGGED_OUT_URL)
  }, [secondsLeft])

  useEffect(() => {
    if (warning) stayRef.current?.focus()
  }, [warning])

  // Any authenticated request counts as activity and its response carries the
  // new deadline, so no dedicated endpoint is needed to stay signed in.
  const extend = useCallback(() => {
    setExtending(true)
    getMe()
      .catch(() => {
        /* 401 redirects to login in the API layer. */
      })
      .finally(() => setExtending(false))
  }, [])

  if (!warning) return null

  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-[rgba(8,13,24,.55)] p-6 backdrop-blur-[3px]">
      <div
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="session-timeout-title"
        tabIndex={-1}
        className="w-full max-w-[420px] rounded-[18px] border border-border bg-card p-5 shadow-[var(--shadow-lg,0_28px_64px_-22px_rgba(15,23,42,.42))] outline-none motion-safe:animate-[dialog-pop_.22s_cubic-bezier(.2,.8,.2,1)]"
      >
        <div className="flex items-start gap-3">
          <span
            className="flex size-[52px] shrink-0 items-center justify-center rounded-[12px] bg-accent text-accent-foreground"
            aria-hidden="true"
          >
            <Clock className="size-6" />
          </span>
          <div className="min-w-0 flex-1">
            <h2
              id="session-timeout-title"
              className="text-[18px] font-bold text-foreground"
            >
              {t('session.warningTitle')}
            </h2>
            <p className="mt-1 text-[14px] leading-relaxed text-[var(--text-3,var(--muted-foreground))]">
              {t('session.warningBody', { time: countdown(secondsLeft) })}
            </p>
          </div>
        </div>

        <button
          ref={stayRef}
          type="button"
          onClick={extend}
          disabled={extending}
          className="mt-5 flex w-full items-center justify-center gap-2 rounded-[10px] bg-primary py-3 text-[14px] font-semibold text-primary-foreground transition-colors hover:bg-[var(--primary-hover)] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring disabled:cursor-not-allowed disabled:opacity-60"
        >
          {t('session.stay')}
        </button>
      </div>
    </div>
  )
}
