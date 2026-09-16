'use client'

import type { GlossaryKey } from '@ayana/content'

import { AstroTerm } from '@/components/AstroTerm'
import type { SadeSatiStatus } from '@/lib/astrology-api'
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
export const PHASES: ReadonlyArray<{ key: string; label: string; term: GlossaryKey }> = [
  { key: 'rising', label: 'Rising', term: 'sade_sati_rising' },
  { key: 'peak', label: 'Peak', term: 'sade_sati_peak' },
  { key: 'setting', label: 'Setting', term: 'sade_sati_setting' },
] as const

export function SadeSatiIndicator({
  status,
  className,
}: {
  status: SadeSatiStatus
  className?: string
}) {
  if (!status.is_active) {
    return (
      <div className={cn('rounded-lg border border-border p-4', className)}>
        <h3 className="text-sm font-medium">
          <AstroTerm term="sade_sati">Sade Sati</AstroTerm>
        </h3>
        <p className="mt-1 text-sm text-ink-muted">
          Not currently running. Saturn is in {status.saturn_sign}, the{' '}
          {ordinal(status.houses_from_moon)} sign from your Moon.
        </p>
      </div>
    )
  }

  const activeIndex = PHASES.findIndex((p) => p.key === status.phase)

  return (
    <div className={cn('rounded-lg border border-gold/35 bg-gold/5 p-4', className)}>
      <h3 className="text-sm font-medium">
        <AstroTerm term="sade_sati">Sade Sati</AstroTerm>
      </h3>

      <p className="mt-1 text-sm text-ink-muted">
        Saturn is in {status.saturn_sign}, the {ordinal(status.houses_from_moon)} sign from
        your Moon.
      </p>

      {/*
        The three phases as an ordered list with the current one named.

        `aria-current` and a filled dot, not colour alone — and the phase
        name is written beside the dots rather than only encoded by
        position, so the state survives greyscale, colour blindness and a
        screen reader equally.
      */}
      <ol className="mt-3 flex items-center gap-1" aria-label="Sade Sati phase">
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
                {phase.label}
                {active && <span className="sr-only"> — current phase</span>}
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
          The phase was not reported for this reading.
        </p>
      )}
    </div>
  )
}
