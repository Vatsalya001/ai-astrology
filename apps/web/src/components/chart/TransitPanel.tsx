'use client'

import { AstroTerm } from '@/components/AstroTerm'
import type { NatalTransits } from '@/lib/astrology-api'
import { useLocale } from '@/lib/i18n/context'
import { cn } from '@/lib/utils'

import { formatDegree } from './format'
import { RETROGRADE_MARK, ordinal, planetAbbreviation } from './glyphs'
import { SadeSatiIndicator } from './SadeSatiIndicator'

/**
 * Where the planets are right now, relative to this person's natal Moon.
 *
 * ── Houses from the Moon, not from the ascendant ──
 *
 * Gochara — transit reading — counts from the Moon's sign by convention,
 * and api-service computes `house_from_moon` per request rather than
 * storing it. That is not an implementation detail worth hiding: the
 * heading says which frame it is, because "Saturn in your 12th" means
 * two different things depending on where you count from, and a reader
 * comparing this screen to a printed chart needs to know which.
 *
 * It is also why this panel works without a birth time. The Moon's sign
 * needs a date, not a clock; the ascendant needs both. A user with no
 * birth time has no houses on the chart screen and still has this one.
 *
 * ── The timestamp is shown ──
 *
 * Transits are refreshed every six hours by a worker. A panel headed
 * "right now in the sky" that is silently four hours stale is a small
 * lie told confidently; the instant it was computed at is one line.
 */
export function TransitPanel({
  data,
  className,
}: {
  data: NatalTransits
  className?: string
}) {
  const { locale, t, fill } = useLocale()

  /*
    The instant the positions were COMPUTED for, not the instant they
    were asked for.

    `data.at` is the query time — api-service defaults it to `now()` —
    while each row carries the six-hourly slot the refresher wrote it
    at. Rendering `data.at` stamped a sky up to six hours old as
    computed this second, and if the worker had been down for three
    days it still said "computed just now" over positions where the Moon
    had moved more than a full sign. The header comment below promised
    exactly the opposite.

    The OLDEST row, not the newest: a partial refresh can leave one
    planet behind, and the panel is only as fresh as its stalest
    position. Taking the newest would overstate it.
  */
  const computedFor = data.transits.reduce<number | null>((oldest, position) => {
    const at = Date.parse(position.timestamp)
    if (!Number.isFinite(at)) return oldest
    return oldest === null || at < oldest ? at : oldest
  }, null)

  const computedAt =
    computedFor === null
      ? null
      : new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(
          new Date(computedFor),
        )

  return (
    <div className={cn('space-y-6', className)}>
      <div>
        <h2 className="font-serif text-lg">{t.chart.transitsTitle}</h2>
        <p className="mt-1 text-xs text-ink-muted">
          {fill(t.chart.transitsFrame, { sign: data.natal_moon_sign })}{' '}
          <AstroTerm term="gochara">gochara</AstroTerm>.{' '}
          {computedAt !== null && fill(t.chart.transitsComputed, { when: computedAt })}
        </p>
      </div>

      {data.transits.length === 0 ? (
        <p className="rounded-lg border border-border p-6 text-sm text-ink-muted">
          {t.chart.transitsEmpty}
        </p>
      ) : (
        /* Named, because the Sade Sati indicator below is also a list
           and a screen reader moving by landmark otherwise meets two
           unlabelled ones. */
        <ul aria-label={t.chart.transitsList} className="divide-y divide-border">
          {data.transits.map((position) => (
            <li
              key={position.planet}
              className="flex items-baseline gap-3 py-3 text-sm"
              /*
                One label for the whole row. Read cell by cell it comes
                out as "Saturn, Pisces, 12 degrees 30, 12th" — four
                fragments that are not a sentence.
              */
              aria-label={[
                fill(t.chart.transitRowLabel, {
                  planet: position.planet,
                  sign: position.sign,
                  ordinal: ordinal(position.house_from_moon),
                }),
                position.is_retrograde ? 'retrograde' : null,
              ]
                .filter(Boolean)
                .join(', ')}
            >
              <span aria-hidden="true" className="w-7 shrink-0 text-ink-muted">
                {planetAbbreviation(position.planet)}
              </span>

              <span className="min-w-0 flex-1">
                <span className="font-medium text-ink">{position.planet}</span>
                <span className="text-ink-muted">
                  {' '}
                  in {position.sign} {formatDegree(position.degree)}
                </span>
                {position.is_retrograde && (
                  <>
                    {' '}
                    <span aria-hidden="true" className="text-gold">
                      {RETROGRADE_MARK}
                    </span>
                    <span className="sr-only">retrograde</span>
                  </>
                )}
              </span>

              <span className="shrink-0 text-xs text-ink-muted">
                {fill(t.chart.transitsFromMoon, {
                  ordinal: ordinal(position.house_from_moon),
                })}
              </span>
            </li>
          ))}
        </ul>
      )}

      <SadeSatiIndicator status={data.sade_sati} at={data.at} />
    </div>
  )
}
