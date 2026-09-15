'use client'

import { useEffect, useState } from 'react'

import { Panel } from '@/components/ui'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { track } from '@/lib/analytics'
import { useLocale } from '@/lib/i18n/context'
import { usersApi, type Profile } from '@/lib/users-api'
import { useRequireAuth } from '@/lib/use-require-auth'

const GENDERS = ['male', 'female', 'other', 'prefer_not_to_say'] as const

export default function ProfileSettingsPage() {
  const { t } = useLocale()
  const [profile, setProfile] = useState<Profile | null>(null)
  const [name, setName] = useState('')
  const [gender, setGender] = useState('')
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading')
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const onUnauthenticated = useRequireAuth()

  useEffect(() => {
    usersApi
      .me()
      .then((p) => {
        setProfile(p)
        setName(p.name ?? '')
        setGender(p.gender ?? '')
        setState('ready')
      })
      .catch(onUnauthenticated)
  }, [onUnauthenticated])

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setSaving(true)
    setError(null)
    setSaved(false)

    try {
      const updated = await usersApi.updateProfile({
        name: name.trim(),
        ...(gender ? { gender } : {}),
      })
      setProfile(updated)
      setSaved(true)
      // The FIELD changed, never its value. A name is PII; the fact that
      // somebody edited their name is not.
      track('profile_updated', { user_id: updated.id, field: 'name' })
    } catch {
      setError(t.common.somethingWentWrong)
    } finally {
      setSaving(false)
    }
  }

  if (state === 'loading') {
    return (
      <div aria-busy="true" aria-live="polite">
        <span className="sr-only">{t.home.loading}</span>
        <Skeleton className="h-6 w-40" />
        <Skeleton className="mt-6 h-12 w-full" />
        <Skeleton className="mt-4 h-12 w-full" />
      </div>
    )
  }

  return (
    <form onSubmit={save} noValidate>
      <h2 className="font-serif text-2xl">{t.settings.profileTitle}</h2>

      <div className="mt-6">
        <label htmlFor="name" className="mb-2 block text-sm font-medium">
          {t.settings.nameLabel}
        </label>
        <Input
          id="name"
          value={name}
          maxLength={100}
          disabled={saving}
          onChange={(e) => {
            setName(e.target.value)
            setSaved(false)
          }}
          className="h-12 text-base"
        />
      </div>

      <fieldset className="mt-6">
        <legend className="mb-2 text-sm font-medium">{t.settings.genderLabel}</legend>
        <div className="flex flex-wrap gap-2">
          {GENDERS.map((g) => (
            <label
              key={g}
              className={
                'cursor-pointer rounded-lg border px-4 py-2 text-sm transition-colors ' +
                'focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2 focus-within:ring-offset-background ' +
                (gender === g
                  ? 'border-gold bg-gold/10 text-ink'
                  : 'border-input text-ink-muted hover:border-border')
              }
            >
              <input
                type="radio"
                name="gender"
                value={g}
                checked={gender === g}
                onChange={() => {
                  setGender(g)
                  setSaved(false)
                }}
                disabled={saving}
                className="sr-only"
              />
              {t.values[g]}
            </label>
          ))}
        </div>
      </fieldset>

      {/* Contact details are shown but not editable. Changing one has to
          go through verification, or a stolen access token becomes
          permanent account takeover by moving where codes are sent. */}
      <Panel className="mt-8">
        <dl className="space-y-3 text-sm">
          <div className="flex items-center justify-between gap-4">
            <dt className="text-ink-muted">{t.settings.emailLabel}</dt>
            <dd className="text-right">
              {profile?.email ?? t.settings.notSet}
              {profile?.email && (
                <span className="ml-2 text-xs text-ok">
                  {profile.email_verified ? `✓ ${t.settings.verified}` : t.settings.unverified}
                </span>
              )}
            </dd>
          </div>
          <div className="flex items-center justify-between gap-4">
            <dt className="text-ink-muted">{t.settings.phoneLabel}</dt>
            <dd className="text-right">
              {profile?.phone ?? t.settings.notSet}
              {profile?.phone && (
                <span className="ml-2 text-xs text-ok">
                  {profile.phone_verified ? `✓ ${t.settings.verified}` : t.settings.unverified}
                </span>
              )}
            </dd>
          </div>
        </dl>
        <p className="mt-4 text-xs text-ink-faint">{t.settings.contactLocked}</p>
      </Panel>

      {error && (
        <p role="alert" className="mt-4 text-sm text-danger">
          {error}
        </p>
      )}

      <div className="mt-8 flex items-center gap-4">
        <Button type="submit" disabled={saving}>
          {saving ? t.common.saving : t.settings.save}
        </Button>
        {/* aria-live so the confirmation is announced, not just shown. */}
        <span aria-live="polite" className="text-sm text-ok">
          {saved ? `✓ ${t.settings.saved}` : ''}
        </span>
      </div>
    </form>
  )
}
