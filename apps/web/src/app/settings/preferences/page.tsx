'use client'

import { useEffect, useState } from 'react'

import { LoadError } from '@/components/LoadError'
import { Skeleton } from '@/components/ui/skeleton'
import { track } from '@/lib/analytics'
import { LOCALE_NAMES, type Locale } from '@/lib/i18n/dictionaries'
import { useLocale } from '@/lib/i18n/context'
import { usersApi, type Preferences } from '@/lib/users-api'
import { useRequireAuth } from '@/lib/use-require-auth'

/** Must match the server's allowlists in internal/users/service.go. */
const OPTIONS = {
  astrology_system: ['vedic', 'western'],
  chart_style: ['north', 'south', 'east'],
  theme: ['dark', 'light', 'system'],
} as const

export default function PreferencesPage() {
  const { t, setLocale } = useLocale()
  const [prefs, setPrefs] = useState<Preferences | null>(null)
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading')
  const [saving, setSaving] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const onUnauthenticated = useRequireAuth()
  // Bumped by Retry; the effect depends on it, so the fetch re-runs.
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    usersApi
      .preferences()
      .then((p) => {
        setPrefs(p)
        // The saved language wins over the browser's: it is what this
        // person chose, on this account, deliberately.
        if (p.preferred_language === 'en' || p.preferred_language === 'hi') {
          setLocale(p.preferred_language)
        }
        setState('ready')
      })
      .catch((err) => {
        // Only a real "signed out" redirects. Anything else — a dropped
        // connection, a 500, a deploy in progress — gets an error state
        // with a retry, because none of those say the session is gone.
        if (!onUnauthenticated(err)) setState('error')
      })
  }, [onUnauthenticated, setLocale, attempt])

  async function update(field: keyof Preferences, value: string) {
    setSaving(field)
    setError(null)

    // Optimistic: these are single-tap choices and a spinner on each one
    // makes the screen feel broken. Reverted below if the write fails.
    const previous = prefs
    setPrefs((p) => (p ? { ...p, [field]: value } : p))
    if (field === 'preferred_language') setLocale(value as Locale)

    try {
      const updated = await usersApi.updatePreferences({ [field]: value })
      setPrefs(updated)
      // The field name, never the value.
      const me = await usersApi.me()
      track('preferences_updated', { user_id: me.id, field })
    } catch {
      setPrefs(previous)
      if (field === 'preferred_language' && previous) {
        setLocale(previous.preferred_language as Locale)
      }
      setError(t.common.somethingWentWrong)
    } finally {
      setSaving(null)
    }
  }

  if (state === 'error') {
    return <LoadError onRetry={() => setAttempt((n) => n + 1)} />
  }

  if (state === 'loading' || !prefs) {
    return (
      <div aria-busy="true" aria-live="polite">
        <span className="sr-only">{t.home.loading}</span>
        <Skeleton className="h-6 w-40" />
        <Skeleton className="mt-6 h-20 w-full" />
        <Skeleton className="mt-4 h-20 w-full" />
      </div>
    )
  }

  return (
    <div>
      <h2 className="font-serif text-2xl">{t.settings.prefsTitle}</h2>

      <Choice
        legend={t.settings.languageLabel}
        options={(Object.keys(LOCALE_NAMES) as Locale[]).map((code) => ({
          value: code,
          label: LOCALE_NAMES[code],
        }))}
        selected={prefs.preferred_language}
        busy={saving === 'preferred_language'}
        onSelect={(v) => void update('preferred_language', v)}
      />

      <Choice
        legend={t.settings.systemLabel}
        options={OPTIONS.astrology_system.map((v) => ({ value: v, label: t.values[v] }))}
        selected={prefs.astrology_system}
        busy={saving === 'astrology_system'}
        onSelect={(v) => void update('astrology_system', v)}
      />

      <Choice
        legend={t.settings.chartStyleLabel}
        options={OPTIONS.chart_style.map((v) => ({ value: v, label: t.values[v] }))}
        selected={prefs.chart_style}
        busy={saving === 'chart_style'}
        onSelect={(v) => void update('chart_style', v)}
      />

      <Choice
        legend={t.settings.themeLabel}
        options={OPTIONS.theme.map((v) => ({ value: v, label: t.values[v] }))}
        selected={prefs.theme}
        busy={saving === 'theme'}
        onSelect={(v) => void update('theme', v)}
      />

      {error && (
        <p role="alert" className="mt-6 text-sm text-danger">
          {error}
        </p>
      )}
    </div>
  )
}

function Choice({
  legend,
  options,
  selected,
  busy,
  onSelect,
}: {
  legend: string
  options: Array<{ value: string; label: string }>
  selected: string
  busy: boolean
  onSelect: (value: string) => void
}) {
  return (
    <fieldset className="mt-8" disabled={busy}>
      <legend className="mb-3 text-sm font-medium">{legend}</legend>
      <div className="flex flex-wrap gap-2">
        {options.map((option) => (
          <label
            key={option.value}
            className={
              'cursor-pointer rounded-lg border px-4 py-2.5 text-sm transition-colors ' +
              'focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2 focus-within:ring-offset-background ' +
              (selected === option.value
                ? 'border-gold bg-gold/10 text-ink'
                : 'border-input text-ink-muted hover:border-border') +
              (busy ? ' opacity-60' : '')
            }
          >
            <input
              type="radio"
              name={legend}
              value={option.value}
              checked={selected === option.value}
              onChange={() => onSelect(option.value)}
              className="sr-only"
            />
            {option.label}
          </label>
        ))}
      </div>
    </fieldset>
  )
}
