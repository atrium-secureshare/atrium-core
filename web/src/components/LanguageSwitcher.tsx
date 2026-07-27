import { useEffect, useRef, useState } from 'react'
import { Check, Languages } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { SUPPORTED_LANGUAGES } from '@/i18n'
import { cn } from '@/lib/utils'

// Hand-rolled dropdown (mirrors UserMenu) rather than a UI-primitive dependency,
// per the minimalism ladder. changeLanguage persists to localStorage.
export function LanguageSwitcher() {
  const { t, i18n } = useTranslation()
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

  const active = i18n.resolvedLanguage

  function choose(lng: string) {
    void i18n.changeLanguage(lng)
    setOpen(false)
  }

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t('language.label')}
        className="flex size-[38px] items-center justify-center rounded-full border border-border bg-secondary text-foreground transition-colors hover:bg-[var(--surface-3)]"
      >
        <Languages className="size-[18px]" />
      </button>

      {open && (
        <div
          role="menu"
          className="absolute right-0 top-[calc(100%+8px)] min-w-[160px] overflow-hidden rounded-xl border border-border bg-background py-1 shadow-lg"
        >
          {SUPPORTED_LANGUAGES.map((lng) => (
            <button
              key={lng}
              type="button"
              role="menuitemradio"
              aria-checked={lng === active}
              onClick={() => choose(lng)}
              className={cn(
                'flex w-full items-center justify-between gap-2 px-4 py-2 text-left text-[13px] transition-colors hover:bg-secondary',
                lng === active
                  ? 'font-semibold text-foreground'
                  : 'text-[var(--text-2)]',
              )}
            >
              {t(`language.${lng}`)}
              {lng === active && (
                <Check className="size-4 text-primary" aria-hidden="true" />
              )}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
