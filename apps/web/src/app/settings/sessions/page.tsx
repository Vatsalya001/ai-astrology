'use client'

import { useCallback, useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'

import { Badge, Panel } from '@/components/ui'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { resetAnalytics, track } from '@/lib/analytics'
import { useLocale } from '@/lib/i18n/context'
import { authApi } from '@/lib/auth-api'
import { usersApi, type DeviceSession } from '@/lib/users-api'
import { useRequireAuth } from '@/lib/use-require-auth'

export default function SessionsPage() {
  const { t } = useLocale()
  const router = useRouter()
  const [sessions, setSessions] = useState<DeviceSession[]>([])
  const [state, setState] = useState<'loading' | 'ready'>('loading')
  const [revoking, setRevoking] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const onUnauthenticated = useRequireAuth()

  const load = useCallback(() => {
    usersApi
      .sessions()
      .then(({ sessions: list }) => {
        setSessions(list)
        setState('ready')
      })
      .catch(onUnauthenticated)
  }, [onUnauthenticated])

  useEffect(load, [load])

  async function revoke(id: string, isCurrent: boolean) {
    setRevoking(id)
    setError(null)
    try {
      const me = await usersApi.me()
      await usersApi.revokeSession(id)
      track('session_revoked', { user_id: me.id })

      if (isCurrent) {
        // Revoking THIS device kills its refresh chain. The access token
        // in memory stays valid until it expires — access tokens are
        // stateless by design — so the app would keep working for up to
        // fifteen minutes and then fail confusingly. Signing out locally
        // makes the button mean what it says.
        await usersApi.signOut()
        resetAnalytics()
        router.replace('/')
        return
      }

      load()
    } catch {
      setError(t.common.somethingWentWrong)
    } finally {
      setRevoking(null)
    }
  }

  if (state === 'loading') {
    return (
      <div aria-busy="true" aria-live="polite">
        <span className="sr-only">{t.home.loading}</span>
        <Skeleton className="h-6 w-48" />
        <Skeleton className="mt-6 h-20 w-full" />
        <Skeleton className="mt-3 h-20 w-full" />
      </div>
    )
  }

  return (
    <div>
      <h2 className="font-serif text-2xl">{t.settings.sessionsTitle}</h2>
      <p className="mt-2 text-sm text-ink-muted">{t.settings.sessionsBody}</p>

      {sessions.length === 0 ? (
        // The empty state is a real state, not an oversight — a user with
        // one device sees this every time.
        <Panel className="mt-6">
          <p className="text-sm text-ink-muted">{t.settings.sessionsEmpty}</p>
        </Panel>
      ) : (
        <ul className="mt-6 space-y-3">
          {sessions.map((s) => (
            <li key={s.id}>
              <Panel className="p-4">
                <div className="flex flex-wrap items-center justify-between gap-4">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <p className="truncate text-sm font-medium">
                        {s.user_agent || '—'}
                      </p>
                      {s.current && <Badge tone="gold">{t.settings.thisDevice}</Badge>}
                    </div>
                    <p className="mt-1 text-xs text-ink-faint">
                      {t.settings.signedIn}: {formatDate(s.created_at)}
                      {' · '}
                      {t.settings.expires}: {formatDate(s.expires_at)}
                    </p>
                  </div>
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={revoking === s.id}
                    onClick={() => void revoke(s.id, s.current)}
                  >
                    {revoking === s.id ? t.settings.revoking : t.settings.revoke}
                  </Button>
                </div>
              </Panel>
            </li>
          ))}
        </ul>
      )}

      {error && (
        <p role="alert" className="mt-4 text-sm text-danger">
          {error}
        </p>
      )}

      <div className="mt-8 border-t border-border/60 pt-6">
        <Button
          variant="secondary"
          onClick={async () => {
            const me = await usersApi.me().catch(() => null)
            await authApi.logout().catch(() => undefined)
            await usersApi.signOut()
            if (me) track('logout_completed', { user_id: me.id, scope: 'all' })
            resetAnalytics()
            router.replace('/')
          }}
        >
          {t.settings.signOutEverywhere}
        </Button>
      </div>
    </div>
  )
}

/**
 * Locale-independent and stable.
 *
 * `toLocaleDateString()` renders differently on the server and in the
 * browser whenever their locales differ, which React reports as a
 * hydration mismatch. These are timestamps, not prose.
 */
function formatDate(iso: string): string {
  return iso.slice(0, 10)
}
