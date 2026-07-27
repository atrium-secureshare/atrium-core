import type { ReactNode } from 'react'

// Shared card for page-level errors. Deliberately terse and never explains the
// cause: a failed sign-in is a server-side matter whose reason lives in the logs.
export function ErrorScreen({
  title,
  action,
}: {
  title: string
  action?: ReactNode
}) {
  return (
    <div className="rounded-[14px] border border-border bg-card px-6 py-16 text-center">
      <p className="text-[15px] font-semibold text-foreground">{title}</p>
      {action && (
        <div className="mt-4 text-[13.5px] font-semibold text-primary">
          {action}
        </div>
      )}
    </div>
  )
}
