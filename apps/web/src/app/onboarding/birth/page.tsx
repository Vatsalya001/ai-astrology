'use client'

import { useState } from 'react'
import { useRouter } from 'next/navigation'

import { PlaceSearch } from '@/components/PlaceSearch'
import { StepHeader } from '@/components/StepHeader'
import { Wordmark } from '@/components/Logo'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { AuthError } from '@/lib/auth-api'
import { astrologyApi, type Place } from '@/lib/astrology-api'
import { validateBirthDate, validateBirthTime } from '@/lib/birth-validation'
import { getDictionary } from '@/lib/i18n/dictionaries'
import { track, trackAsUser } from '@/lib/analytics'
import { cn } from '@/lib/utils'

/**
 * Birth details, in three steps.
 *
 * One question per screen, because this is the highest drop-off point in
 * the product and every field on a screen costs conversion. The three
 * are not arbitrary: date is the one everybody knows, time is the one
 * many do not, and place needs a search. Putting them together would
 * mean the person who does not know their time abandons the whole form
 * rather than one step of it.
 *
 * Nothing is sent until the last step. A half-finished birth profile is
 * worse than none: it produces a chart nobody asked for, from details
 * nobody confirmed.
 */

const TOTAL_STEPS = 3

export default function BirthDetailsPage() {
  const router = useRouter()
  const t = getDictionary('en')

  const [step, setStep] = useState(1)

  const [day, setDay] = useState('')
  const [month, setMonth] = useState('')
  const [year, setYear] = useState('')

  const [hour, setHour] = useState('')
  const [minute, setMinute] = useState('')
  const [timeUnknown, setTimeUnknown] = useState(false)

  const [place, setPlace] = useState<Place | null>(null)

  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  function goBack() {
    setError(null)
    if (step === 1) {
      router.back()
      return
    }
    setStep(step - 1)
  }

  function handleDateStep(e: React.FormEvent) {
    e.preventDefault()
    const result = validateBirthDate(day, month, year)
    if (!result.ok) {
      setError(t.birth[result.reason])
      return
    }
    setError(null)
    track('birth_profile_step_completed', { step: 1 })
    setStep(2)
  }

  function handleTimeStep(e: React.FormEvent) {
    e.preventDefault()
    if (!timeUnknown) {
      const result = validateBirthTime(hour, minute)
      if (!result.ok) {
        setError(t.birth[result.reason])
        return
      }
    }
    setError(null)
    track('birth_profile_step_completed', { step: 2 })
    setStep(3)
  }

  async function handlePlaceStep(e: React.FormEvent) {
    e.preventDefault()
    if (!place) {
      setError(t.birth.placeRequired)
      return
    }

    setError(null)
    setSaving(true)

    const date = validateBirthDate(day, month, year)
    if (!date.ok) {
      // Only reachable if someone walked back and broke the date; the
      // step-1 guard is the real one.
      setStep(1)
      setSaving(false)
      return
    }

    try {
      const profile = await astrologyApi.createProfile({
        label: 'self',
        birth_date: date.value,
        // Omitted entirely when unknown — an empty string would be a
        // client sending a birth time it does not have.
        ...(timeUnknown
          ? {}
          : { birth_time: `${hour.padStart(2, '0')}:${minute.padStart(2, '0')}` }),
        time_accuracy: timeUnknown ? 'unknown' : 'exact',
        place_id: place.id,
      })

      track('birth_profile_step_completed', { step: 3 })
      // The accuracy ENUM, never the date, the time or the place. Those
      // three together identify a person.
      void trackAsUser('birth_profile_created', {
        time_accuracy: timeUnknown ? 'unknown' : 'exact',
      })
      router.replace(`/onboarding/computing?profile=${profile.id}`)
    } catch (err) {
      setSaving(false)
      setError(
        err instanceof AuthError && err.status === 401
          ? 'Your session expired. Please sign in again.'
          : t.common.somethingWentWrong,
      )
    }
  }

  return (
    <main id="main" className="flex min-h-dvh flex-col items-center px-6 py-12">
      <Wordmark />

      <div className="mt-8 w-full max-w-sm">
        <StepHeader current={step} total={TOTAL_STEPS} onBack={goBack} label={t.birth.stepOf} />

        {step === 1 && (
          <form onSubmit={handleDateStep} className="mt-6" noValidate>
            <h1 className="text-balance font-serif text-3xl tracking-tight">
              {t.birth.dateTitle}
            </h1>
            <p className="mt-3 text-sm text-ink-muted">{t.birth.dateSubtitle}</p>

            <div className="mt-8 grid grid-cols-[1fr_1fr_1.4fr] gap-3">
              <NumberField
                id="day"
                label={t.birth.dayLabel}
                value={day}
                onChange={setDay}
                max={2}
                placeholder="17"
                invalid={Boolean(error)}
                clearError={() => setError(null)}
              />
              <NumberField
                id="month"
                label={t.birth.monthLabel}
                value={month}
                onChange={setMonth}
                max={2}
                placeholder="08"
                invalid={Boolean(error)}
                clearError={() => setError(null)}
              />
              <NumberField
                id="year"
                label={t.birth.yearLabel}
                value={year}
                onChange={setYear}
                max={4}
                placeholder="1994"
                invalid={Boolean(error)}
                clearError={() => setError(null)}
              />
            </div>

            <ErrorLine id="date-error" message={error} />

            <Button type="submit" size="lg" className="mt-8 w-full">
              {t.common.continue}
            </Button>
          </form>
        )}

        {step === 2 && (
          <form onSubmit={handleTimeStep} className="mt-6" noValidate>
            <h1 className="text-balance font-serif text-3xl tracking-tight">
              {t.birth.timeTitle}
            </h1>
            <p className="mt-3 text-sm text-ink-muted">{t.birth.timeSubtitle}</p>

            <div
              className={cn(
                'mt-8 grid grid-cols-2 gap-3 transition-opacity',
                timeUnknown && 'pointer-events-none opacity-40',
              )}
            >
              <NumberField
                id="hour"
                label={t.birth.hourLabel}
                value={hour}
                onChange={setHour}
                max={2}
                placeholder="14"
                disabled={timeUnknown}
                invalid={Boolean(error)}
                clearError={() => setError(null)}
              />
              <NumberField
                id="minute"
                label={t.birth.minuteLabel}
                value={minute}
                onChange={setMinute}
                max={2}
                placeholder="35"
                disabled={timeUnknown}
                invalid={Boolean(error)}
                clearError={() => setError(null)}
              />
            </div>

            {!timeUnknown && <ErrorLine id="time-error" message={error} />}

            {/* The escape hatch. Without it, somebody who does not know
                their birth time abandons signup entirely — this converts
                a dead end into a completed account. */}
            <label className="mt-6 flex cursor-pointer items-start gap-3 rounded-lg border border-input p-4 focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2 focus-within:ring-offset-background">
              <input
                type="checkbox"
                checked={timeUnknown}
                onChange={(e) => {
                  setTimeUnknown(e.target.checked)
                  setError(null)
                  if (e.target.checked) {
                    // Its own event in the spec §15, because how many
                    // people do not know their birth time decides
                    // whether the escape hatch was worth building.
                    void trackAsUser('birth_time_unknown_selected', {})
                  }
                }}
                className="mt-0.5 size-4 shrink-0 accent-gold"
              />
              <span className="text-sm">{t.birth.unknownLabel}</span>
            </label>

            {/* Explained, not hidden. Saying what becomes unavailable is
                the difference between an informed choice and a chart
                that is quietly worse than the user thinks. */}
            {timeUnknown && (
              <p
                role="status"
                className="mt-3 rounded-lg bg-surface-2 p-4 text-sm leading-relaxed text-ink-muted"
              >
                {t.birth.unknownExplained}
              </p>
            )}

            <Button type="submit" size="lg" className="mt-8 w-full">
              {t.common.continue}
            </Button>
          </form>
        )}

        {step === 3 && (
          <form onSubmit={handlePlaceStep} className="mt-6" noValidate>
            <h1 className="text-balance font-serif text-3xl tracking-tight">
              {t.birth.placeTitle}
            </h1>
            <p className="mt-3 text-sm text-ink-muted">{t.birth.placeSubtitle}</p>

            <div className="mt-8">
              <PlaceSearch
                selected={place}
                onSelect={(chosen) => {
                  setPlace(chosen)
                  setError(null)
                }}
                disabled={saving}
              />
            </div>

            <ErrorLine id="place-error" message={error} />

            <Button type="submit" size="lg" disabled={saving} className="mt-8 w-full">
              {saving ? t.common.saving : t.birth.submit}
            </Button>
          </form>
        )}
      </div>
    </main>
  )
}

/**
 * A two- or four-digit field.
 *
 * `inputMode="numeric"` rather than `type="number"`: a number input
 * brings spinner arrows nobody wants on a year, silently accepts `e`
 * and `+`, and on some browsers returns an empty string for anything it
 * dislikes — which turns "1994e" into a missing year with no error.
 */
function NumberField({
  id,
  label,
  value,
  onChange,
  max,
  placeholder,
  disabled,
  invalid,
  clearError,
}: {
  id: string
  label: string
  value: string
  onChange: (next: string) => void
  max: number
  placeholder: string
  disabled?: boolean
  invalid?: boolean
  clearError: () => void
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
        placeholder={placeholder}
        value={value}
        onChange={(e) => {
          // Strip anything that is not a digit as it is typed, so the
          // validator never sees a value the user cannot see is wrong.
          onChange(e.target.value.replace(/\D/g, '').slice(0, max))
          clearError()
        }}
        aria-invalid={invalid ? true : undefined}
        className={cn('h-12 text-center text-base tabular-nums', invalid && 'border-danger')}
      />
    </div>
  )
}

function ErrorLine({ id, message }: { id: string; message: string | null }) {
  if (!message) return null
  return (
    <p id={id} role="alert" className="mt-3 text-sm text-danger">
      {message}
    </p>
  )
}
