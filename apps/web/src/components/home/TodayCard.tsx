'use client'

import { moonDay } from '@ayana/content'

import { AstroTerm } from '@/components/AstroTerm'
import { useLocale } from '@/lib/i18n/context'
import { cn } from '@/lib/utils'

/**
 * The day, from the transiting Moon.
 *
 * ── What the spec asks for and what the engine gives ──
 *
 * The mock reads "Moon in Rohini · Taurus" plus a static line — sign,
 * nakshatra, and elsewhere a tithi. `GET /astrology/transits` returns
 * the transiting Moon's sign, degree and longitude and nothing else.
 *
 * Deriving the nakshatra from the longitude is four lines of arithmetic
 * and it is exactly the four lines this project forbids: astrology is
 * computed by `astro-service` or it is not shown. Tithi is not computed
 * anywhere in the engine at all. So this card shows the sign and the
 * written line, and neither of the other two — both are gaps to close
 * in Python, not to paper over in TypeScript.
 *
 * ── The line is about the sky, not about the reader ──
 *
 * The transiting Moon is in one sign for everyone alive today, so this
 * is a general note and the copy says so. A personalised daily reading
 * needs the natal chart and arrives in Phase 6.
 */
export function TodayCard({
  moonSign,
  className,
}: {
  /** The transiting Moon's sign, or null when transits are unavailable. */
  moonSign: string | null
  className?: string
}) {
  const { locale } = useLocale()
  const day = moonSign ? moonDay(moonSign, locale) : null

  return (
    <section
      aria-labelledby="today-heading"
      className={cn('rounded-lg border border-border p-4', className)}
    >
      <h2
        id="today-heading"
        className="text-xs uppercase tracking-wide text-ink-muted"
      >
        Today
      </h2>

      {moonSign === null ? (
        <p className="mt-2 text-sm text-ink-muted">
          Today&rsquo;s sky is still being prepared. Positions are computed every six hours.
        </p>
      ) : (
        <>
          <p className="mt-2 font-serif text-lg text-ink">
            <AstroTerm term="gochara">Moon</AstroTerm> in {moonSign}
          </p>

          {day ? (
            <p className="mt-1 text-sm leading-relaxed text-ink-muted">{day.short}</p>
          ) : (
            /*
              A sign name the corpus has no line for. `moon-days.test.ts`
              compares the twelve keys against the engine's own SIGNS
              tuple, so this should be unreachable — and it renders as an
              absence rather than as a composed sentence, because
              composing one here would be the frontend writing astrology.
            */
            <p className="mt-1 text-sm text-ink-muted">
              No note written for this sign yet.
            </p>
          )}
        </>
      )}
    </section>
  )
}
