'use client'

import { useCallback, useEffect, useState } from 'react'

import { Badge, Panel } from '@/components/ui'
import { Button } from '@/components/ui/button'
import { LoadError } from '@/components/LoadError'
import { Skeleton } from '@/components/ui/skeleton'
import { astrologyApi, type ShareLink } from '@/lib/astrology-api'
import { useLocale } from '@/lib/i18n/context'
import { useRequireAuth } from '@/lib/use-require-auth'

/**
 * The links you have handed out, and the button that takes them back.
 *
 * ── Why this screen has to exist ──
 *
 * `createShare` mints a bearer credential to a birth chart that works
 * for thirty days, for anybody holding it, with no account required.
 * The API has always had `List` and `Revoke`; nothing in the app called
 * either, so the only remedy for a link that reached the wrong person
 * was unreachable. Both client functions sat in `astrology-api.ts` with
 * zero call sites.
 *
 * Revocation is not a convenience here. The share endpoint deliberately
 * sends `no-store` on a public URL so that a CDN cannot keep serving a
 * chart after its owner turned the link off — that whole design assumes
 * the owner CAN turn it off.
 *
 * ── What it cannot do, and says so ──
 *
 * It cannot show you a link again. The plaintext token exists only in
 * the response to `createShare`; the database stores a hash, and the
 * listing deliberately omits the field. So the honest answer to "what
 * was that link?" is "revoke it and make a new one", which
 * `sharesNoToken` says in both languages rather than leaving the user to
 * hunt for a copy button that cannot exist.
 */

type State = 'loading' | 'ready' | 'error'

/** A link plus the profile it belongs to, which the API does not join. */
interface Row {
  share: ShareLink
  profileLabel: string
}

/**
 * Live, revoked or expired.
 *
 * Derived once when the data arrives rather than during render. The
 * frontend rules forbid `Date.now()` in render because the server and
 * client must agree or React reports a hydration mismatch — and beyond
 * the rule, a value recomputed on every render means a link can change
 * status mid-interaction for no reason the user can see.
 */
type Status = 'live' | 'revoked' | 'expired'

function statusOf(share: ShareLink, asOf: number): Status {
  if (share.revoked_at) return 'revoked'
  return Date.parse(share.expires_at) <= asOf ? 'expired' : 'live'
}

export default function SharesPage() {
  const { t } = useLocale()
  const [rows, setRows] = useState<Row[]>([])
  const [asOf, setAsOf] = useState(0)
  const [state, setState] = useState<State>('loading')
  const [revoking, setRevoking] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const onUnauthenticated = useRequireAuth()

  const load = useCallback(() => {
    /*
      One request per profile, because the API lists shares per profile
      and there is no cross-profile endpoint. `ListChartSharesForUser`
      exists in the queries but is wired only to the data export.

      N is the number of birth profiles a person has — a handful — so
      this is not the N+1 worth adding an endpoint for.
    */
    astrologyApi
      .listProfiles()
      .then(async ({ birth_profiles: profiles }) => {
        /*
          `all`, NOT `allSettled`, and this is the one decision on the
          page worth arguing about.

          allSettled would render the profiles that loaded and quietly
          drop the one that failed — and this screen's entire job is to
          answer "who can currently see my charts". A list that silently
          omits a profile answers that question WRONG, in the reassuring
          direction: the user concludes nothing is shared and stops
          looking. An error state with a retry is worse UX and the only
          honest option.
        */
        const perProfile = await Promise.all(
          profiles.map((p) =>
            astrologyApi
              .listShares(p.id)
              .then(({ shares }) => shares.map((share) => ({ share, profileLabel: p.label }))),
          ),
        )

        // Newest first, across every profile.
        const flat = perProfile
          .flat()
          .sort((a, b) => b.share.created_at.localeCompare(a.share.created_at))

        setRows(flat)
        setAsOf(Date.now())
        setState('ready')
      })
      .catch((err) => {
        // Only a real "signed out" redirects — a dropped connection or a
        // 500 gets a retry, because neither says the session is gone.
        if (!onUnauthenticated(err)) setState('error')
      })
  }, [onUnauthenticated])

  useEffect(load, [load])

  async function revoke(row: Row) {
    setRevoking(row.share.id)
    setError(null)
    try {
      await astrologyApi.revokeShare(row.share.birth_profile_id, row.share.id)
      // Reloaded rather than patched in memory. The server decides what
      // "revoked" means — it keeps the ORIGINAL timestamp if the link was
      // already off — and a local guess at the new row would drift from it.
      load()
    } catch {
      // Names the consequence, not the failure: the link is STILL LIVE.
      // "Something went wrong" after pressing revoke reads as "probably
      // fine", which is the opposite of the truth.
      setError(t.settings.sharesRevokeFailed)
    } finally {
      setRevoking(null)
    }
  }

  if (state === 'error') {
    return <LoadError onRetry={load} />
  }

  if (state === 'loading') {
    return (
      <div aria-busy="true" aria-live="polite">
        <span className="sr-only">{t.home.loading}</span>
        <Skeleton className="h-6 w-48" />
        <Skeleton className="mt-6 h-24 w-full" />
        <Skeleton className="mt-3 h-24 w-full" />
      </div>
    )
  }

  return (
    <div>
      <h2 className="font-serif text-2xl">{t.settings.sharesTitle}</h2>
      <p className="mt-2 text-sm text-ink-muted">{t.settings.sharesBody}</p>

      {rows.length === 0 ? (
        <Panel className="mt-6">
          <p className="text-sm text-ink-muted">{t.settings.sharesEmpty}</p>
        </Panel>
      ) : (
        <>
          <ul className="mt-6 space-y-3">
            {rows.map((row) => {
              const status = statusOf(row.share, asOf)
              return (
                <li key={row.share.id}>
                  <Panel className="p-4">
                    <div className="flex flex-wrap items-center justify-between gap-4">
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <p className="truncate text-sm font-medium">{row.profileLabel}</p>
                          {/*
                            A word, not just a colour. Roughly 8% of men
                            cannot distinguish the green from the amber,
                            and "is this link still live" is exactly the
                            question colour alone must not answer.
                          */}
                          <Badge
                            tone={
                              status === 'live' ? 'ok' : status === 'expired' ? 'warn' : 'neutral'
                            }
                          >
                            {status === 'live'
                              ? t.settings.sharesLive
                              : status === 'expired'
                                ? t.settings.sharesExpired
                                : t.settings.sharesRevoked}
                          </Badge>
                        </div>
                        <p className="mt-1 text-xs text-ink-faint">
                          {t.settings.sharesCreated}: {formatDate(row.share.created_at)}
                          {' · '}
                          {t.settings.expires}: {formatDate(row.share.expires_at)}
                          {' · '}
                          {t.settings.sharesViews}: {row.share.view_count}
                        </p>
                      </div>

                      {status === 'live' && (
                        <Button
                          variant="secondary"
                          size="sm"
                          disabled={revoking === row.share.id}
                          // Named for a screen reader, which otherwise
                          // hears a page of identical "Revoke" buttons
                          // with no way to tell which link is which.
                          aria-label={`${t.settings.revoke} — ${row.profileLabel}, ${formatDate(
                            row.share.created_at,
                          )}`}
                          onClick={() => void revoke(row)}
                        >
                          {revoking === row.share.id ? t.settings.revoking : t.settings.revoke}
                        </Button>
                      )}
                    </div>
                  </Panel>
                </li>
              )
            })}
          </ul>

          <p className="mt-6 text-xs text-ink-faint">{t.settings.sharesNoToken}</p>
        </>
      )}

      {error && (
        <p role="alert" className="mt-4 text-sm text-danger">
          {error}
        </p>
      )}
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
