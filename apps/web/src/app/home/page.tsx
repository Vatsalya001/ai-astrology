'use client'

import Link from 'next/link'
import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'

import { LoadError } from '@/components/LoadError'
import { Wordmark } from '@/components/Logo'
import { Badge, Panel, SectionLabel } from '@/components/ui'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useRequireAuth } from '@/lib/use-require-auth'
import { usersApi, type Profile } from '@/lib/users-api'
import { useLocale } from '@/lib/i18n/context'
import { resetAnalytics, track } from '@/lib/analytics'

/**
 * The authenticated shell.
 *
 * Phase 3 fills this with the chart. For now it proves the loop closes:
 * a signed-in user lands somewhere that knows who they are, and can sign
 * out again.
 */
export default function HomePage() {
  const router = useRouter()
  const onUnauthenticated = useRequireAuth()
  const { t, fill } = useLocale()
  const [profile, setProfile] = useState<Profile | null>(null)
  // 'error' was declared here from the start and nothing ever set it —
  // the same declared-never-populated pattern that hid the unpopulated
  // `current` flag on the sessions list and `Identities` in the export.
  // It is reachable now.
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading')
  // Bumped by Retry; the effect depends on it, so the fetch re-runs.
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false

    usersApi
      .me()
      .then((p) => {
        if (cancelled) return
        setProfile(p)
        setState('ready')
      })
      .catch((err: unknown) => {
        if (cancelled) return
        // Only a real "signed out" redirects. A dropped connection or a
        // 500 says nothing about the session, and throwing the user to
        // /auth for one tells them they have been signed out when they
        // have not.
        if (!onUnauthenticated(err)) setState('error')
      })

    return () => {
      cancelled = true
    }
  }, [onUnauthenticated, attempt])

  return (
    <>
      <header className="border-b border-border/60">
        <div className="mx-auto flex max-w-4xl items-center justify-between px-6 py-5">
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

      <main id="main" className="mx-auto max-w-4xl px-6 py-14">
        {state === 'error' ? (
          <LoadError onRetry={() => setAttempt((n) => n + 1)} />
        ) : state === 'loading' ? (
          <div aria-busy="true" aria-live="polite">
            <span className="sr-only">{t.home.loading}</span>
            <Skeleton className="h-10 w-64" />
            <Skeleton className="mt-4 h-4 w-full max-w-md" />
            <Skeleton className="mt-8 h-40 w-full" />
          </div>
        ) : (
          <>
            <SectionLabel>{t.home.sectionLabel}</SectionLabel>
            <h1 className="font-serif text-4xl tracking-tight">
              {profile?.name
                ? fill(t.home.welcomeNamed, { name: profile.name })
                : t.home.welcome}
            </h1>
            <p className="mt-3 max-w-xl text-sm leading-relaxed text-ink-muted">
              {t.home.body}
            </p>

            <Panel className="mt-8">
              <div className="mb-4 flex items-center gap-2.5">
                <Badge tone="accent">Phase 2</Badge>
                <h2 className="font-serif text-xl">{t.home.chartTitle}</h2>
              </div>
              <p className="text-sm leading-relaxed text-ink-muted">
                {t.home.chartBody}
              </p>
              <Button disabled className="mt-5">
                {t.home.chartCta}
              </Button>
            </Panel>
          </>
        )}
      </main>
    </>
  )
}
