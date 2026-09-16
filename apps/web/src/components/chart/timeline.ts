/**
 * Date → pixel, for the dasha timeline.
 *
 * Pure, and separate from the component for the same reason `format.ts`
 * is: the bugs here are arithmetic, and a rendering test buries them.
 *
 * ── Everything is epoch milliseconds ──
 *
 * The API sends RFC 3339 with an offset. Working in `Date` objects and
 * comparing them with `<` happens to work, but the moment anything
 * touches `getFullYear()` it is reading the BROWSER's timezone, and a
 * Vimshottari period boundary computed in Kolkata renders on the wrong
 * side of a year label for a user in Los Angeles. Milliseconds since the
 * epoch are the same number everywhere; the only place a timezone is
 * allowed in is the final label, where it is the user's own and correct.
 */

export interface Span {
  planet: string
  /** Epoch milliseconds. */
  start: number
  end: number
}

export interface Placed extends Span {
  /** Fraction of the track, 0..1. */
  offset: number
  width: number
}

/** A parsed period, or null if the dates are unusable. */
export function toSpan(period: { planet: string; start: string; end: string }): Span | null {
  const start = Date.parse(period.start)
  const end = Date.parse(period.end)

  // `Date.parse` returns NaN for anything it cannot read, and NaN
  // propagates silently through every arithmetic below into a style
  // attribute of `width: NaN%`, which browsers drop. The result is a
  // period that is simply not drawn, on a timeline that still looks
  // complete.
  if (!Number.isFinite(start) || !Number.isFinite(end)) return null

  // A period that ends before it starts is not a rendering problem, it
  // is corrupt data — and a negative width reads as a gap.
  if (end <= start) return null

  return { planet: period.planet, start, end }
}

/**
 * Lay spans out proportionally across a track.
 *
 * Proportional, not equal-width. A Vimshottari cycle runs Ketu 7 years,
 * Venus 20, Sun 6, Moon 10, Mars 7, Rahu 18, Jupiter 16, Saturn 19,
 * Mercury 17 — nearly a 3.5:1 spread. Equal columns would be easier to
 * read and would be a lie about the thing the screen exists to show,
 * which is how long you are in each one.
 */
export function place(spans: Span[]): Placed[] {
  if (spans.length === 0) return []

  const from = Math.min(...spans.map((s) => s.start))
  const to = Math.max(...spans.map((s) => s.end))
  const total = to - from

  // Every span at the same instant. Not reachable from real dasha data,
  // but dividing by it would make every offset NaN and blank the track.
  if (total <= 0) {
    return spans.map((span) => ({ ...span, offset: 0, width: 1 / spans.length }))
  }

  return spans.map((span) => ({
    ...span,
    offset: (span.start - from) / total,
    width: (span.end - span.start) / total,
  }))
}

/**
 * Where "you are here" goes, as a fraction of the track.
 *
 * Returns null outside the range rather than clamping to an edge. A
 * marker pinned to 0 for a date before the chart begins says "you are at
 * the start of your first mahadasha", which is a specific and wrong
 * claim; no marker says nothing, which is correct.
 */
export function markerAt(spans: Span[], at: number): number | null {
  if (spans.length === 0 || !Number.isFinite(at)) return null

  const from = Math.min(...spans.map((s) => s.start))
  const to = Math.max(...spans.map((s) => s.end))
  if (at < from || at > to) return null
  if (to === from) return 0

  return (at - from) / (to - from)
}

/**
 * How far through a period a moment is, 0..1.
 *
 * Used for the progress bar inside the current period. Clamped, because
 * this one IS asked about a specific period the caller already knows
 * contains the moment, and a rounding error at the boundary should read
 * as "just finished" rather than as 1.0000001.
 */
export function elapsed(span: Span, at: number): number {
  const total = span.end - span.start
  if (total <= 0) return 0
  return Math.min(1, Math.max(0, (at - span.start) / total))
}

/** Which span contains a moment, or null. */
export function spanAt<T extends Span>(spans: T[], at: number): T | null {
  // Half-open: a boundary instant belongs to the period it starts, the
  // same convention the engine uses for nakshatra and dasamsa
  // boundaries. Closed at both ends would make two periods "current" for
  // one millisecond every fifteen years or so, and whichever came first
  // in the array would silently win.
  return spans.find((span) => at >= span.start && at < span.end) ?? null
}

/**
 * The years to write under the track.
 *
 * Chosen by how much time is on screen rather than fixed, so a
 * seven-year antardasha is not labelled every decade and a 120-year
 * cycle is not labelled 120 times. The steps are the ones a reader
 * expects to see on an axis; 3 and 7 are deliberately absent.
 */
export function yearTicks(spans: Span[], maxLabels = 8): number[] {
  if (spans.length === 0) return []

  const from = new Date(Math.min(...spans.map((s) => s.start)))
  const to = new Date(Math.max(...spans.map((s) => s.end)))

  const firstYear = from.getUTCFullYear()
  const lastYear = to.getUTCFullYear()
  const years = lastYear - firstYear

  // `maxLabels - 1`, because `years / s` counts INTERVALS and the labels
  // are the fenceposts: 2000 to 2016 at a step of 2 is 8 intervals and
  // nine labels. Written without the `- 1` first, and 16 years produced
  // exactly one more label than asked for.
  const step = [1, 2, 5, 10, 20, 25, 50, 100].find((s) => years / s <= maxLabels - 1) ?? 100

  const ticks: number[] = []
  // Start at a round multiple of the step, so the labels read 1980,
  // 1990, 2000 rather than 1983, 1993, 2003.
  for (let year = Math.ceil(firstYear / step) * step; year <= lastYear; year += step) {
    ticks.push(year)
  }
  return ticks
}

/** Where a year label sits on the track, 0..1, or null if off it. */
export function yearOffset(spans: Span[], year: number): number | null {
  return markerAt(spans, Date.UTC(year, 0, 1))
}

/**
 * `Mar 2019 – Mar 2035`.
 *
 * Month and year only. A dasha boundary is a moment computed to the
 * second, and printing it to the second implies the birth time was known
 * to the second — which it never is. The precision shown should not
 * exceed the precision of the input.
 */
export function formatSpan(span: Span, locale: string): string {
  return `${formatMonth(span.start, locale)} – ${formatMonth(span.end, locale)}`
}

export function formatMonth(ms: number, locale: string): string {
  return new Intl.DateTimeFormat(locale, { month: 'short', year: 'numeric' }).format(new Date(ms))
}
