'use client'

import Link from 'next/link'
import { useEffect, useRef, useState } from 'react'

import type { ChartStyle } from '@ayana/astrology-geometry'

import { ChartSVG } from '@/components/chart/ChartSVG'
import { DownloadPdf } from '@/components/chart/DownloadPdf'
import { ShareSheet } from '@/components/chart/ShareSheet'
import { StyleSwitcher } from '@/components/chart/StyleSwitcher'
import { VargaSwitcher } from '@/components/chart/VargaSwitcher'
import { parseChartData } from '@/components/chart/parse'
import { toChartStyle } from '@/components/chart/style'
import type { ChartData } from '@/components/chart/types'
import { LoadError } from '@/components/LoadError'
import { ProfileSwitcher } from '@/components/ProfileSwitcher'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { track, trackAsUser } from '@/lib/analytics'
import { astrologyApi, type BirthProfile } from '@/lib/astrology-api'
import { useLocale } from '@/lib/i18n/context'
import { resolveProfile, useSelectedProfile } from '@/lib/profile-context'
import { useRequireAuth } from '@/lib/use-require-auth'
import { usersApi } from '@/lib/users-api'
import { DEFAULT_VARGA, type VargaType } from '@/lib/varga'

type State = 'loading' | 'ready' | 'error' | 'no-profile' | 'unreadable'

/**
 * The chart itself.
 *
 * `ChartSVG` was built in Phase 3 PR 2 and, until this file, was mounted
 * on no route at all — thirty passing unit tests and nothing a user
 * could reach. That is two gate items ("renders a complete, correct
 * chart for all 30 fixtures" and "North and South both correct; switcher
 * persists to preferences") resting on a component nobody could open.
 *
 * ── The style is a stored preference, not a view toggle ──
 *
 * The spec is explicit that the two layouts are not cosmetic: showing a
 * South Indian reader a diamond makes the product feel foreign. So the
 * switcher writes through to `user_preferences.chart_style`, and the
 * write is optimistic — the diagram redraws immediately and the request
 * settles behind it. A reader tapping a layout toggle should not wait on
 * a round trip, and the cost of the write failing is that the choice
 * does not follow them to another device, which is recoverable.
 */
export default function ChartPage() {
  const onUnauthenticated = useRequireAuth()
  const { t } = useLocale()
  const { selectedId } = useSelectedProfile()

  const [profiles, setProfiles] = useState<BirthProfile[]>([])

  /*
    A handle on the rendered SVG, for the image export.

    The PNG is produced by reading the LIVE element's computed styles —
    every colour in ChartSVG comes from a Tailwind class, and a clone
    detached from the document has none of them. So the export needs the
    element that is actually on screen, not a re-render of it.
  */
  const chartRef = useRef<SVGSVGElement>(null)
  const [chart, setChart] = useState<ChartData | null>(null)
  const [varga, setVarga] = useState<VargaType>(DEFAULT_VARGA)
  const [style, setStyle] = useState<ChartStyle>('north')
  const [state, setState] = useState<State>('loading')
  const [attempt, setAttempt] = useState(0)

  /*
    The stored style, fetched once and not on every varga change.

    `allSettled`, because the preference is not worth failing the screen
    over: a reader whose preferences call 502s should still see their
    chart, in the default layout.
  */
  useEffect(() => {
    let cancelled = false

    usersApi
      .preferences()
      .then((prefs) => {
        if (!cancelled) setStyle(toChartStyle(prefs.chart_style))
      })
      .catch(() => {
        // Deliberately swallowed. `toChartStyle` already supplies the
        // default, and an error state here would blank a chart over a
        // layout preference.
      })

    return () => {
      cancelled = true
    }
  }, [])

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
        return astrologyApi.chart(profile.id, varga)
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

        /*
          Emitted here rather than on mount: "viewed" should mean a chart
          was actually rendered, not that a route was opened and then
          failed. `chart_type` distinguishes D1 from D9 and D10, which is
          the whole question this event exists to answer.

          `trackAsUser`, and this was wrong first: it read
          `{ user_id: result.birth_profile_id }` — a BIRTH PROFILE id in
          the user field. That type-checks, and it is the exact mistake
          `trackAsUser` was written to prevent; its own docstring names
          it. Every "viewed" row would have been attributed to a record
          that is not a user, and a row attributed to the wrong id looks
          like a fact.
        */
        void trackAsUser('kundli_viewed', { chart_type: varga })
      })
      .catch((err: unknown) => {
        if (cancelled) return
        if (!onUnauthenticated(err)) setState('error')
      })

    return () => {
      cancelled = true
    }
  }, [varga, attempt, onUnauthenticated, selectedId])

  function chooseVarga(next: VargaType) {
    setState('loading')
    setVarga(next)
  }

  function chooseStyle(next: ChartStyle) {
    if (next === style) return

    track('chart_style_switched', { from: style, to: next })
    setStyle(next)

    // Optimistic: the diagram is already redrawing. A failed write costs
    // the reader nothing on this device and is not worth an error state.
    void usersApi.updatePreferences({ chart_style: next }).catch(() => {})
  }

  function retry() {
    setState('loading')
    setAttempt((n) => n + 1)
  }

  // Derived rather than stored: one source for "which profile is this",
  // shared with the fetch above, so the download cannot address a
  // different chart from the one rendered.
  const shownProfile = resolveProfile(profiles, selectedId)

  return (
    <main id="main" tabIndex={-1} className="mx-auto max-w-2xl px-4 py-8 sm:px-6">
      <header className="mb-6 flex flex-wrap items-baseline justify-between gap-3">
        <h1 className="font-serif text-2xl">{t.chart.chartTitle}</h1>
        <ProfileSwitcher profiles={profiles} />
      </header>

      <div className="mb-6 flex flex-wrap gap-6">
        {/* Not disabled while loading — see the note on /kundli/planets. */}
        <VargaSwitcher value={varga} onChange={chooseVarga} />
        <StyleSwitcher value={style} onChange={chooseStyle} />
      </div>

      {state === 'loading' && (
        <div aria-busy="true" aria-live="polite">
          <span className="sr-only">{t.chart.loading}</span>
          {/* Square, like the diagram — so the page does not jump. */}
          <Skeleton className="aspect-square w-full" />
        </div>
      )}

      {state === 'error' && <LoadError onRetry={retry} />}

      {state === 'no-profile' && (
        <div className="rounded-lg border border-border p-8 text-center">
          <p className="text-sm text-ink-muted">{t.chart.planetsNoProfile}</p>
          <Button asChild className="mt-4">
            <Link href="/onboarding/birth">{t.chart.addBirthDetails}</Link>
          </Button>
        </div>
      )}

      {state === 'unreadable' && (
        <div role="alert" className="rounded-lg border border-border p-8 text-center">
          <p className="text-sm text-ink-muted">{t.chart.unreadable}</p>
          <Button variant="secondary" className="mt-4" onClick={retry}>
            {t.chart.tryAgain}
          </Button>
        </div>
      )}

      {state === 'ready' && chart && (
        <>
          <ChartSVG
            svgRef={chartRef}
            chart={chart}
            style={style}
            captionText={t.chart.srTableCaption}
            className="w-full"
          />

          <p className="mt-6 text-center">
            <Link
              href="/kundli/planets"
              className="text-sm text-primary underline-offset-4 hover:underline"
            >
              {t.chart.seePositions}
            </Link>
          </p>

          {/*
            The download, mounted here rather than left for a later PR.

            An endpoint nobody calls is the same defect as a component
            nobody can open — which this screen already exists to fix.
            The profile is DERIVED here, by the same `resolveProfile`
            the fetch used — not stored in a second piece of state that
            could disagree with the chart on screen. The button must ask
            for the chart the reader is looking at, not for whichever
            profile happens to be default.
          */}
          {shownProfile && (
            <div className="mt-8 flex flex-col items-center gap-4">
              <ShareSheet profileId={shownProfile.id} chartRef={chartRef} />
              <DownloadPdf profileId={shownProfile.id} />
            </div>
          )}
        </>
      )}
    </main>
  )
}
