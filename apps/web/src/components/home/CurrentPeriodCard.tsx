'use client'

import Link from 'next/link'

import { AstroTerm } from '@/components/AstroTerm'
import type { CurrentDashas } from '@/lib/astrology-api'
import { useLocale } from '@/lib/i18n/context'
import { cn } from '@/lib/utils'

import { elapsed, formatMonth, toSpan } from '../chart/timeline'

/**
 * "You are here" on the dashboard.
 *
 * ── Why this takes the server's answer whole ──
 *
 * `GET /charts/{id}/dashas/current` returns the maha, antar and
 * pratyantar periods containing a given instant, and the instant is the
 * SERVER's. Finding the current period by searching the fetched tree in
 * the browser would be one `filter` and would be wrong on any device
 * whose clock is off — which for a phone with its date reset is off by
 * years, and puts the reader in the wrong sixteen-year period on the
 * card they came to the app for.
 *
 * The progress bar is computed here, from those dates and that instant.
 * That is arithmetic on values the engine supplied, not astrology.
 */
export function CurrentPeriodCard({
  current,
  className,
}: {
  current: CurrentDashas
  className?: string
}) {
  const { locale, t, fill } = useLocale()

  const maha = current.mahadasha
  const antar = current.antardasha
  const at = Date.parse(current.at)

  /*
    No mahadasha means no dasha tree, which means no birth time — the
    Vimshottari sequence starts from the Moon's nakshatra pada and that
    needs a clock. Saying so beats an empty card, and it names something
    the reader can actually fix.
  */
  if (!maha) {
    return (
      <section
        aria-labelledby="period-heading"
        className={cn('rounded-lg border border-border p-4', className)}
      >
        <h2 id="period-heading" className="text-xs uppercase tracking-wide text-ink-muted">
          {t.home.periodLabel}
        </h2>
        <p className="mt-2 text-sm text-ink-muted">{t.home.periodNoBirthTime}</p>
      </section>
    )
  }

  const span = toSpan(maha)
  const progress = span ? elapsed(span, at) : 0

  return (
    <section
      aria-labelledby="period-heading"
      className={cn('rounded-lg border border-border p-4', className)}
    >
      <h2 id="period-heading" className="text-xs uppercase tracking-wide text-ink-muted">
        {t.home.periodLabel}
      </h2>

      <p className="mt-2 font-serif text-lg text-ink">
        {maha.planet} <AstroTerm term="mahadasha">Mahadasha</AstroTerm>
      </p>

      {span && (
        <>
          <div className="mt-2 flex items-center justify-between text-xs tabular-nums text-ink-muted">
            <span>{formatMonth(span.start, locale)}</span>
            <span>{formatMonth(span.end, locale)}</span>
          </div>

          {/*
            A real progressbar with a value, and the percentage written
            out beside the dates. A bar alone tells a screen reader
            nothing and a colour-blind reader very little.
          */}
          <div
            role="progressbar"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={Math.round(progress * 100)}
            aria-label={fill(t.home.periodElapsedLabel, { planet: maha.planet })}
            className="mt-1 h-2 w-full overflow-hidden rounded-full bg-elevated"
          >
            <div className="h-full bg-gold" style={{ width: `${progress * 100}%` }} />
          </div>
          <p className="mt-1 text-xs text-ink-muted">
            {fill(t.home.periodElapsed, { percent: Math.round(progress * 100) })}
          </p>
        </>
      )}

      {antar && (
        <p className="mt-3 text-sm text-ink-muted">
          <AstroTerm term="antardasha">
            {antarLine(antar, locale, t.home.periodAntardasha, t.home.periodAntardashaTo, fill)}
          </AstroTerm>
        </p>
      )}

      <Link
        href="/kundli/dashas"
        className={cn(
          'mt-3 inline-block text-sm text-primary underline-offset-4 hover:underline',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
          'focus-visible:ring-offset-2 focus-visible:ring-offset-background rounded-sm',
        )}
      >
        {t.home.periodSeeAll}
      </Link>
    </section>
  )
}

/**
 * `Antardasha: Saturn (to Nov 2026)` — or without the date.
 *
 * Two templates rather than one with an optional tail, because the end
 * date sits in a different place in Hindi and a concatenated
 * " (to Nov 2026)" would land in the wrong half of the sentence.
 */
function antarLine(
  antar: { planet: string; start: string; end: string },
  locale: string,
  withoutEnd: string,
  withEnd: string,
  fill: (template: string, values: Record<string, string | number>) => string,
): string {
  const span = toSpan(antar)
  if (!span) return fill(withoutEnd, { planet: antar.planet })
  return fill(withEnd, { planet: antar.planet, end: formatMonth(span.end, locale) })
}
