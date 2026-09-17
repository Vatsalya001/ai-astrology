'use client'

import Link from 'next/link'
import { useEffect, useState } from 'react'

import { YogaCard } from '@/components/chart/YogaCard'
import { parseChartData } from '@/components/chart/parse'
import type { ChartData } from '@/components/chart/types'
import { LoadError } from '@/components/LoadError'
import { ProfileSwitcher } from '@/components/ProfileSwitcher'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { astrologyApi, type BirthProfile } from '@/lib/astrology-api'
import { resolveProfile, useSelectedProfile } from '@/lib/profile-context'
import { useLocale } from '@/lib/i18n/context'
import { useRequireAuth } from '@/lib/use-require-auth'

type State = 'loading' | 'ready' | 'error' | 'no-profile' | 'unreadable'

/**
 * The combinations the engine found in this chart.
 *
 * ── Zero yogas is a real answer, not an empty state ──
 *
 * Most charts have a few; some have none, and "none" is what the engine
 * genuinely computed rather than something missing. Rendering a blank
 * panel, or the same "add your birth details" prompt the no-profile case
 * uses, would tell a reader their chart failed to load when it loaded
 * perfectly. It says the engine looked.
 *
 * That also matters for what this screen must never do: pad. An empty
 * result is the most tempting place to add "but you have a strong Moon"
 * or some other sentence nobody computed. Nothing here composes text.
 */
export default function YogasPage() {
  const onUnauthenticated = useRequireAuth()
  const { t } = useLocale()
  const { selectedId } = useSelectedProfile()

  const [profiles, setProfiles] = useState<BirthProfile[]>([])
  const [chart, setChart] = useState<ChartData | null>(null)
  const [state, setState] = useState<State>('loading')
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false

    astrologyApi
      .listProfiles()
      .then(({ birth_profiles: list }) => {
        if (cancelled) return null
        setProfiles(list)

        const profile = resolveProfile(list, selectedId)
        if (!profile) {
          setState('no-profile')
          return null
        }
        // The rasi, always. Yogas are read from the birth chart; the
        // divisionals carry none, which is why `ChartResponse` puts them
        // beside the chart rather than inside each one.
        return astrologyApi.chart(profile.id, 'D1')
      })
      .then((result) => {
        if (cancelled || !result) return

        const parsed = parseChartData(result.chart_data)
        if (!parsed) {
          setState('unreadable')
          return
        }
        setChart(parsed)
        setState('ready')
      })
      .catch((err: unknown) => {
        if (cancelled) return
        if (!onUnauthenticated(err)) setState('error')
      })

    return () => {
      cancelled = true
    }
  }, [onUnauthenticated, attempt, selectedId])

  return (
    <main className="mx-auto max-w-2xl px-4 py-8 sm:px-6">
      <header className="mb-6 flex flex-wrap items-baseline justify-between gap-3">
        <h1 className="font-serif text-2xl">{t.chart.yogasTitle}</h1>
        <ProfileSwitcher profiles={profiles} />
      </header>

      {state === 'loading' && (
        <div className="space-y-3" aria-busy="true" aria-live="polite">
          <span className="sr-only">{t.chart.yogasLoading}</span>
          {Array.from({ length: 4 }, (_, i) => (
            <Skeleton key={i} className="h-24 w-full" />
          ))}
        </div>
      )}

      {state === 'error' && <LoadError onRetry={() => setAttempt((n) => n + 1)} />}

      {state === 'no-profile' && (
        <div className="rounded-lg border border-border p-8 text-center">
          <p className="text-sm text-ink-muted">
            {t.chart.yogasNoProfile}
          </p>
          <Button asChild className="mt-4">
            <Link href="/onboarding/birth">{t.chart.addBirthDetails}</Link>
          </Button>
        </div>
      )}

      {state === 'unreadable' && (
        <div role="alert" className="rounded-lg border border-border p-8 text-center">
          <p className="text-sm text-ink-muted">
            {t.chart.yogasUnreadable}
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

      {state === 'ready' && chart && chart.yogas.length === 0 && (
        <div className="rounded-lg border border-border p-8 text-center">
          <p className="text-sm text-ink-muted">
            {t.chart.yogasNone}
          </p>
        </div>
      )}

      {state === 'ready' && chart && chart.yogas.length > 0 && (
        <div className="space-y-3">
          {chart.yogas.map((yoga) => (
            <YogaCard key={yoga.name} yoga={yoga} />
          ))}
        </div>
      )}
    </main>
  )
}
