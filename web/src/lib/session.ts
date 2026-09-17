// Tracks when the gateway will end the session, so the UI can warn ahead of an
// idle logout without polling for it. Every authenticated response advertises the
// seconds left; the deadline is kept in localStorage so all tabs share one view.
import { sessionIdleTtl } from '@/config'

const STORAGE_KEY = 'atrium-session-expires'

export const EXPIRES_HEADER = 'Atrium-Session-Expires-In'

/** Epoch milliseconds at which the session ends, or null if nothing is known yet. */
export function sessionDeadline(): number | null {
  const stored = Number(localStorage.getItem(STORAGE_KEY))
  return stored > 0 ? stored : null
}

/**
 * Drops a deadline that has already passed. Called at startup, where whether the
 * session is still alive is the gateway's answer to give: a deadline left over
 * from an earlier one fires the expiry check on mount and sends even a freshly
 * logged-in visitor straight back to the logged-out page.
 */
export function forgetExpiredDeadline(): void {
  const deadline = sessionDeadline()
  if (deadline !== null && deadline <= Date.now())
    localStorage.removeItem(STORAGE_KEY)
}

// A deadline only ever moves forward, so the newest reading wins and a stale one
// — from another tab, or from a request whose response this app never sees —
// can never pull it back.
function extendTo(at: number): void {
  const current = sessionDeadline()
  if (current !== null && current >= at) return
  localStorage.setItem(STORAGE_KEY, String(at))
}

/** Records the deadline a response advertised. */
export function noteExpiresIn(header: string | null): void {
  const seconds = Number(header)
  if (header === null || !Number.isFinite(seconds)) return
  extendTo(Date.now() + seconds * 1000)
}

// Records a request the app cannot read the response of: the hidden-anchor
// download still counts as activity at the gateway, so without this the UI would
// announce a logout that did not happen.
export function noteUnreadRequest(): void {
  if (sessionIdleTtl > 0) extendTo(Date.now() + sessionIdleTtl * 1000)
}
