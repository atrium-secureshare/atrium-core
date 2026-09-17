import { useEffect, useState } from 'react'
import { sessionIdleTtl } from '@/config'
import { sessionDeadline } from '@/lib/session'

/** How long before the deadline the warning appears. */
const WARN_BEFORE_SECONDS = 120

// Comparing against a stored timestamp on a tick survives laptop suspend, which
// a single long setTimeout does not.
const TICK_MS = 1000

// Outside the warning window the remaining time is never rendered, so report a
// constant: React then bails out of the state update and nothing re-renders.
function secondsLeft(): number {
  const deadline = sessionDeadline()
  if (deadline === null) return Infinity
  const left = Math.ceil((deadline - Date.now()) / 1000)
  return left > WARN_BEFORE_SECONDS ? Infinity : Math.max(left, 0)
}

/**
 * Seconds until the gateway ends the session, once close enough to warn about:
 * Infinity while there is plenty of time, 0 once the session is gone.
 */
export function useSessionExpiry(): number {
  const [left, setLeft] = useState(secondsLeft)

  useEffect(() => {
    if (sessionIdleTtl <= 0) return
    const update = () => setLeft(secondsLeft())
    const id = window.setInterval(update, TICK_MS)
    // A background tab throttles timers, so re-check the moment it is back.
    document.addEventListener('visibilitychange', update)
    return () => {
      window.clearInterval(id)
      document.removeEventListener('visibilitychange', update)
    }
  }, [])

  return left
}
