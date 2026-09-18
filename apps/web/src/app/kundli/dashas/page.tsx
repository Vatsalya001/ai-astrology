'use client'

import Link from 'next/link'
import { useEffect, useState } from 'react'

import { DashaTimeline, type TimelineLevel } from '@/components/chart/DashaTimeline'
import { LoadError } from '@/components/LoadError'
import { ProfileSwitcher } from '@/components/ProfileSwitcher'
import { Button } from '@/components/ui/button'
import { astrologyApi, type BirthProfile, type DashaPeriod } from '@/lib/astrology-api'
import { resolveProfile, useSelectedProfile } from '@/lib/profile-context'
import { useLocale } from '@/lib/i18n/context'
import { useRequireAuth } from '@/lib/use-require-auth'

type State = 'loading' | 'ready' | 'error' | 'no-profile' | 'no-dashas'

/**
 * The dasha screen.
 *
 * ── Three levels, fetched one at a time ──
 *
 * The stored tree is 819 rows. Level 1 is nine of them, and nobody sees
 * the other 810 until they drill in. Fetching everything to render the
 * top is 90x the payload for the first paint of the screen the spec
 * calls the most compelling non-AI surface in the product.
 *
 * ── "no dashas" is not "error" ──
 *
 * A chart computed without a birth time has no dasha tree at all: the
 * Moon's position cannot be pinned to a nakshatra pada without one, and
 * the whole Vimshottari sequence starts from that pada. api-service
 * reports it distinctly, and so does this — the reader can fix it, and
 * only if they are told what to fix.
 */
export default function DashasPage() {
  const onUnauthenticated = useRequireAuth()
  const { t } = useLocale()
  const { selectedId } = useSelectedProfile()
  const [profiles, setProfiles] = useState<BirthProfile[]>([])

  const [profileId, setProfileId] = useState<string | null>(null)
  const [currentAt, setCurrentAt] = useState<number>(() => Date.now())
  const [levels, setLevels] = useState<TimelineLevel[]>([
    { periods: [], loading: true },
    { periods: [], loading: false },
    { periods: [], loading: false },
  ])
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
        setProfileId(first.id)

        return Promise.all([
          astrologyApi.dashas(first.id, 1),
          /*
            The server's instant, not the browser's.

            A phone with its date reset — common — would put the "you are
            here" marker in the wrong mahadasha, which is a confident
            wrong answer to the exact question this screen exists for.
            `Date.now()` is only the initial value, replaced as soon as
            the server answers.
          */
          astrologyApi.currentDashas(first.id),
        ])
      })
      .then((result) => {
        if (cancelled || !result) return
        const [tree, current] = result

        if (tree.periods.length === 0) {
          setState('no-dashas')
          return
        }

        setCurrentAt(Date.parse(current.at))
        /*
          Every level replaced, not just the first.

          This read `[{...}, prev[1]!, prev[2]!]`, which is right for a
          refetch of the same chart and wrong for a profile switch: the
          effect also re-runs on `selectedId`, so one person's
          mahadashas rendered above another person's antardashas and
          pratyantardashas, with nothing on screen to say so.
        */
        setLevels([
          { periods: tree.periods, loading: false },
          { periods: [], loading: false },
          { periods: [], loading: false },
        ])
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

  /** Remembered so a failed level can be retried without re-tapping. */
  const [lastParent, setLastParent] = useState<Record<number, string | null>>({})

  function drillDown(level: number, parentId: string | null) {
    if (!profileId || level > 2) return
    setLastParent((prev) => ({ ...prev, [level]: parentId }))

    setLevels((prev) => {
      const next = [...prev]
      next[level] = { periods: [], loading: true }
      // Everything below the level being replaced is now about a parent
      // that is no longer selected. Leaving it would show a
      // pratyantardasha track belonging to a different antardasha.
      for (let i = level + 1; i < next.length; i++) next[i] = { periods: [], loading: false }
      return next
    })

    astrologyApi
      .dashas(profileId, (level + 1) as 1 | 2 | 3)
      .then(({ periods }) => {
        setLevels((prev) => {
          const next = [...prev]
          next[level] = { periods: childrenOf(periods, parentId), loading: false }
          return next
        })
      })
      .catch((err: unknown) => {
        if (!onUnauthenticated(err)) {
          setLevels((prev) => {
            const next = [...prev]
            next[level] = { periods: [], loading: false, failed: true }
            return next
          })
        }
      })
  }

  return (
    <main id="main" tabIndex={-1} className="mx-auto max-w-4xl px-4 py-8 sm:px-6">
      <header className="mb-6 flex flex-wrap items-baseline justify-between gap-3">
        <h1 className="font-serif text-2xl">{t.chart.dashasTitle}</h1>
        <ProfileSwitcher profiles={profiles} />
      </header>

      {state === 'loading' && (
        <div aria-busy="true" aria-live="polite">
          <span className="sr-only">{t.chart.dashasLoading}</span>
          <div className="h-11 w-full animate-pulse rounded-md bg-elevated" />
        </div>
      )}

      {state === 'error' && <LoadError onRetry={() => setAttempt((n) => n + 1)} />}

      {state === 'no-profile' && (
        <div className="rounded-lg border border-border p-8 text-center">
          <p className="text-sm text-ink-muted">
            {t.chart.dashasNoProfile}
          </p>
          <Button asChild className="mt-4">
            <Link href="/onboarding/birth">{t.chart.addBirthDetails}</Link>
          </Button>
        </div>
      )}

      {/*
        Distinct from `no-profile`: there IS a profile, and the reason
        there are no dashas is a specific, fixable one. Telling this
        reader to "add birth details" would be wrong — they have.
      */}
      {state === 'no-dashas' && (
        <div className="rounded-lg border border-border p-8 text-center">
          <p className="text-sm text-ink-muted">
            {t.chart.dashasNoBirthTime}
          </p>
          <Button asChild variant="secondary" className="mt-4">
            <Link href="/settings/birth-profiles">{t.chart.dashasAddBirthTime}</Link>
          </Button>
        </div>
      )}

      {state === 'ready' && (
        <DashaTimeline
          levels={levels}
          currentAt={currentAt}
          onDrillDown={drillDown}
          onRetryLevel={(level) => drillDown(level, lastParent[level] ?? null)}
        />
      )}
    </main>
  )
}

/**
 * Filter a level to one parent's children.
 *
 * The endpoint returns every period at a level — 81 antardashas, not the
 * nine belonging to the selected mahadasha — because the level is the
 * query and the parent is not. Filtering here rather than adding a
 * parameter keeps one cacheable response per level instead of nine.
 */
function childrenOf(periods: DashaPeriod[], parentId: string | null): DashaPeriod[] {
  if (!parentId) return []
  return periods.filter((p) => p.parent_id === parentId)
}
