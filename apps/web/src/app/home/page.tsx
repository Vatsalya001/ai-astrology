'use client'

import Link from 'next/link'
import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'

import { Wordmark } from '@/components/Logo'
import { Badge, Panel, SectionLabel } from '@/components/ui'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
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
  const { t, fill } = useLocale()
  const [profile, setProfile] = useState<Profile | null>(null)
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading')

  useEffect(() => {
    let cancelled = false

    usersApi
      .me()
      .then((p) => {
        if (cancelled) return
        setProfile(p)
        setState('ready')
      })
      .catch(() => {
        if (cancelled) return
        // No valid session. Replace rather than push, so Back does not
        // return to a page that will only bounce them again.
        router.replace('/auth')
      })

    return () => {
      cancelled = true
    }
  }, [router])

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
        {state === 'loading' ? (
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
