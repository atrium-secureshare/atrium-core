import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { ChevronDown, LogOut } from 'lucide-react'
import { brand } from '@/config'
import type { Theme } from '@/hooks/useTheme'
import { LanguageSwitcher } from './LanguageSwitcher'
import { ThemeToggle } from './ThemeToggle'

// Logos are served under /branding/, not bundled, so an operator can override them.
const logo = '/branding/logo.svg'
const logoDark = '/branding/logo-dark.svg'

function initials(email: string): string {
  const local = email.split('@')[0] ?? email
  const parts = local.split(/[.\-_+]/).filter(Boolean)
  const letters =
    parts.length >= 2 ? parts[0][0] + parts[1][0] : local.slice(0, 2)
  return letters.toUpperCase()
}

// Logout is a real form POST (not fetch) so the browser follows the gateway's
// redirect to the end_session_endpoint, ending the SSO session, not just the cookie.
function UserMenu({ email }: { email: string }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    function onPointerDown(e: PointerEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('pointerdown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('pointerdown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t('header.accountMenu')}
        className="flex items-center gap-2 rounded-full border border-border bg-secondary py-1 pl-1 pr-2.5 transition-colors hover:bg-[var(--surface-3)]"
      >
        <span
          className="flex size-7 items-center justify-center rounded-full bg-primary text-[11px] font-semibold text-primary-foreground"
          aria-hidden="true"
        >
          {initials(email)}
        </span>
        <span className="hidden text-[13px] text-[var(--text-2)] sm:inline">
          {email}
        </span>
        <ChevronDown
          className={`size-4 text-[var(--text-2)] transition-transform ${open ? 'rotate-180' : ''}`}
          aria-hidden="true"
        />
      </button>

      {open && (
        <div
          role="menu"
          className="absolute right-0 top-[calc(100%+8px)] min-w-[180px] overflow-hidden rounded-xl border border-border bg-background py-1 shadow-lg"
        >
          <form method="POST" action="/auth/logout">
            <button
              type="submit"
              role="menuitem"
              className="flex w-full items-center gap-2 px-4 py-2 text-left text-[13px] text-foreground transition-colors hover:bg-secondary"
            >
              <LogOut className="size-4" aria-hidden="true" />
              {t('header.logout')}
            </button>
          </form>
        </div>
      )}
    </div>
  )
}

interface Props {
  email: string
  theme: Theme
  onToggleTheme: () => void
}

export function Header({ email, theme, onToggleTheme }: Props) {
  const { t } = useTranslation()
  return (
    <header className="sticky top-0 z-10 bg-background">
      <div className="mx-auto flex h-16 max-w-[1040px] items-center justify-between px-6">
        <Link
          to="/"
          aria-label={t('header.home')}
          className="rounded-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          <img
            src={theme === 'dark' ? logoDark : logo}
            alt={`${brand.brandName} ${brand.brandSub}`}
            className="h-10 w-auto"
          />
        </Link>

        <div className="flex items-center gap-3">
          {email && <UserMenu email={email} />}
          <LanguageSwitcher />
          <ThemeToggle theme={theme} onToggle={onToggleTheme} />
        </div>
      </div>
    </header>
  )
}
