'use client'

import { use, useCallback, useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'

import { LoadError } from '@/components/LoadError'
import { PlaceSearch } from '@/components/PlaceSearch'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { astrologyApi, type BirthProfile, type Place } from '@/lib/astrology-api'
import { validateBirthDate, validateBirthTime } from '@/lib/birth-validation'
import { getDictionary } from '@/lib/i18n/dictionaries'
import { trackAsUser } from '@/lib/analytics'
import { useRequireAuth } from '@/lib/use-require-auth'
import { cn } from '@/lib/utils'

/**
 * Correcting birth details.
 *
 * The warning is the point of this screen, not decoration. Saving does
 * not modify the profile — it creates version N+1 and marks the old one
 * superseded, so a reading given last month stays attached to the
 * details it was actually computed from. Without saying so, someone
 * corrects a birth time and cannot understand why their old reading did
 * not change.
 *
 * The response carries a DIFFERENT id from the one in the URL, for the
 * same reason. This screen navigates away rather than trying to stay on
 * a record that no longer exists at that address.
 */
export default function EditBirthProfilePage({
  params,
}: {
  params: Promise<{ id: string }>
}) {
  const { id } = use(params)
  const ready = useRequireAuth()
  const router = useRouter()
  const t = getDictionary('en')

  const [profile, setProfile] = useState<BirthProfile | null>(null)
  const [loadFailed, setLoadFailed] = useState(false)

  const [day, setDay] = useState('')
  const [month, setMonth] = useState('')
  const [year, setYear] = useState('')
  const [hour, setHour] = useState('')
  const [minute, setMinute] = useState('')
  const [timeUnknown, setTimeUnknown] = useState(false)
  const [place, setPlace] = useState<Place | null>(null)

  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  // No synchronous setState here — see the profile list for why.
  const load = useCallback(() => {
    astrologyApi
      .getProfile(id)
      .then((loaded) => {
        setProfile(loaded)

        const [y, m, d] = loaded.birth_date.split('-')
        setYear(y ?? '')
        setMonth(String(Number(m ?? 0)))
        setDay(String(Number(d ?? 0)))

        if (loaded.birth_time) {
          const [h, min] = loaded.birth_time.split(':')
          setHour(String(Number(h ?? 0)))
          setMinute(String(Number(min ?? 0)))
        }
        setTimeUnknown(loaded.time_accuracy === 'unknown')
        setLoadFailed(false)
      })
      .catch(() => setLoadFailed(true))
  }, [id])

  function retry() {
    setLoadFailed(false)
    load()
  }

  useEffect(() => {
    if (!ready) return
    load()
  }, [ready, load])

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()

    const date = validateBirthDate(day, month, year)
    if (!date.ok) {
      setError(t.birth[date.reason])
      return
    }
    if (!timeUnknown) {
      const time = validateBirthTime(hour, minute)
      if (!time.ok) {
        setError(t.birth[time.reason])
        return
      }
    }
    // The place is the one field with no existing value to fall back on:
    // the API takes a place_id and the stored profile holds resolved
    // coordinates, not the id it came from. Re-selecting is required
    // rather than optional, and the label says so.
    if (!place) {
      setError(t.birth.placeRequired)
      return
    }

    setError(null)
    setSaving(true)

    try {
      const updated = await astrologyApi.updateProfile(id, {
        label: profile?.label ?? 'self',
        birth_date: date.value,
        ...(timeUnknown
          ? {}
          : { birth_time: `${hour.padStart(2, '0')}:${minute.padStart(2, '0')}` }),
        time_accuracy: timeUnknown ? 'unknown' : 'exact',
        place_id: place.id,
      })

      void trackAsUser('birth_profile_edited', { new_version: updated.version })
      // The chart for the NEW version has to be computed; the old one
      // stays attached to the old version and is not recomputed.
      router.replace(`/onboarding/computing?profile=${updated.id}`)
    } catch {
      setSaving(false)
      setError(t.common.somethingWentWrong)
    }
  }

  if (loadFailed) {
    return <LoadError onRetry={retry} />
  }

  if (!ready || !profile) {
    return (
      <div className="space-y-4" aria-hidden="true">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-24 w-full rounded-xl" />
        <Skeleton className="h-64 w-full rounded-xl" />
      </div>
    )
  }

  return (
    <div>
      <h1 className="font-serif text-2xl tracking-tight">{t.profiles.editTitle}</h1>

      {/* The warning, before the fields rather than beside the button.
          Someone who reads only the first line of a screen should still
          learn that saving creates a version. */}
      <p
        role="note"
        className="mt-4 rounded-lg border border-gold/30 bg-gold/5 p-4 text-sm leading-relaxed"
      >
        {t.profiles.editWarning}
      </p>

      <form onSubmit={handleSubmit} className="mt-8" noValidate>
        <fieldset disabled={saving}>
          <legend className="sr-only">{t.birth.dateTitle}</legend>

          <div className="grid grid-cols-[1fr_1fr_1.4fr] gap-3">
            <Field id="day" label={t.birth.dayLabel} value={day} onChange={setDay} max={2} />
            <Field id="month" label={t.birth.monthLabel} value={month} onChange={setMonth} max={2} />
            <Field id="year" label={t.birth.yearLabel} value={year} onChange={setYear} max={4} />
          </div>

          <div
            className={cn(
              'mt-6 grid grid-cols-2 gap-3 transition-opacity',
              timeUnknown && 'pointer-events-none opacity-40',
            )}
          >
            <Field
              id="hour"
              label={t.birth.hourLabel}
              value={hour}
              onChange={setHour}
              max={2}
              disabled={timeUnknown}
            />
            <Field
              id="minute"
              label={t.birth.minuteLabel}
              value={minute}
              onChange={setMinute}
              max={2}
              disabled={timeUnknown}
            />
          </div>

          <label className="mt-4 flex cursor-pointer items-start gap-3 rounded-lg border border-input p-4 focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2 focus-within:ring-offset-background">
            <input
              type="checkbox"
              checked={timeUnknown}
              onChange={(e) => {
                setTimeUnknown(e.target.checked)
                setError(null)
              }}
              className="mt-0.5 size-4 shrink-0 accent-gold"
            />
            <span className="text-sm">{t.birth.unknownLabel}</span>
          </label>

          <div className="mt-6">
            <PlaceSearch selected={place} onSelect={setPlace} disabled={saving} />
            {!place && (
              <p className="mt-2 text-sm text-ink-muted">
                {/* Named, not silent. The stored profile holds resolved
                    coordinates rather than the gazetteer id they came
                    from, so the place genuinely has to be chosen again. */}
                Currently {profile.birth_place}. Choose it again to confirm.
              </p>
            )}
          </div>
        </fieldset>

        {error && (
          <p role="alert" className="mt-4 text-sm text-danger">
            {error}
          </p>
        )}

        <div className="mt-8 flex gap-3">
          <Button type="submit" size="lg" disabled={saving} className="flex-1">
            {saving ? t.common.saving : t.common.continue}
          </Button>
          <Button type="button" variant="ghost" size="lg" onClick={() => router.back()}>
            {t.common.cancel}
          </Button>
        </div>
      </form>
    </div>
  )
}

function Field({
  id,
  label,
  value,
  onChange,
  max,
  disabled,
}: {
  id: string
  label: string
  value: string
  onChange: (next: string) => void
  max: number
  disabled?: boolean
}) {
  return (
    <div>
      <label htmlFor={id} className="mb-2 block text-sm font-medium">
        {label}
      </label>
      <Input
        id={id}
        name={id}
        inputMode="numeric"
        autoComplete="off"
        maxLength={max}
        disabled={disabled}
        value={value}
        onChange={(e) => onChange(e.target.value.replace(/\D/g, '').slice(0, max))}
        className="h-12 text-center text-base tabular-nums"
      />
    </div>
  )
}
