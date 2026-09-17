'use client'

import { useMemo, useState } from 'react'

import type { GlossaryKey } from '@ayana/content'

import { AstroTerm } from '@/components/AstroTerm'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { useLocale } from '@/lib/i18n/context'
import { cn } from '@/lib/utils'

import {
  elapsed,
  formatSpan,
  markerAt,
  place,
  spanAt,
  toSpan,
  yearOffset,
  yearTicks,
  type Placed,
  type Span,
} from './timeline'

/**
 * The dasha timeline.
 *
 * The spec calls this the single most compelling non-AI screen in the
 * product, and the reason is narrow: people care intensely about which
 * period they are in. Everything here serves that one question.
 *
 * ── Drill-down IS the zoom ──
 *
 * The spec asks for decade ↔ year ↔ month zooming. Rather than a zoom
 * control, selecting a period narrows the track to its children, which
 * lands on those three scales by construction: the mahadasha cycle spans
 * 120 years, one mahadasha spans about sixteen, and one antardasha spans
 * months. It is also the interaction people already expect from a
 * timeline, and it needs no explanation.
 *
 * ── "You are here" comes from the server ──
 *
 * `currentAt` is the server's instant, not `Date.now()`. A browser clock
 * can be wrong by years — a phone with the date reset is common — and a
 * marker derived from it is a confident wrong answer to the exact thing
 * this screen exists for.
 */
export interface TimelineLevel {
  /** Periods at this level, already filtered to the selected parent. */
  periods: Array<{ id: string; planet: string; start: string; end: string }>
  loading: boolean
}

export function DashaTimeline({
  levels,
  currentAt,
  onDrillDown,
  className,
}: {
  /** Index 0 is mahadashas, 1 antardashas, 2 pratyantardashas. */
  levels: TimelineLevel[]
  /** The server's instant, as epoch milliseconds. */
  currentAt: number
  /** Called with the period whose children should be fetched next. */
  onDrillDown: (level: number, periodId: string | null) => void
  className?: string
}) {
  const [open, setOpen] = useState<{ span: Span; level: number } | null>(null)

  return (
    <div className={cn('space-y-8', className)}>
      {levels.map((level, index) => (
        <Track
          key={index}
          level={index}
          periods={level.periods}
          loading={level.loading}
          currentAt={currentAt}
          onSelect={(span, id) => {
            setOpen({ span, level: index })
            // The last level has no children to fetch.
            if (index < 2) onDrillDown(index + 1, id)
          }}
        />
      ))}

      <PeriodSheet entry={open} currentAt={currentAt} onClose={() => setOpen(null)} />
    </div>
  )
}

/**
 * The three levels, named and defined.
 *
 * One table rather than two parallel arrays: a name and a term that can
 * drift apart by one index is how the third house ended up showing the
 * fourth house's meaning two PRs ago.
 *
 * `term` is `GlossaryKey`, not `string`. It reaches `AstroTerm` as a
 * computed prop, which the source scan in `astro-term-usage.test.ts`
 * cannot follow, so the type carries the guarantee and
 * `DashaTimeline.test.tsx` asserts each one resolves.
 */
export const LEVELS: ReadonlyArray<{ name: string; term: GlossaryKey }> = [
  { name: 'Mahadasha', term: 'mahadasha' },
  { name: 'Antardasha', term: 'antardasha' },
  { name: 'Pratyantardasha', term: 'pratyantardasha' },
] as const

// Falls back to the first level rather than rendering `undefined`. The
// callers all pass an array index, so this is unreachable today — which
// is a statement about today's callers, not about the type.
function levelInfo(level: number) {
  return LEVELS[level] ?? LEVELS[0]!
}

function Track({
  level,
  periods,
  loading,
  currentAt,
  onSelect,
}: {
  level: number
  periods: Array<{ id: string; planet: string; start: string; end: string }>
  loading: boolean
  currentAt: number
  onSelect: (span: Span, id: string) => void
}) {
  const { locale, t, fill } = useLocale()

  const { placed, ids, ticks, marker } = useMemo(() => {
    const spans: Span[] = []
    const ids: string[] = []

    for (const period of periods) {
      const span = toSpan(period)
      // A period with unusable dates is dropped, not drawn at zero
      // width. `toSpan` returns null rather than NaN precisely so this
      // decision is visible here instead of silently producing an
      // element the browser refuses to lay out.
      if (!span) continue
      spans.push(span)
      ids.push(period.id)
    }

    return {
      placed: place(spans),
      ids,
      ticks: yearTicks(spans),
      marker: markerAt(spans, currentAt),
      spans,
    }
  }, [periods, currentAt])

  if (loading) {
    return (
      <div aria-busy="true" aria-live="polite">
        <p className="mb-2 text-xs uppercase tracking-wide text-ink-muted">
          {levelInfo(level).name}
        </p>
        <div className="h-10 w-full animate-pulse rounded-md bg-elevated" />
      </div>
    )
  }

  if (placed.length === 0) {
    // Only reachable for levels 2 and 3 before a parent is chosen.
    if (level === 0) return null
    return (
      <div>
        <p className="mb-2 text-xs uppercase tracking-wide text-ink-muted">
          {levelInfo(level).name}
        </p>
        <p className="text-sm text-ink-muted">
          {fill(t.chart.dashaChooseParent, {
            parent: levelInfo(level - 1).name.toLowerCase(),
          })}
        </p>
      </div>
    )
  }

  const current = spanAt(placed, currentAt)

  return (
    <section aria-label={fill(t.chart.dashaPeriodsOf, { level: levelInfo(level).name })}>
      <p className="mb-2 text-xs uppercase tracking-wide text-ink-muted">
        <AstroTerm term={levelInfo(level).term}>{levelInfo(level).name}</AstroTerm>
      </p>

      <div className="relative">
        {/*
          A list, not a row of divs. Nine bars of different widths are
          nine items with an order and a name; a screen reader gets that
          for free from list semantics and gets nothing from a flex box.
        */}
        <ul className="relative flex h-11 w-full overflow-hidden rounded-md border border-border">
          {placed.map((span, i) => (
            <li
              key={ids[i]}
              style={{ width: `${span.width * 100}%` }}
              className="min-w-0 border-r border-border/60 last:border-r-0"
            >
              <button
                type="button"
                onClick={() => onSelect(span, ids[i]!)}
                className={cn(
                  'flex h-full w-full items-center justify-center overflow-hidden px-1',
                  'text-xs transition-colors hover:bg-elevated',
                  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                  'focus-visible:ring-inset',
                  span === current ? 'bg-gold/20 font-medium text-ink' : 'text-ink-muted',
                )}
                aria-label={[
                  `${span.planet} ${levelInfo(level).name.toLowerCase()}`,
                  formatSpan(span, locale),
                  span === current ? t.chart.dashaCurrent : null,
                ]
                  .filter(Boolean)
                  .join(', ')}
                aria-current={span === current ? 'true' : undefined}
              >
                <span className="truncate">{span.planet}</span>
              </button>
            </li>
          ))}
        </ul>

        {/*
          The marker, drawn over the track.

          `aria-hidden`: the same fact is on the current period's button
          as `aria-current` and in its label, and a floating "you are
          here" with no position a screen reader can interpret is noise.
          Null when today falls outside the range, rather than pinned to
          an edge — see `markerAt`.
        */}
        {marker !== null && (
          <div
            aria-hidden="true"
            className="pointer-events-none absolute inset-y-0 w-px bg-gold"
            style={{ left: `${marker * 100}%` }}
          >
            <span className="absolute -top-1 left-1/2 h-2 w-2 -translate-x-1/2 rounded-full bg-gold" />
          </div>
        )}
      </div>

      {/* Year labels. Decorative — every date is already in a button label. */}
      <div aria-hidden="true" className="relative mt-1 h-4">
        {ticks.map((year) => {
          const offset = yearOffset(
            placed.map((p) => ({ planet: p.planet, start: p.start, end: p.end })),
            year,
          )
          if (offset === null) return null
          return (
            <span
              key={year}
              className="absolute -translate-x-1/2 text-[10px] tabular-nums text-ink-muted"
              style={{ left: `${offset * 100}%` }}
            >
              {year}
            </span>
          )
        })}
      </div>
    </section>
  )
}

function PeriodSheet({
  entry,
  currentAt,
  onClose,
}: {
  entry: { span: Span; level: number } | null
  currentAt: number
  onClose: () => void
}) {
  const { locale, t, fill } = useLocale()
  if (!entry) return null

  const { span, level } = entry
  const isCurrent = currentAt >= span.start && currentAt < span.end
  const progress = elapsed(span, currentAt)

  return (
    <Dialog open onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>
            {span.planet} {levelInfo(level).name}
          </DialogTitle>
          <DialogDescription>{formatSpan(span, locale)}</DialogDescription>
        </DialogHeader>

        {isCurrent && (
          <div>
            <div className="flex items-center justify-between text-xs text-ink-muted">
              <span>{t.chart.dashaCurrent}</span>
              <span className="tabular-nums">
                {fill(t.home.periodElapsed, { percent: Math.round(progress * 100) })}
              </span>
            </div>
            {/*
              A real progress bar with a value, not a styled div. The
              percentage is also written out beside it, because a bar
              alone tells a screen reader nothing and tells a colour-blind
              reader only slightly more.
            */}
            <div
              role="progressbar"
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={Math.round(progress * 100)}
              aria-label={`${span.planet} period elapsed`}
              className="mt-1 h-2 w-full overflow-hidden rounded-full bg-elevated"
            >
              <div className="h-full bg-gold" style={{ width: `${progress * 100}%` }} />
            </div>
          </div>
        )}

        <p className="text-sm text-ink-muted">
          <AstroTerm term={levelInfo(level).term} />
        </p>
      </DialogContent>
    </Dialog>
  )
}

export type { Placed }
