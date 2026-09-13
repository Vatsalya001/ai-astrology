import Link from 'next/link'
import type { Metadata } from 'next'
import { Wordmark } from '@/components/Logo'
import { Button } from '@/components/ui/button'

export const metadata: Metadata = {
  title: 'Page not found — Ayana',
  robots: { index: false, follow: false },
}

/**
 * 404.
 *
 * Without this file Next serves its own black-and-white default, which
 * has no wordmark, no way back, and none of the app's type or colour —
 * so the one page most likely to be someone's first impression is the
 * one page that looks nothing like the product.
 *
 * Deliberately offers a route out rather than only an apology. A dead
 * end with no navigation is how a mistyped URL becomes a lost visitor.
 */
export default function NotFound() {
  return (
    <main
      id="main"
      className="flex min-h-dvh flex-col items-center justify-center px-6 text-center"
    >
      <Wordmark />

      <p className="mt-10 font-mono text-sm uppercase tracking-[0.18em] text-gold">
        404
      </p>

      <h1 className="mt-3 font-serif text-4xl tracking-tight sm:text-5xl">
        This page isn&apos;t written yet
      </h1>

      <p className="mt-4 max-w-md text-balance text-sm leading-relaxed text-ink-muted">
        The link may be mistyped, or it may point at something that arrives in
        a later phase. Nothing is broken.
      </p>

      <div className="mt-9 flex flex-col items-center gap-3 sm:flex-row">
        <Button asChild>
          <Link href="/">Back to home</Link>
        </Button>
        <Button variant="secondary" asChild>
          <Link href="/status">System status</Link>
        </Button>
      </div>
    </main>
  )
}
