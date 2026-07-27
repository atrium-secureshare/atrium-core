interface Props {
  message: string | null
}

// A polite ARIA live region so screen readers hear the message without stealing focus.
export function Toast({ message }: Props) {
  return (
    <div
      role="status"
      aria-live="polite"
      className="pointer-events-none fixed inset-x-0 bottom-6 z-30 flex justify-center px-4"
    >
      {message && (
        <div className="rounded-full bg-foreground px-4 py-2 text-[13px] font-medium text-background shadow-[var(--shadow-md,0_6px_20px_-8px_rgba(15,23,42,.18))]">
          {message}
        </div>
      )}
    </div>
  )
}
