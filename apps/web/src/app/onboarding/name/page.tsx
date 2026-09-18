'use client'

import { useState } from 'react'
import { useRouter } from 'next/navigation'

import { Wordmark } from '@/components/Logo'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { AuthError } from '@/lib/auth-api'
import { usersApi } from '@/lib/users-api'
import { getDictionary, LOCALE_NAMES, type Locale } from '@/lib/i18n/dictionaries'
import { cn } from '@/lib/utils'

/**
 * First-run: a name and a language.
 *
 * Birth details are deliberately NOT here — they are Phase 2, and asking
 * for date, time and place before someone has seen anything of value is
 * how you lose them at the door.
 */
export default function OnboardingNamePage() {
  const router = useRouter()
  const t = getDictionary('en')

  const [name, setName] = useState('')
  const [locale, setLocale] = useState<Locale>('en')
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError(null)

    const trimmed = name.trim()
    if (!trimmed) {
      setError(t.onboarding.nameRequired)
      return
    }

    setSaving(true)
    try {
      // Two calls rather than one endpoint: name is a profile field and
      // language is a preference, and they live in different tables. The
      // preference failing must not lose the name, so it is sent second
      // and its failure is non-fatal.
      await usersApi.updateProfile({ name: trimmed })
      try {
        await usersApi.updatePreferences({ preferred_language: locale })
      } catch {
        // The account exists and has a name. A language that did not
        // save is a setting they can change later, not a reason to
        // strand someone on the first screen they ever saw.
      }
      router.replace('/home')
    } catch (err) {
      setError(
        err instanceof AuthError && err.status === 401
          ? 'Your session expired. Please sign in again.'
          : t.common.somethingWentWrong,
      )
      setSaving(false)
    }
  }

  return (
    <main id="main" tabIndex={-1} className="flex min-h-dvh flex-col items-center justify-center px-6 py-16">
      <Wordmark />

      <div className="mt-10 w-full max-w-sm">
        <h1 className="text-balance text-center font-serif text-3xl tracking-tight">
          {t.onboarding.title}
        </h1>
        <p className="mt-3 text-balance text-center text-sm text-ink-muted">
          {t.onboarding.subtitle}
        </p>

        <form onSubmit={handleSubmit} className="mt-8" noValidate>
          <label htmlFor="name" className="mb-2 block text-sm font-medium">
            {t.onboarding.nameLabel}
          </label>
          <Input
            id="name"
            name="name"
            // Lets the browser offer the value it already knows.
            autoComplete="given-name"
            required
            maxLength={100}
            disabled={saving}
            placeholder={t.onboarding.namePlaceholder}
            value={name}
            onChange={(e) => {
              setName(e.target.value)
              if (error) setError(null)
            }}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? 'name-error' : undefined}
            className={cn('h-12 text-base', error && 'border-danger')}
          />
          {error && (
            <p id="name-error" role="alert" className="mt-2 text-sm text-danger">
              {error}
            </p>
          )}

          <fieldset className="mt-6">
            <legend className="mb-2 text-sm font-medium">
              {t.onboarding.languageLabel}
            </legend>
            {/* Radios, not a select. Two options do not need a dropdown,
                and each is then a real tab stop with a visible label. */}
            <div className="flex gap-3">
              {(Object.keys(LOCALE_NAMES) as Locale[]).map((code) => (
                <label
                  key={code}
                  className={cn(
                    'flex flex-1 cursor-pointer items-center justify-center rounded-lg border px-4 py-3 text-sm transition-colors',
                    'focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2 focus-within:ring-offset-background',
                    locale === code
                      ? 'border-gold bg-gold/10 text-ink'
                      : 'border-input text-ink-muted hover:border-border',
                  )}
                >
                  <input
                    type="radio"
                    name="locale"
                    value={code}
                    checked={locale === code}
                    onChange={() => setLocale(code)}
                    disabled={saving}
                    className="sr-only"
                  />
                  {LOCALE_NAMES[code]}
                </label>
              ))}
            </div>
          </fieldset>

          <Button type="submit" size="lg" disabled={saving} className="mt-7 w-full">
            {saving ? t.common.saving : t.onboarding.finish}
          </Button>
        </form>
      </div>
    </main>
  )
}
