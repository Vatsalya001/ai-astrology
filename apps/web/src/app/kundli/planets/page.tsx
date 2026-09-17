'use client'

import Link from 'next/link'
import { useEffect, useState } from 'react'

import { HouseList } from '@/components/chart/HouseList'
import { PlanetTable } from '@/components/chart/PlanetTable'
import { VargaSwitcher } from '@/components/chart/VargaSwitcher'
import { parseChartData } from '@/components/chart/parse'
import type { ChartData } from '@/components/chart/types'
import { LoadError } from '@/components/LoadError'
import { ProfileSwitcher } from '@/components/ProfileSwitcher'
import { SectionLabel } from '@/components/ui'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { astrologyApi, type BirthProfile } from '@/lib/astrology-api'
import { resolveProfile, useSelectedProfile } from '@/lib/profile-context'
import { useLocale } from '@/lib/i18n/context'
import { useRequireAuth } from '@/lib/use-require-auth'
import { DEFAULT_VARGA, type VargaType } from '@/lib/varga'

type State = 'loading' | 'ready' | 'error' | 'no-profile' | 'unreadable'

/**
 * Planets and houses, for the selected divisional chart.
 *
 * The four states the Definition of Done asks for are all reachable
 * here and all distinct, which matters more than usual on this screen:
 *
 *   loading      a skeleton shaped like the table, not a spinner
 *   error        the API failed — recoverable, so it offers a retry
 *   no-profile   nothing to compute from — offers the thing to do next
 *   unreadable   the chart came back but could not be parsed
 *
 * The last two look identical if you collapse them into "empty", and
 * they are opposites: one is the user's next step, the other is ours.
 */
export default function PlanetsPage() {
  const onUnauthenticated = useRequireAuth()
  const { t } = useLocale()
  const { selectedId } = useSelectedProfile()
  const [profiles, setProfiles] = useState<BirthProfile[]>([])

  const [varga, setVarga] = useState<VargaType>(DEFAULT_VARGA)
  const [chart, setChart] = useState<ChartData | null>(null)
  /** Which profile the chart in state was fetched for. */
  const [chartFor, setChartFor] = useState<string | null>(null)
  const [state, setState] = useState<State>('loading')
  const [attempt, setAttempt] = useState(0)

  /**
   * A promise chain with a cancellation flag, matching every other
   * loading screen in this app.
   *
   * Not an `async` function called from the effect: `load()` would set
   * no state synchronously, but `react-hooks/set-state-in-effect` cannot
   * prove that and flags the call. Rather than suppress the rule, this
   * uses the shape the rest of the app already uses — and the shape has
   * its own reason to exist.
   *
   * ── The flag is about ordering, not about waste ──
   *
   * Switching charts quickly — D1, D9, D10 — starts three fetches, and
   * they can land in any order. Without the flag the slowest response
   * wins and the table shows a chart the switcher does not say is
   * selected. Phase 2 shipped exactly that bug on the birth-details
   * flow; a cancellation flag is what fixed it there too.
   */
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
        return astrologyApi.chart(first.id, varga).then((chart) => ({ chart, profileId: first.id }))
      })
      .then((result) => {
        if (cancelled || !result) return
        const { chart: fetched, profileId: first } = result

        const parsed = parseChartData(fetched.chart_data)
        if (!parsed) {
          setState('unreadable')
          return
        }
        setChart(parsed)
        setChartFor(first)
        setState('ready')
      })
      .catch((err: unknown) => {
        if (cancelled) return
        // Only a real "signed out" redirects. A dropped connection says
        // nothing about the session, and sending the user to /auth for
        // one tells them they have been signed out when they have not.
        if (!onUnauthenticated(err)) setState('error')
      })

    return () => {
      cancelled = true
    }
  }, [varga, attempt, onUnauthenticated, selectedId])

  /*
    A profile switch has to blank the table, not leave the previous
    person's planets under the new person's header.

    DERIVED during render rather than set from an effect. `selectedId`
    changes in the ProfileSwitcher — a sibling, through context — so
    there is no handler on this page to set 'loading' from, and the
    obvious `useEffect(() => setState('loading'), [selectedId])` is the
    cascading render `react-hooks/set-state-in-effect` forbids.
    Comparing what the chart is FOR against what is selected needs no
    effect and is strictly more correct: it also covers the window while
    the new request is still in flight.
  */
  const resolvedId = resolveProfile(profiles, selectedId)?.id ?? null
  const showingAnotherProfile = chart !== null && chartFor !== null && chartFor !== resolvedId

  function chooseVarga(next: VargaType) {
    setState('loading')
    setVarga(next)
  }

  function retry() {
    setState('loading')
    setAttempt((n) => n + 1)
  }

  return (
    <main id="main" className="mx-auto max-w-4xl px-4 py-8 sm:px-6">
      <header className="mb-6 flex flex-wrap items-baseline justify-between gap-3">
        <h1 className="font-serif text-2xl">{t.chart.planetsTitle}</h1>
        <ProfileSwitcher profiles={profiles} />
      </header>

      {/*
        Not disabled while loading.

        Disabling the fieldset disables the radio the user is standing
        on, and the browser blurs a focused element that becomes
        disabled — so a keyboard user pressing ArrowRight lost focus to
        the document root mid-interaction and could never reach D10 by
        arrow key. The loading state is already carried by the skeleton
        below, and a second request is handled by the cancellation flag
        rather than by preventing one.
      */}
      <VargaSwitcher value={varga} onChange={chooseVarga} className="mb-8" />

      {(state === 'loading' || showingAnotherProfile) && (
        <LoadingTable label={t.chart.loading} />
      )}

      {state === 'error' && <LoadError onRetry={retry} />}

      {state === 'no-profile' && (
        <div className="rounded-lg border border-border p-8 text-center">
          <p className="text-sm text-ink-muted">
            {t.chart.planetsNoProfile}
          </p>
          <Button asChild className="mt-4">
            <Link href="/onboarding/birth">{t.chart.addBirthDetails}</Link>
          </Button>
        </div>
      )}

      {/*
        Deliberately not the same message as `no-profile`. This one is
        ours to fix, not the reader's, and telling them to add birth
        details they have already added is worse than saying nothing.
      */}
      {state === 'unreadable' && (
        <div role="alert" className="rounded-lg border border-border p-8 text-center">
          <p className="text-sm text-ink-muted">
            {t.chart.unreadable}
          </p>
          <Button
            variant="secondary"
            className="mt-4"
            onClick={retry}
          >
            {t.chart.tryAgain}
          </Button>
        </div>
      )}

      {state === 'ready' && chart && !showingAnotherProfile && (
        <div className="space-y-10">
          <section aria-labelledby="planets-heading">
            <SectionLabel>
              <span id="planets-heading">{t.chart.planetsSection}</span>
            </SectionLabel>
            <PlanetTable planets={chart.planets} className="mt-3" />
          </section>

          <section aria-labelledby="houses-heading">
            <SectionLabel>
              <span id="houses-heading">{t.chart.housesSection}</span>
            </SectionLabel>
            <HouseList houses={chart.houses} planets={chart.planets} className="mt-3" />
          </section>
        </div>
      )}
    </main>
  )
}

/**
 * A skeleton shaped like the table it replaces.
 *
 * The frontend rules ask for this specifically: a skeleton that looks
 * like the eventual content beats a spinner on a blank page, because the
 * layout does not jump when the data lands.
 */
function LoadingTable({ label }: { label: string }) {
  return (
    <div className="space-y-3" aria-busy="true" aria-live="polite">
      <span className="sr-only">{label}</span>
      {Array.from({ length: 9 }, (_, i) => (
        <Skeleton key={i} className="h-10 w-full" />
      ))}
    </div>
  )
}
