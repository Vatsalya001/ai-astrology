'use client'

import { useCallback, useEffect, useState } from 'react'
import Link from 'next/link'

import { LoadError } from '@/components/LoadError'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { astrologyApi, type BirthProfile } from '@/lib/astrology-api'
import { getDictionary } from '@/lib/i18n/dictionaries'
import { useRequireAuth } from '@/lib/use-require-auth'

/**
 * The birth-profile manager.
 *
 * Deleting is SOFT, and the confirmation says so. The charts and
 * readings that reference a profile have to stay explicable — a reading
 * given last month was based on specific birth details, and "which
 * details?" must keep having an answer. Hard deletion happens only when
 * the whole account goes, where the cascade takes profiles, charts and
 * dashas together.
 */
export default function BirthProfilesPage() {
  const ready = useRequireAuth()
  const t = getDictionary('en')

  const [profiles, setProfiles] = useState<BirthProfile[] | null>(null)
  const [failed, setFailed] = useState(false)
  const [removing, setRemoving] = useState<string | null>(null)

  // No synchronous setState in here. The effect calls it, and clearing
  // the error state before the request would be a cascading render —
  // which the React compiler rejects, correctly. Retrying clears the
  // error instead, in an event handler where that is the right place.
  const load = useCallback(() => {
    astrologyApi
      .listProfiles()
      .then((response) => {
        setProfiles(response.birth_profiles)
        setFailed(false)
      })
      .catch(() => setFailed(true))
  }, [])

  useEffect(() => {
    if (!ready) return
    load()
  }, [ready, load])

  function retry() {
    setFailed(false)
    load()
  }

  async function remove(id: string) {
    setRemoving(id)
    try {
      await astrologyApi.deleteProfile(id)
      setProfiles((current) => current?.filter((profile) => profile.id !== id) ?? null)
    } catch {
      setFailed(true)
    } finally {
      setRemoving(null)
    }
  }

  return (
    <div>
      <header>
        <h1 className="font-serif text-2xl tracking-tight">{t.profiles.title}</h1>
        <p className="mt-2 text-sm text-ink-muted">{t.profiles.subtitle}</p>
      </header>

      {/* Four states, all four handled: loading, error, empty, populated.
          A skeleton shaped like the eventual content, never a spinner on
          a blank page. */}
      {!ready || profiles === null ? (
        failed ? (
          <LoadError onRetry={retry} />
        ) : (
          <div className="mt-8 space-y-3" aria-hidden="true">
            <Skeleton className="h-28 w-full rounded-xl" />
            <Skeleton className="h-28 w-full rounded-xl" />
          </div>
        )
      ) : profiles.length === 0 ? (
        <div className="mt-10 rounded-xl border border-border bg-surface-2 p-8 text-center">
          <p className="text-sm text-ink-muted">{t.profiles.empty}</p>
          <Button asChild size="lg" className="mt-6">
            <Link href="/onboarding/birth">{t.profiles.emptyCta}</Link>
          </Button>
        </div>
      ) : (
        <>
          <ul className="mt-8 space-y-3">
            {profiles.map((profile) => (
              <li key={profile.id}>
                <ProfileCard
                  profile={profile}
                  removing={removing === profile.id}
                  onRemove={() => remove(profile.id)}
                />
              </li>
            ))}
          </ul>

          <Button asChild variant="outline" className="mt-6 w-full">
            <Link href="/onboarding/birth">{t.profiles.add}</Link>
          </Button>
        </>
      )}
    </div>
  )
}

function ProfileCard({
  profile,
  removing,
  onRemove,
}: {
  profile: BirthProfile
  removing: boolean
  onRemove: () => void
}) {
  const t = getDictionary('en')
  const [confirming, setConfirming] = useState(false)

  return (
    <article className="rounded-xl border border-border bg-surface-2 p-5">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <h2 className="truncate font-medium">{profile.label}</h2>
          <p className="mt-1 text-sm text-ink-muted">
            {formatDate(profile.birth_date)}
            {profile.birth_time ? ` · ${profile.birth_time}` : ''}
          </p>
          <p className="mt-0.5 truncate text-sm text-ink-muted">{profile.birth_place}</p>
        </div>

        {profile.version > 1 && (
          <span className="shrink-0 rounded-full bg-gold/10 px-2.5 py-1 text-xs text-gold">
            {t.profiles.versionLabel.replace('{version}', String(profile.version))}
          </span>
        )}
      </div>

      {/* Persistent, not nagging: one line on the card, no dialog, no
          repeated prompt. It offers the fix rather than just naming the
          gap. */}
      {profile.time_accuracy === 'unknown' && (
        <div className="mt-4 rounded-lg bg-background/60 p-3">
          <p className="text-sm">
            <span aria-hidden="true">◷</span> {t.profiles.unknownTime}
          </p>
          <p className="mt-1 text-xs text-ink-muted">{t.profiles.unknownTimeBanner}</p>
          <Link
            href={`/settings/birth-profiles/${profile.id}/edit`}
            className="mt-2 inline-block text-sm text-gold underline underline-offset-4 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            {t.profiles.unknownTimeCta}
          </Link>
        </div>
      )}

      <div className="mt-4 flex gap-2">
        <Button asChild variant="outline" size="sm">
          <Link href={`/settings/birth-profiles/${profile.id}/edit`}>{t.profiles.edit}</Link>
        </Button>

        {confirming ? (
          <div className="flex flex-1 flex-col gap-2 rounded-lg border border-border p-3">
            <p className="text-sm">{t.profiles.removeConfirm}</p>
            {/* Says what actually happens. "Delete" on a screen where
                deletion is soft is a lie the user finds out about later. */}
            <p className="text-xs text-ink-muted">{t.profiles.removeExplained}</p>
            <div className="flex gap-2">
              <Button size="sm" variant="destructive" disabled={removing} onClick={onRemove}>
                {removing ? t.common.saving : t.profiles.remove}
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setConfirming(false)}>
                {t.common.cancel}
              </Button>
            </div>
          </div>
        ) : (
          <Button variant="ghost" size="sm" onClick={() => setConfirming(true)}>
            {t.profiles.remove}
          </Button>
        )}
      </div>
    </article>
  )
}

/**
 * Renders a YYYY-MM-DD date without letting a timezone move it.
 *
 * `new Date('1994-08-17')` is parsed as UTC midnight and then formatted
 * in the local zone, so west of Greenwich it renders as the 16th. A
 * birth date is a calendar date, not an instant, and must not shift
 * because of where the reader is sitting.
 */
function formatDate(iso: string): string {
  const [year, month, day] = iso.split('-').map(Number)
  if (!year || !month || !day) return iso
  return new Date(Date.UTC(year, month - 1, day)).toLocaleDateString('en-GB', {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
    timeZone: 'UTC',
  })
}
