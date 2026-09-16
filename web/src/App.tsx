import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ReactNode,
  type Ref,
} from 'react'
import { Link, Route, Routes, useLocation } from 'react-router-dom'
import { CheckCircle2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getMe } from '@/lib/api'
import { useTheme, type Theme } from '@/hooks/useTheme'
import { useShares } from '@/hooks/useShares'
import { useToast } from '@/hooks/useToast'
import { Header } from '@/components/Header'
import { SharesView } from '@/components/SharesView'
import { ShareView } from '@/components/ShareView'
import { MessageScreen } from '@/components/MessageScreen'
import { TosOverlay } from '@/components/TosOverlay'
import { Toast } from '@/components/Toast'

// The download is fire-and-forget (no completion signal), so give the gateway a
// moment to bump the count before refetching the shares.
const DOWNLOAD_REFRESH_MS = 1500

// `/auth/error` and `/auth/logged-out` use a standalone layout that does NOT
// load authenticated endpoints: the visitor has no session, so /api/me would 401
// and bounce them back to login. Everything else runs inside the authenticated
// Shell.
function App() {
  const { theme, toggle } = useTheme()
  const { pathname } = useLocation()
  const { t } = useTranslation()

  if (pathname === '/auth/error') {
    return (
      <StandaloneLayout
        theme={theme}
        onToggleTheme={toggle}
        title={t('app.loginFailedTitle')}
        action={
          <a href="/auth/login" className="hover:underline">
            {t('app.loginRetry')}
          </a>
        }
      />
    )
  }

  if (pathname === '/auth/logged-out') {
    return <LoggedOut theme={theme} onToggleTheme={toggle} />
  }

  return <Shell theme={theme} onToggleTheme={toggle} />
}

// Post-logout landing page, the provider's post_logout_redirect_uri target. It
// never redirects on its own: an automatic bounce is what leaves the recipient
// unsure whether they are still signed in.
function LoggedOut({
  theme,
  onToggleTheme,
}: {
  theme: Theme
  onToggleTheme: () => void
}) {
  const { t } = useTranslation()
  const headingRef = useRef<HTMLDivElement>(null)

  // Land keyboard and screen-reader users on the confirmation itself.
  useEffect(() => {
    headingRef.current?.focus()
  }, [])

  return (
    <StandaloneLayout
      theme={theme}
      onToggleTheme={onToggleTheme}
      ref={headingRef}
      icon={<CheckCircle2 className="size-8" />}
      title={t('app.loggedOutTitle')}
      description={t('app.loggedOutHint')}
      status
      action={
        <a
          href="/auth/login"
          className="rounded-[10px] bg-primary px-5 py-2.5 text-primary-foreground transition-colors hover:bg-[var(--primary-hover)] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          {t('app.loginAgain')}
        </a>
      }
    />
  )
}

// Plain brand header with no authenticated data loading, for pages where a
// session cannot be assumed. tabIndex={-1} lets a caller focus the card via ref
// without adding a tab stop.
function StandaloneLayout({
  theme,
  onToggleTheme,
  ref,
  icon,
  title,
  description,
  action,
  status,
}: {
  theme: Theme
  onToggleTheme: () => void
  ref?: Ref<HTMLDivElement>
  icon?: ReactNode
  title: string
  description?: string
  action: ReactNode
  status?: boolean
}) {
  return (
    <div className="min-h-svh">
      <Header email="" theme={theme} onToggleTheme={onToggleTheme} />
      <main className="mx-auto max-w-[1040px] px-6 pb-[72px] pt-7">
        <div ref={ref} tabIndex={-1} className="focus:outline-none">
          <MessageScreen
            icon={icon}
            title={title}
            description={description}
            action={action}
            status={status}
          />
        </div>
      </main>
    </div>
  )
}

function Shell({
  theme,
  onToggleTheme,
}: {
  theme: Theme
  onToggleTheme: () => void
}) {
  const { shares, loading, error, tosRequired, reload } = useShares()
  const { message, show } = useToast()
  const { t } = useTranslation()
  const [email, setEmail] = useState('')

  const loadMe = useCallback(() => {
    getMe()
      .then((me) => setEmail(me.email))
      .catch(() => {
        /* 401 redirects to login; a tos_required 403 clears once accepted. */
      })
  }, [])

  useEffect(loadMe, [loadMe])

  // Refetch shortly after a download so the stale counter updates in place.
  const onDownloaded = useCallback(() => {
    window.setTimeout(reload, DOWNLOAD_REFRESH_MS)
  }, [reload])

  // Consent unblocks both the shares listing and /api/me, so reload both.
  const onTosAccepted = useCallback(() => {
    reload()
    loadMe()
  }, [reload, loadMe])

  return (
    <div className="min-h-svh">
      <Header email={email} theme={theme} onToggleTheme={onToggleTheme} />

      <main className="mx-auto max-w-[1040px] px-6 pb-[72px] pt-7">
        <Routes>
          <Route
            path="/"
            element={
              <SharesView
                shares={shares}
                loading={loading || tosRequired}
                error={error}
                onToast={show}
                onDownloaded={onDownloaded}
              />
            }
          />
          <Route
            path="/share/:hash/*"
            element={
              <ShareView
                shares={shares}
                sharesLoading={loading || tosRequired}
                onToast={show}
                onDownloaded={onDownloaded}
              />
            }
          />
          <Route
            path="*"
            element={
              <MessageScreen
                title={t('app.notFoundTitle')}
                action={
                  <Link to="/" className="hover:underline">
                    {t('app.home')}
                  </Link>
                }
              />
            }
          />
        </Routes>
      </main>

      {tosRequired && <TosOverlay onAccepted={onTosAccepted} />}

      <Toast message={message} />
    </div>
  )
}

export default App
