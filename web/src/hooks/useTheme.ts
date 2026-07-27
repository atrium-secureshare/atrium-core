import { useCallback, useEffect, useState } from 'react'
import { brand } from '@/config'

export type Theme = 'light' | 'dark'

const STORAGE_KEY = 'atrium-theme'

function initialTheme(): Theme {
  const stored = localStorage.getItem(STORAGE_KEY)
  if (stored === 'light' || stored === 'dark') return stored
  return brand.defaultTheme
}

// Manages the light/dark theme: persisted per browser and applied by toggling the
// `dark` class on <html>, which flips every CSS-variable token in index.css.
export function useTheme(): { theme: Theme; toggle: () => void } {
  const [theme, setTheme] = useState<Theme>(initialTheme)

  useEffect(() => {
    const dark = theme === 'dark'
    document.documentElement.classList.toggle('dark', dark)
    // Keep color-scheme in sync with the class so native UI (scrollbars, form controls) matches.
    document.documentElement.style.colorScheme = dark ? 'dark' : 'light'
    localStorage.setItem(STORAGE_KEY, theme)
  }, [theme])

  const toggle = useCallback(() => {
    setTheme((t) => (t === 'dark' ? 'light' : 'dark'))
  }, [])

  return { theme, toggle }
}
