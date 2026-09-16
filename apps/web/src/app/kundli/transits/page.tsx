'use client'

import Link from 'next/link'
import { useEffect, useState } from 'react'

import { TransitPanel } from '@/components/chart/TransitPanel'
import { LoadError } from '@/components/LoadError'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { astrologyApi, type NatalTransits } from '@/lib/astrology-api'
import { AuthError } from '@/lib/auth-api'
import { useRequireAuth } from '@/lib/use-require-auth'

type State = 'loading' | 'ready' | 'error' | 'no-profile' | 'not-yet'

/**
 * Where the planets are now, read against this person's natal Moon.
 *
 * This screen works without a birth time, which is worth saying because
 * almost nothing else in the product does: the Moon's sign needs a date,
 * not a clock. A user who never knew their birth time gets no ascendant,
 * no houses and no dashas — and still gets this, and Sade Sati with it,
 * which is the question most of them came to ask.
 *
 * ── 503 is not an error ──
 *
 * Transits are written by a worker that runs every six hours. Before its
 * first run the table is empty, and api-service answers 503 with a
 * `Retry-After` and the message "Transits are being prepared" — a
 * deliberate distinction from both 404 and an empty list, because "no
 * planets are transiting" is never true.
 *
 * This page collapsed that into "Something went wrong. Please try
 * again." until an end-to-end run against a freshly started stack hit
 * it. Nothing HAD gone wrong; the reader was being told their account
 * was broken because a cron had not fired yet.
 */
export default function TransitsPage() {
  const onUnauthenticated = useRequireAuth()

  const [data, setData] = useState<NatalTransits | null>(null)
  const [state, setState] = useState<State>('loading')
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false

    astrologyApi
      .listProfiles()
      .then(({ birth_profiles: profiles }) => {
        if (cancelled) return null

        const first = profiles[0]
        if (!first) {
          setState('no-profile')
          return null
        }
        return astrologyApi.transits(first.id)
      })
      .then((result) => {
        if (cancelled || !result) return
        setData(result)
        setState('ready')
      })
      .catch((err: unknown) => {
        if (cancelled) return
        if (onUnauthenticated(err)) return

        if (err instanceof AuthError && err.status === 503) {
          setState('not-yet')
          return
        }
        setState('error')
      })

    return () => {
      cancelled = true
    }
  }, [onUnauthenticated, attempt])

  return (
    <main className="mx-auto max-w-2xl px-4 py-8 sm:px-6">
      {state === 'loading' && (
        <div className="space-y-3" aria-busy="true" aria-live="polite">
          <span className="sr-only">Loading transits…</span>
          {Array.from({ length: 7 }, (_, i) => (
            <Skeleton key={i} className="h-10 w-full" />
          ))}
        </div>
      )}

      {state === 'error' && <LoadError onRetry={() => setAttempt((n) => n + 1)} />}

      {state === 'no-profile' && (
        <div className="rounded-lg border border-border p-8 text-center">
          <p className="text-sm text-ink-muted">
            Transits are read against your birth chart. Add your birth details and this
            fills in — a birth date is enough, a time is not needed here.
          </p>
          <Button asChild className="mt-4">
            <Link href="/onboarding/birth">Add birth details</Link>
          </Button>
        </div>
      )}

      {state === 'not-yet' && (
        <div className="rounded-lg border border-border p-8 text-center">
          <p className="text-sm text-ink-muted">
            Today&rsquo;s sky is still being prepared. Positions are computed every six
            hours; this usually resolves within a few minutes of a fresh start.
          </p>
          <Button
            variant="secondary"
            className="mt-4"
            onClick={() => setAttempt((n) => n + 1)}
          >
            Try again
          </Button>
        </div>
      )}

      {state === 'ready' && data && <TransitPanel data={data} />}
    </main>
  )
}
