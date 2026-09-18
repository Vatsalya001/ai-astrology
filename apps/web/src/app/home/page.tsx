'use client'

import Link from 'next/link'
import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'

import { SadeSatiIndicator } from '@/components/chart/SadeSatiIndicator'
import { AskBox } from '@/components/home/AskBox'
import { CurrentPeriodCard } from '@/components/home/CurrentPeriodCard'
import { TodayCard } from '@/components/home/TodayCard'
import { LoadError } from '@/components/LoadError'
import { Wordmark } from '@/components/Logo'
import { ProfileSwitcher } from '@/components/ProfileSwitcher'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'
import {
  astrologyApi,
  type BirthProfile,
  type CurrentDashas,
  type NatalTransits,
} from '@/lib/astrology-api'
import { resetAnalytics, track } from '@/lib/analytics'
import { useLocale } from '@/lib/i18n/context'
import type { Dictionary } from '@/lib/i18n/dictionaries'
import { resolveProfile, useSelectedProfile } from '@/lib/profile-context'
import { useTimeOfDay } from '@/lib/time-of-day'
import { useRequireAuth } from '@/lib/use-require-auth'
import { usersApi, type Profile } from '@/lib/users-api'

type State = 'loading' | 'ready' | 'error'

/**
 * The dashboard.
 *
 * ── Every card degrades on its own ──
 *
 * Four sources feed this screen: the user, the profile list, the current
 * dasha periods and today's transits. Only the first two are required;
 * the other two are `Promise.allSettled` because a transit refresh that
 * has not run yet must not blank the dasha card, and a chart with no
 * birth time must not blank the sky.
 *
 * Written with `Promise.all` first, which made the whole dashboard one
 * failure away from an error page — and the most likely failure, a 503
 * from transits before the worker's first run, is the one a new user
 * meets on their very first visit.
 */
export default function HomePage() {
  const router = useRouter()
  const onUnauthenticated = useRequireAuth()
  const { t, fill } = useLocale()
  const { selectedId } = useSelectedProfile()
  const timeOfDay = useTimeOfDay()

  const [profile, setProfile] = useState<Profile | null>(null)
  const [profiles, setProfiles] = useState<BirthProfile[]>([])
  const [dashas, setDashas] = useState<CurrentDashas | null>(null)
  const [transits, setTransits] = useState<NatalTransits | null>(null)
  const [aiEnabled, setAiEnabled] = useState(false)
  const [state, setState] = useState<State>('loading')
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false

    Promise.all([usersApi.me(), astrologyApi.listProfiles()])
      .then(async ([me, { birth_profiles: list }]) => {
        if (cancelled) return
        setProfile(me)
        setProfiles(list)

        const chosen = resolveProfile(list, selectedId)

        // Settled, not all: each of these is allowed to be missing, and
        // the flag call is the least important thing on the page.
        const [meta, current, sky] = await Promise.allSettled([
          api.meta(),
          chosen ? astrologyApi.currentDashas(chosen.id) : Promise.reject(new Error('no profile')),
          chosen ? astrologyApi.transits(chosen.id) : Promise.reject(new Error('no profile')),
        ])
        if (cancelled) return

        setAiEnabled(meta.status === 'fulfilled' && meta.value.features.ai_chat === true)
        setDashas(current.status === 'fulfilled' ? current.value : null)
        setTransits(sky.status === 'fulfilled' ? sky.value : null)
        setState('ready')
      })
      .catch((err: unknown) => {
        if (cancelled) return
        // Only a real "signed out" redirects. A dropped connection says
        // nothing about the session.
        if (!onUnauthenticated(err)) setState('error')
      })

    return () => {
      cancelled = true
    }
  }, [onUnauthenticated, attempt, selectedId])

  const moonSign = transits?.transits.find((p) => p.planet === 'Moon')?.sign ?? null

  return (
    <>
      <header className="border-b border-border/60">
        <div className="mx-auto flex max-w-2xl items-center justify-between px-4 py-4 sm:px-6">
          <Wordmark />
          <nav className="flex items-center gap-2">
            <Link
              href="/settings/profile"
              className="rounded-lg px-3 py-2 text-sm text-ink-muted transition-colors hover:text-ink"
            >
              {t.nav.settings}
            </Link>
            <Button
              variant="secondary"
              size="sm"
              onClick={async () => {
                const me = await usersApi.me().catch(() => null)
                await usersApi.signOut()
                if (me) track('logout_completed', { user_id: me.id, scope: 'device' })
                resetAnalytics()
                router.replace('/')
              }}
            >
              {t.nav.signOut}
            </Button>
          </nav>
        </div>
      </header>

      <main id="main" className="mx-auto max-w-2xl space-y-4 px-4 py-8 sm:px-6">
        {state === 'error' && <LoadError onRetry={() => setAttempt((n) => n + 1)} />}

        {state === 'loading' && (
          <div aria-busy="true" aria-live="polite" className="space-y-4">
            <span className="sr-only">{t.home.loading}</span>
            <Skeleton className="h-9 w-56" />
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-32 w-full" />
            <Skeleton className="h-28 w-full" />
          </div>
        )}

        {state === 'ready' && (
          <>
            <div className="flex flex-wrap items-baseline justify-between gap-3">
              {/*
                The greeting drops the time of day rather than guessing
                one, on the server and for anybody whose clock it cannot
                read. "Good morning" shown at midnight is worse than
                "Hello".
              */}
              <h1 className="font-serif text-2xl tracking-tight">
                {greeting(t, fill, timeOfDay, profile?.name)}
              </h1>
              <ProfileSwitcher profiles={profiles} />
            </div>

            <TodayCard moonSign={moonSign} />

            <AskBox enabled={aiEnabled} />

            {dashas && <CurrentPeriodCard current={dashas} />}

            {transits && (
              <SadeSatiIndicator status={transits.sade_sati} at={transits.at} />
            )}

            <div className="flex flex-wrap gap-2 pt-2">
              <Button asChild>
                <Link href="/kundli/planets">{t.home.viewKundli}</Link>
              </Button>
              {/*
                Disabled, not hidden. Same reasoning as the ask box: the
                shape of the dashboard is settled now so Phase 8 ships a
                marketplace rather than a marketplace plus a navigation
                redesign.
              */}
              <Button variant="secondary" disabled>
                {t.home.talkToAstrologer}
              </Button>
            </div>
          </>
        )}
      </main>
    </>
  )
}

/**
 * `Good morning, Priya` — or `Hello, Priya` when the clock is unknown.
 *
 * Both halves are optional and both absences are real: the time of day
 * is null on the server, and the name is null for somebody who skipped
 * it during onboarding. Four combinations, none of which may render a
 * stray comma.
 */
export function greeting(
  t: Dictionary,
  fill: (template: string, values: Record<string, string | number>) => string,
  time: ReturnType<typeof useTimeOfDay>,
  name?: string | null,
): string {
  const opening =
    time === 'morning'
      ? t.home.greetMorning
      : time === 'afternoon'
        ? t.home.greetAfternoon
        : time === 'evening'
          ? t.home.greetEvening
          : t.home.greetNeutral

  /*
    A template, not string concatenation.

    `${opening}, ${name}` bakes in a comma-then-name order that is not
    universal, and the greeting is the one string on the dashboard a
    reader sees before anything else. The dictionary owns the shape.
  */
  return name?.trim() ? fill(t.home.greetWithName, { greeting: opening, name: name.trim() }) : opening
}
