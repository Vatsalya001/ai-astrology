'use client'

import { useEffect } from 'react'
import Link from 'next/link'
import { Wordmark } from '@/components/Logo'
import { Button } from '@/components/ui/button'

/**
 * Error boundary for the whole app.
 *
 * Must be a Client Component — React needs `reset` to run in the browser.
 *
 * Two rules govern what this renders. It never shows `error.message`: a
 * server-side render error can carry a connection string or an internal
 * hostname, and this page is public. And it never shows a dead end — an
 * error the user cannot retry or navigate away from is worse than the
 * error itself.
 *
 * `error.digest` is safe and is the one useful thing here: Next logs the
 * full stack server-side under the same digest, so a user reading the
 * code aloud is enough to find the trace.
 */
export default function Error({
  error,
  reset,
}: {
  error: Error & { digest?: string }
  reset: () => void
}) {
  useEffect(() => {
    // Sentry is wired in the API but not the browser until Phase 6; the
    // console is what exists now, and it stays client-side.
    console.error('unhandled render error', error)
  }, [error])

  return (
    <main
      id="main"
      tabIndex={-1}
      className="flex min-h-dvh flex-col items-center justify-center px-6 text-center"
    >
      <Wordmark />

      <p className="mt-10 font-mono text-sm uppercase tracking-[0.18em] text-danger">
        Something went wrong
      </p>

      <h1 className="mt-3 font-serif text-4xl tracking-tight sm:text-5xl">
        We couldn&apos;t render this page
      </h1>

      <p className="mt-4 max-w-md text-balance text-sm leading-relaxed text-ink-muted">
        The failure has been logged. Trying again often works — the cause is
        usually a service that was briefly unavailable.
      </p>

      <div className="mt-9 flex flex-col items-center gap-3 sm:flex-row">
        <Button onClick={reset}>Try again</Button>
        <Button variant="secondary" asChild>
          <Link href="/">Back to home</Link>
        </Button>
      </div>

      {error.digest && (
        <p className="mt-8 font-mono text-xs text-ink-faint">
          Reference: {error.digest}
        </p>
      )}
    </main>
  )
}
