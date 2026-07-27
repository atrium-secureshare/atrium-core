import { useCallback, useEffect, useState } from 'react'
import i18n from '@/i18n'
import { listShares, TosRequiredError, type Share } from '@/lib/api'

export interface SharesState {
  shares: Share[]
  loading: boolean
  error: string | null
  /** tosRequired is true while the recipient must accept the ToS first. */
  tosRequired: boolean
  reload: () => void
}

// Loads the recipient's shares and exposes a reload. A 401 redirects to login in
// the API layer, so it never surfaces here; a 403 tos_required surfaces as tosRequired.
export function useShares(): SharesState {
  const [shares, setShares] = useState<Share[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [tosRequired, setTosRequired] = useState(false)

  const load = useCallback(() => {
    setLoading(true)
    setTosRequired(false)
    listShares()
      .then((data) => {
        setShares(data)
        setError(null)
      })
      .catch((err) => {
        if (err instanceof TosRequiredError) setTosRequired(true)
        else setError(i18n.t('shares.loadError'))
      })
      .finally(() => setLoading(false))
  }, [])

  useEffect(load, [load])

  return { shares, loading, error, tosRequired, reload: load }
}
