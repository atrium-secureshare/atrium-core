import type { ReactNode } from 'react'

// Shared card for page-level messages. Errors stay deliberately terse and never
// explain the cause: a failed sign-in is a server-side matter whose reason lives
// in the logs.
export function MessageScreen({
  icon,
  title,
  description,
  action,
  status,
}: {
  icon?: ReactNode
  title: string
  description?: string
  action?: ReactNode
  /** Announces the card to screen readers. */
  status?: boolean
}) {
  return (
    <div
      role={status ? 'status' : undefined}
      className="rounded-[14px] border border-border bg-card px-6 py-16 text-center"
    >
      {icon && (
        <div
          className="mb-4 flex justify-center text-primary"
          aria-hidden="true"
        >
          {icon}
        </div>
      )}
      <p className="text-[15px] font-semibold text-foreground">{title}</p>
      {description && (
        <p className="mx-auto mt-2 max-w-[42ch] text-[13.5px] text-[var(--text-2)]">
          {description}
        </p>
      )}
      {action && (
        <div className="mt-4 flex justify-center text-[13.5px] font-semibold text-primary">
          {action}
        </div>
      )}
    </div>
  )
}
