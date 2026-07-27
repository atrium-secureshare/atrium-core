import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { Link, Route, Routes, useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { getMe } from '@/lib/api'
import { useTheme, type Theme } from '@/hooks/useTheme'
import { useShares } from '@/hooks/useShares'
import { useToast } from '@/hooks/useToast'
import { Header } from '@/components/Header'
import { SharesView } from '@/components/SharesView'
import { ShareView } from '@/components/ShareView'
import { ErrorScreen } from '@/components/ErrorScreen'
import { TosOverlay } from '@/components/TosOverlay'
import { Toast } from '@/components/Toast'

// The download is fire-and-forget (no completion signal), so give the gateway a
// moment to bump the count before refetching the shares.
const DOWNLOAD_REFRESH_MS = 1500

// `/auth/error` uses a standalone layout that does NOT load authenticated
// endpoints: the visitor has no session, so /api/me would 401 and bounce them
// back to login. Everything else runs inside the authenticated Shell.
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

  return <Shell theme={theme} onToggleTheme={toggle} />
}

// Plain brand header with no authenticated data loading, for pages where a
// session cannot be assumed.
function StandaloneLayout({
  theme,
  onToggleTheme,
  title,
  action,
}: {
  theme: Theme
  onToggleTheme: () => void
  title: string
  action: ReactNode
}) {
  return (
    <div className="min-h-svh">
      <Header email="" theme={theme} onToggleTheme={onToggleTheme} />
      <main className="mx-auto max-w-[1040px] px-6 pb-[72px] pt-7">
        <ErrorScreen title={title} action={action} />
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
              <ErrorScreen
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
