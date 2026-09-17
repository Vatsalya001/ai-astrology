'use client'

import Link from 'next/link'
import { useEffect, useState } from 'react'

import { TransitPanel } from '@/components/chart/TransitPanel'
import { LoadError } from '@/components/LoadError'
import { ProfileSwitcher } from '@/components/ProfileSwitcher'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { astrologyApi, type BirthProfile, type NatalTransits } from '@/lib/astrology-api'
import { AuthError } from '@/lib/auth-api'
import { resolveProfile, useSelectedProfile } from '@/lib/profile-context'
import { useLocale } from '@/lib/i18n/context'
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
  const { t } = useLocale()
  const { selectedId } = useSelectedProfile()
  const [profiles, setProfiles] = useState<BirthProfile[]>([])

  const [data, setData] = useState<NatalTransits | null>(null)
  const [state, setState] = useState<State>('loading')
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false

    astrologyApi
      .listProfiles()
      .then(({ birth_profiles: profiles }) => {
        if (cancelled) return null

        setProfiles(profiles)

        /*
          The SELECTED profile, not the first one.
          `resolveProfile` falls back to the first when the stored id
          names a profile that no longer exists — deleting one, or
          editing one, which creates a new version with a new id.
          Requesting the stale id gives a 404 that is indistinguishable
          from "not yours", so the screen would show an error forever.
        */
        const first = resolveProfile(profiles, selectedId)
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
  }, [onUnauthenticated, attempt, selectedId])

  return (
    <main id="main" className="mx-auto max-w-2xl px-4 py-8 sm:px-6">
      {/*
        An h1, which this screen shipped without — its only heading was
        the h2 inside TransitPanel, so the document's first and highest
        heading was a level down and a screen reader navigating by
        heading started midway.
      */}
      <header className="mb-6 flex flex-wrap items-baseline justify-between gap-3">
        <h1 className="font-serif text-2xl">{t.chart.transitsPageTitle}</h1>
        <ProfileSwitcher profiles={profiles} />
      </header>

      {state === 'loading' && (
        <div className="space-y-3" aria-busy="true" aria-live="polite">
          <span className="sr-only">{t.chart.transitsLoading}</span>
          {Array.from({ length: 7 }, (_, i) => (
            <Skeleton key={i} className="h-10 w-full" />
          ))}
        </div>
      )}

      {state === 'error' && <LoadError onRetry={() => setAttempt((n) => n + 1)} />}

      {state === 'no-profile' && (
        <div className="rounded-lg border border-border p-8 text-center">
          <p className="text-sm text-ink-muted">
            {t.chart.transitsNoProfile}
          </p>
          <Button asChild className="mt-4">
            <Link href="/onboarding/birth">{t.chart.addBirthDetails}</Link>
          </Button>
        </div>
      )}

      {state === 'not-yet' && (
        <div className="rounded-lg border border-border p-8 text-center">
          <p className="text-sm text-ink-muted">
            {t.chart.transitsNotYet}
          </p>
          <Button
            variant="secondary"
            className="mt-4"
            onClick={() => setAttempt((n) => n + 1)}
          >
            {t.chart.tryAgain}
          </Button>
        </div>
      )}

      {state === 'ready' && data && <TransitPanel data={data} />}
    </main>
  )
}
