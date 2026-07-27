import { Fragment } from 'react'
import { ChevronRight } from 'lucide-react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'

// One breadcrumb step; the last one (no `to`) is the current location.
export interface Crumb {
  label: string
  to?: string
}

export function Breadcrumb({ items }: { items: Crumb[] }) {
  const { t } = useTranslation()
  return (
    <nav
      aria-label={t('breadcrumb.aria')}
      className="mb-4 flex flex-wrap items-center gap-1 text-[13px]"
    >
      {items.map((item, i) => {
        const last = i === items.length - 1
        return (
          <Fragment key={i}>
            {i > 0 && (
              <ChevronRight
                className="size-3.5 shrink-0 text-muted-foreground"
                aria-hidden="true"
              />
            )}
            {item.to && !last ? (
              <Link
                to={item.to}
                className="truncate rounded px-1 text-muted-foreground transition-colors hover:text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring"
              >
                {item.label}
              </Link>
            ) : (
              <span
                className="truncate px-1 font-semibold text-foreground"
                aria-current={last ? 'page' : undefined}
              >
                {item.label}
              </span>
            )}
          </Fragment>
        )
      })}
    </nav>
  )
}
