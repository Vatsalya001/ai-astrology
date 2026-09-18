'use client'

import type { GlossaryKey } from '@ayana/content'

import { AstroTerm } from '@/components/AstroTerm'
import type { SadeSatiStatus } from '@/lib/astrology-api'
import { useLocale } from '@/lib/i18n/context'
import { cn } from '@/lib/utils'

import { ordinal } from './glyphs'

/**
 * Saturn's seven-and-a-half-year passage over the natal Moon.
 *
 * The single most-asked and most-dreaded thing in Indian astrology, which
 * makes it the place where this product's framing rules matter most. The
 * copy here says what the tradition reads, never what will happen, and
 * never that it is bad — an indicator that tells somebody they are in for
 * seven bad years is the exact failure the AI rules exist to prevent, and
 * it does not become acceptable for being static text rather than model
 * output.
 *
 * ── What is deliberately missing ──
 *
 * Dates. The spec's mock shows "Started Jan 2026 · ends Apr 2033", and
 * the engine does not report a span — only the phase and the geometry.
 * Deriving one here from the phase would be the FRONTEND computing
 * astrology, which is the same rule that keeps the model out of it:
 * astrology is computed by `astro-service` or it is not shown. So the
 * phase is shown and the dates are not, and that is a gap in the engine
 * to close there rather than paper over here.
 */

/**
 * `term` is `GlossaryKey`, so a typo is a compile error.
 *
 * It reaches `AstroTerm` as a computed prop, which the source scan
 * cannot follow — and an unresolved term here renders as plain text with
 * nothing to report it, on the screen that answers the most consequential
 * question the product answers.
 */
export const PHASES: ReadonlyArray<{
  key: string
  /** Dictionary key for the visible label. */
  labelKey: 'sadeSatiRising' | 'sadeSatiPeak' | 'sadeSatiSetting'
  term: GlossaryKey
}> = [
  { key: 'rising', labelKey: 'sadeSatiRising', term: 'sade_sati_rising' },
  { key: 'peak', labelKey: 'sadeSatiPeak', term: 'sade_sati_peak' },
  { key: 'setting', labelKey: 'sadeSatiSetting', term: 'sade_sati_setting' },
] as const

export function SadeSatiIndicator({
  status,
  at,
  className,
}: {
  status: SadeSatiStatus
  /**
   * The server's instant, as an ISO string.
   *
   * "How long is left" is measured from HERE, not from `Date.now()`.
   * Two reasons, and both are project rules rather than preferences:
   * `Date.now()` during render is a hydration mismatch waiting to
   * happen, and a browser clock can be wrong by years — a phone with
   * the date reset is common, and "about 4 years left" derived from one
   * is a confident wrong answer to the exact thing this panel exists
   * for. `DashaTimeline` takes the server's instant for the same
   * reason.
   */
  at: string
  className?: string
}) {
  const { t, fill } = useLocale()

  if (!status.is_active) {
    return (
      <div className={cn('rounded-lg border border-border p-4', className)}>
        <h3 className="text-sm font-medium">
          <AstroTerm term="sade_sati">{t.chart.sadeSatiTitle}</AstroTerm>
        </h3>
        <p className="mt-1 text-sm text-ink-muted">
          {fill(t.chart.sadeSatiInactive, {
            sign: status.saturn_sign,
            ordinal: ordinal(status.houses_from_moon),
          })}
        </p>
      </div>
    )
  }

  const activeIndex = PHASES.findIndex((p) => p.key === status.phase)

  return (
    <div className={cn('rounded-lg border border-gold/35 bg-gold/5 p-4', className)}>
      <h3 className="text-sm font-medium">
        <AstroTerm term="sade_sati">{t.chart.sadeSatiTitle}</AstroTerm>
      </h3>

      <p className="mt-1 text-sm text-ink-muted">
        {fill(t.chart.sadeSatiActive, {
          sign: status.saturn_sign,
          ordinal: ordinal(status.houses_from_moon),
        })}
      </p>

      {/*
        The window, which is the question people actually came to ask.

        Both dates or neither: the server sends them as a pair, and a
        start with no end would be a countdown with nothing to count to.
        When the server has no window yet, this says so in words rather
        than rendering nothing — an absent line reads as "there is no
        end", which is the opposite of the truth and the more frightening
        reading of the two.
      */}
      <Window startedAt={status.started_at} endsAt={status.ends_at} at={at} />

      {/*
        The three phases as an ordered list with the current one named.

        `aria-current` and a filled dot, not colour alone — and the phase
        name is written beside the dots rather than only encoded by
        position, so the state survives greyscale, colour blindness and a
        screen reader equally.
      */}
      <ol className="mt-3 flex items-center gap-1" aria-label={t.chart.sadeSatiPhaseLabel}>
        {PHASES.map((phase, i) => {
          const active = i === activeIndex
          const passed = activeIndex >= 0 && i < activeIndex
          return (
            <li key={phase.key} className="flex flex-1 items-center gap-1">
              <span
                aria-hidden="true"
                className={cn(
                  'h-2.5 w-2.5 shrink-0 rounded-full border',
                  active && 'border-gold bg-gold',
                  passed && 'border-gold/50 bg-gold/40',
                  !active && !passed && 'border-input bg-transparent',
                )}
              />
              <span
                aria-current={active ? 'step' : undefined}
                className={cn('truncate text-xs', active ? 'text-ink' : 'text-ink-muted')}
              >
                {t.chart[phase.labelKey]}
                {active && <span className="sr-only">{t.chart.sadeSatiCurrentPhase}</span>}
              </span>
            </li>
          )
        })}
      </ol>

      {activeIndex >= 0 && (
        <p className="mt-3 text-xs leading-relaxed text-ink-muted">
          <AstroTerm term={PHASES[activeIndex]!.term} />
        </p>
      )}

      {/*
        An active Sade Sati with a phase the engine did not name. Saying
        "rising" as a default would be inventing the answer to the most
        consequential question this screen reports.
      */}
      {activeIndex < 0 && (
        <p className="mt-3 text-xs leading-relaxed text-ink-muted">
          {t.chart.sadeSatiNoPhase}
        </p>
      )}
    </div>
  )
}

/**
 * The start and end of the stretch, plus how long is left.
 *
 * ── The remaining time is derived, and that is allowed ──
 *
 * The first invariant says an LLM never computes astrology, and
 * `astrology-api.ts` notes that deriving "ends April 2033" in TypeScript
 * would be the frontend computing astrology. Both are about the
 * ASTROLOGY: which sign, which house, which boundary date. Those come
 * from the engine and are rendered here unchanged.
 *
 * "About 3 years left" is arithmetic on a date the engine supplied,
 * against the SERVER's instant. It is the same class of thing as
 * formatting a timestamp.
 *
 * Not against `Date.now()`: that is an impure call during render, which
 * this project forbids for hydration reasons, and a browser clock can be
 * wrong by years — which would make this panel confidently wrong about
 * the one number a reader came for.
 */
function Window({
  startedAt,
  endsAt,
  at,
}: {
  startedAt: string | null
  endsAt: string | null
  at: string
}) {
  const { t, fill, locale } = useLocale()

  if (!startedAt || !endsAt) {
    return <p className="mt-2 text-xs text-ink-muted">{t.chart.sadeSatiDatesUnknown}</p>
  }

  const start = new Date(startedAt)
  const end = new Date(endsAt)
  if (Number.isNaN(start.getTime()) || Number.isNaN(end.getTime())) {
    return <p className="mt-2 text-xs text-ink-muted">{t.chart.sadeSatiDatesUnknown}</p>
  }

  const now = new Date(at).getTime()
  if (Number.isNaN(now)) {
    // No trustworthy instant to measure from. The dates themselves are
    // still worth showing; the countdown is not worth guessing at.
    return (
      <p className="mt-2 text-xs tabular-nums text-ink-muted">
        {fill(t.chart.sadeSatiRuns, {
          start: formatMonth(start, locale),
          end: formatMonth(end, locale),
        })}
      </p>
    )
  }

  const remainingYears = (end.getTime() - now) / (365.25 * 24 * 60 * 60 * 1000)

  return (
    <div className="mt-2 space-y-0.5">
      <p className="text-xs tabular-nums text-ink-muted">
        {fill(t.chart.sadeSatiRuns, {
          start: formatMonth(start, locale),
          end: formatMonth(end, locale),
        })}
      </p>
      <p className="text-xs text-ink">
        {remainingYears < 1
          ? t.chart.sadeSatiEndsSoon
          : fill(t.chart.sadeSatiEndsIn, { years: Math.round(remainingYears) })}
      </p>
    </div>
  )
}

/**
 * Month and year, in the reader's language.
 *
 * Not the day. The engine finds these boundaries by bisecting Saturn's
 * motion and they are accurate to about a second — but Saturn hovers
 * near a sign boundary for weeks, and printing "14 March 2027" invites a
 * precision the underlying phenomenon does not have. A month is the
 * honest unit for a seven-and-a-half-year period.
 */
function formatMonth(date: Date, locale: string): string {
  return new Intl.DateTimeFormat(locale === 'hi' ? 'hi-IN' : 'en-IN', {
    month: 'long',
    year: 'numeric',
    timeZone: 'UTC',
  }).format(date)
}
