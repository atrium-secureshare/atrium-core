import { Moon, Sun } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { Theme } from '@/hooks/useTheme'

interface Props {
  theme: Theme
  onToggle: () => void
}

/** ThemeToggle is the 38px icon button that flips light/dark (sun in dark). */
export function ThemeToggle({ theme, onToggle }: Props) {
  const { t } = useTranslation()
  const toDark = theme === 'light'
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-label={toDark ? t('theme.toDark') : t('theme.toLight')}
      className="flex size-[38px] items-center justify-center rounded-full border border-border bg-secondary text-foreground transition-colors hover:bg-[var(--surface-3)]"
    >
      {toDark ? (
        <Moon className="size-[18px]" />
      ) : (
        <Sun className="size-[18px]" />
      )}
    </button>
  )
}
