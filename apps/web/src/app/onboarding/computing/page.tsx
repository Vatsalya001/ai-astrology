'use client'

import { Suspense, useEffect, useState } from 'react'
import { useRouter, useSearchParams } from 'next/navigation'

import { Wordmark } from '@/components/Logo'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { astrologyApi } from '@/lib/astrology-api'
import { getDictionary } from '@/lib/i18n/dictionaries'
import { track, trackAsUser } from '@/lib/analytics'

/**
 * The wait while a chart is computed.
 *
 * The specification asks for a two-to-four-second celestial animation
 * that "streams real progress; does not fake it". What that means in
 * practice: this screen shows a skeleton of the card that is coming and
 * moves on the moment the request resolves. It does NOT run a timed
 * progress bar to four seconds — a fake bar that finishes before the
 * data arrives has to stall at 99%, which is worse than no bar, and one
 * that finishes after wastes the time of everyone on a fast connection.
 *
 * The honest signal is: request in flight, request done, request failed.
 */
export default function ComputingPage() {
  return (
    <Suspense fallback={<ComputingFrame />}>
      <Computing />
    </Suspense>
  )
}

function Computing() {
  const router = useRouter()
  const params = useSearchParams()
  const t = getDictionary('en')

  const profileId = params.get('profile')
  const [failed, setFailed] = useState(false)
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    if (!profileId) {
      router.replace('/onboarding/birth')
      return
    }

    let cancelled = false
    const started = Date.now()

    astrologyApi
      .chart(profileId)
      .then(() => {
        if (cancelled) return
        void trackAsUser('chart_generated', {
          chart_type: 'D1',
          duration_ms: Date.now() - started,
        })
        router.replace('/home')
      })
      .catch(() => {
        if (cancelled) return
        // The technical error goes to the reporter, not to the user. A
        // chart failure names the profile and the engine; neither is
        // something a person can act on, and the profile id is derived
        // from birth data.
        track('chart_generation_failed', { error_code: 'chart_unavailable' })
        setFailed(true)
      })

    return () => {
      cancelled = true
    }
  }, [profileId, router, attempt])

  if (failed) {
    return (
      <ComputingFrame>
        <h1 className="text-balance text-center font-serif text-2xl tracking-tight">
          {t.birth.computingFailed}
        </h1>
        <p className="mt-3 text-balance text-center text-sm text-ink-muted">
          {t.common.somethingWentWrong}
        </p>
        <Button
          size="lg"
          className="mt-8 w-full"
          onClick={() => {
            setFailed(false)
            setAttempt((n) => n + 1)
          }}
        >
          {t.birth.computingRetry}
        </Button>
      </ComputingFrame>
    )
  }

  return (
    <ComputingFrame>
      <h1 className="text-balance text-center font-serif text-2xl tracking-tight">
        {t.birth.computingTitle}
      </h1>
      <p className="mt-3 text-balance text-center text-sm text-ink-muted">
        {t.birth.computingSubtitle}
      </p>

      {/* A skeleton shaped like the chart card that is coming, not a
          spinner on a blank page. Someone who has seen this screen once
          recognises the layout before the data lands. */}
      <div aria-hidden="true" className="mt-10 space-y-3">
        <Skeleton className="h-40 w-full rounded-xl" />
        <div className="grid grid-cols-3 gap-3">
          <Skeleton className="h-16 rounded-lg" />
          <Skeleton className="h-16 rounded-lg" />
          <Skeleton className="h-16 rounded-lg" />
        </div>
      </div>

      <p role="status" aria-live="polite" className="sr-only">
        {t.birth.computingTitle}
      </p>
    </ComputingFrame>
  )
}

function ComputingFrame({ children }: { children?: React.ReactNode }) {
  return (
    <main id="main" className="flex min-h-dvh flex-col items-center px-6 py-16">
      <Wordmark />
      <div className="mt-12 w-full max-w-sm">{children}</div>
    </main>
  )
}
