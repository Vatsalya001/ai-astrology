import { describe, expect, it } from 'vitest'

import {
  elapsed,
  formatMonth,
  formatSpan,
  markerAt,
  place,
  spanAt,
  toSpan,
  yearOffset,
  yearTicks,
  type Span,
} from './timeline'

/**
 * The real Vimshottari proportions, because the spread is the point.
 *
 * Ketu 7, Venus 20, Sun 6, Moon 10, Mars 7, Rahu 18, Jupiter 16,
 * Saturn 19, Mercury 17 — 120 years, and nearly 3.5:1 between the
 * longest and the shortest. A layout bug that makes them equal looks
 * tidier than the truth.
 */
const VIMSHOTTARI: Array<[string, number]> = [
  ['Ketu', 7],
  ['Venus', 20],
  ['Sun', 6],
  ['Moon', 10],
  ['Mars', 7],
  ['Rahu', 18],
  ['Jupiter', 16],
  ['Saturn', 19],
  ['Mercury', 17],
]

const YEAR = 365.25 * 24 * 60 * 60 * 1000

function cycle(startYear = 1994): Span[] {
  let cursor = Date.UTC(startYear, 0, 1)
  return VIMSHOTTARI.map(([planet, years]) => {
    const span = { planet, start: cursor, end: cursor + years * YEAR }
    cursor = span.end
    return span
  })
}

describe('toSpan', () => {
  it('parses RFC 3339 into epoch milliseconds', () => {
    const span = toSpan({ planet: 'Jupiter', start: '2019-03-01T00:00:00Z', end: '2035-03-01T00:00:00Z' })
    expect(span?.start).toBe(Date.UTC(2019, 2, 1))
    expect(span?.end).toBe(Date.UTC(2035, 2, 1))
  })

  /**
   * `Date.parse` returns NaN for anything it cannot read, and NaN
   * propagates silently: `width: NaN%` is dropped by every browser, so
   * the period is not drawn and the timeline still looks complete. A
   * missing sixteen-year mahadasha with no error anywhere.
   */
  it('refuses an unparseable date instead of producing NaN', () => {
    for (const bad of ['', 'yesterday', 'not-a-date', '2019-13-45']) {
      expect(toSpan({ planet: 'X', start: bad, end: '2035-03-01T00:00:00Z' }), bad).toBeNull()
      expect(toSpan({ planet: 'X', start: '2019-03-01T00:00:00Z', end: bad }), bad).toBeNull()
    }
  })

  it('refuses a period that ends before it starts', () => {
    expect(
      toSpan({ planet: 'X', start: '2035-03-01T00:00:00Z', end: '2019-03-01T00:00:00Z' }),
    ).toBeNull()
  })

  it('refuses a zero-length period', () => {
    const same = '2019-03-01T00:00:00Z'
    expect(toSpan({ planet: 'X', start: same, end: same })).toBeNull()
  })
})

describe('place', () => {
  it('lays the cycle out proportionally, not in equal columns', () => {
    const placed = place(cycle())

    const venus = placed.find((p) => p.planet === 'Venus')!
    const sun = placed.find((p) => p.planet === 'Sun')!

    expect(venus.width).toBeCloseTo(20 / 120, 6)
    expect(sun.width).toBeCloseTo(6 / 120, 6)

    // The spread is the assertion. Equal columns would make this 1.
    expect(venus.width / sun.width).toBeCloseTo(20 / 6, 4)
  })

  it('starts the first period at the origin and ends the last at the edge', () => {
    const placed = place(cycle())
    expect(placed[0]!.offset).toBe(0)

    const last = placed[placed.length - 1]!
    expect(last.offset + last.width).toBeCloseTo(1, 10)
  })

  it('leaves no gaps between consecutive periods', () => {
    const placed = place(cycle())
    for (let i = 1; i < placed.length; i++) {
      expect(placed[i]!.offset, placed[i]!.planet).toBeCloseTo(
        placed[i - 1]!.offset + placed[i - 1]!.width,
        10,
      )
    }
  })

  it('sums to the whole track', () => {
    const total = place(cycle()).reduce((sum, p) => sum + p.width, 0)
    expect(total).toBeCloseTo(1, 10)
  })

  it('returns nothing for nothing', () => {
    expect(place([])).toEqual([])
  })

  // Not reachable from real dasha data, but dividing by a zero range
  // makes every offset NaN, which blanks the whole track.
  it('does not divide by zero when every span is the same instant', () => {
    const at = Date.UTC(2020, 0, 1)
    const placed = place([
      { planet: 'A', start: at, end: at },
      { planet: 'B', start: at, end: at },
    ])
    for (const p of placed) {
      expect(Number.isFinite(p.offset)).toBe(true)
      expect(Number.isFinite(p.width)).toBe(true)
    }
  })
})

describe('markerAt', () => {
  it('places the marker proportionally through the range', () => {
    const spans = cycle(1994)
    const halfway = (spans[0]!.start + spans[spans.length - 1]!.end) / 2
    expect(markerAt(spans, halfway)).toBeCloseTo(0.5, 10)
  })

  /**
   * Null, not 0.
   *
   * A marker clamped to the left edge for a date before the chart begins
   * says "you are at the start of your first mahadasha" — a specific
   * claim, and wrong. Saying nothing is correct.
   */
  it('returns null outside the range rather than clamping to an edge', () => {
    const spans = cycle(1994)
    expect(markerAt(spans, Date.UTC(1980, 0, 1))).toBeNull()
    expect(markerAt(spans, Date.UTC(2200, 0, 1))).toBeNull()
  })

  it('returns null for an unusable instant', () => {
    expect(markerAt(cycle(), Number.NaN)).toBeNull()
  })

  it('returns null when there is nothing to place it on', () => {
    expect(markerAt([], Date.now())).toBeNull()
  })
})

describe('spanAt', () => {
  it('finds the period containing a moment', () => {
    const spans = cycle(1994)
    const inVenus = spans[1]!.start + YEAR
    expect(spanAt(spans, inVenus)?.planet).toBe('Venus')
  })

  /**
   * Half-open, matching the engine's convention for every other
   * boundary. Closed at both ends makes two periods current for one
   * millisecond, and whichever came first in the array silently wins.
   */
  it('gives a boundary instant to the period it starts, not the one it ends', () => {
    const spans = cycle(1994)
    const boundary = spans[0]!.end

    expect(spans[1]!.start).toBe(boundary)
    expect(spanAt(spans, boundary)?.planet).toBe('Venus')
    expect(spanAt(spans, boundary - 1)?.planet).toBe('Ketu')
  })

  it('returns null outside every period', () => {
    expect(spanAt(cycle(1994), Date.UTC(1980, 0, 1))).toBeNull()
  })
})

describe('elapsed', () => {
  it('reports progress through a period', () => {
    const span = { planet: 'Jupiter', start: Date.UTC(2019, 0, 1), end: Date.UTC(2035, 0, 1) }
    expect(elapsed(span, Date.UTC(2027, 0, 1))).toBeCloseTo(0.5, 2)
  })

  // Clamped, unlike markerAt: the caller already knows this period
  // contains the moment, so a rounding error at the edge should read as
  // "just finished", not as 1.0000001 overflowing a progress bar.
  it('clamps rather than overflowing', () => {
    const span = { planet: 'X', start: 0, end: 100 }
    expect(elapsed(span, -50)).toBe(0)
    expect(elapsed(span, 150)).toBe(1)
  })

  it('does not divide by zero', () => {
    expect(elapsed({ planet: 'X', start: 5, end: 5 }, 5)).toBe(0)
  })
})

describe('yearTicks', () => {
  it('labels a 120-year cycle at a readable spacing', () => {
    const ticks = yearTicks(cycle(1994))
    expect(ticks.length).toBeGreaterThan(2)
    expect(ticks.length).toBeLessThanOrEqual(8)
  })

  it('labels round years, not the range start', () => {
    // 1983, 1993, 2003 is what a naive "first year + step" produces.
    const ticks = yearTicks(cycle(1983))
    for (const tick of ticks) {
      expect(tick % (ticks[1]! - ticks[0]!), `${tick}`).toBe(0)
    }
  })

  it('uses a finer step for a short span', () => {
    const short: Span[] = [
      { planet: 'Jupiter', start: Date.UTC(2019, 0, 1), end: Date.UTC(2024, 0, 1) },
    ]
    const ticks = yearTicks(short)
    expect(ticks.length).toBeGreaterThan(2)
    expect(ticks[1]! - ticks[0]!).toBeLessThanOrEqual(2)
  })

  it('never returns more labels than asked for', () => {
    for (const years of [1, 5, 16, 40, 120, 400]) {
      const spans: Span[] = [
        { planet: 'X', start: Date.UTC(2000, 0, 1), end: Date.UTC(2000 + years, 0, 1) },
      ]
      expect(yearTicks(spans).length, `${years} years`).toBeLessThanOrEqual(8)
    }
  })

  it('returns nothing for nothing', () => {
    expect(yearTicks([])).toEqual([])
  })
})

describe('yearOffset', () => {
  it('positions a label inside the track', () => {
    const spans = cycle(1994)
    const offset = yearOffset(spans, 2054)
    expect(offset).not.toBeNull()
    expect(offset!).toBeGreaterThan(0)
    expect(offset!).toBeLessThan(1)
  })

  it('is null for a year off the track', () => {
    expect(yearOffset(cycle(1994), 1900)).toBeNull()
  })
})

describe('formatting', () => {
  it('shows month and year, never a day or a time', () => {
    const span = { planet: 'Jupiter', start: Date.UTC(2019, 2, 14), end: Date.UTC(2035, 2, 9) }
    const text = formatSpan(span, 'en-GB')

    expect(text).toMatch(/Mar 2019/)
    expect(text).toMatch(/Mar 2035/)

    /*
      A dasha boundary is computed to the second, and printing it to the
      second implies the birth time was known to the second — which it
      never is. The shown precision must not exceed the input's.
    */
    expect(text).not.toMatch(/\b14\b|\b09\b|:/)
  })

  it('formats a single month', () => {
    expect(formatMonth(Date.UTC(2026, 10, 1), 'en-GB')).toMatch(/Nov 2026/)
  })
})
