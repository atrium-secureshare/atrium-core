import { useCallback, useEffect, useRef, useState } from 'react'

// A single transient message with auto-dismiss (~2.4s); a new call replaces the
// previous and restarts the timer.
export function useToast(): {
  message: string | null
  show: (msg: string) => void
} {
  const [message, setMessage] = useState<string | null>(null)
  const timer = useRef<number | undefined>(undefined)

  const show = useCallback((msg: string) => {
    setMessage(msg)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setMessage(null), 2400)
  }, [])

  useEffect(() => () => window.clearTimeout(timer.current), [])

  return { message, show }
}
