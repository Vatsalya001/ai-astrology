import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { defineTerm } from '@ayana/content'

import { DashaTimeline, LEVELS, type TimelineLevel } from './DashaTimeline'
import { LocaleProvider } from '@/lib/i18n/context'

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

function mahadashas() {
  let cursor = Date.UTC(1994, 0, 1)
  return VIMSHOTTARI.map(([planet, years], i) => {
    const start = cursor
    cursor += years * YEAR
    return {
      id: `maha-${i}`,
      planet,
      start: new Date(start).toISOString(),
      end: new Date(cursor).toISOString(),
    }
  })
}

/** Somewhere inside Jupiter: 1994 + 7 + 20 + 6 + 10 + 7 + 18 = 2062. */
const IN_JUPITER = Date.UTC(2064, 0, 1)

function renderTimeline(
  levels: Partial<TimelineLevel>[] = [{ periods: mahadashas() }],
  currentAt = IN_JUPITER,
  onDrillDown = vi.fn(),
) {
  const full: TimelineLevel[] = levels.map((l) => ({
    periods: l.periods ?? [],
    loading: l.loading ?? false,
    // Carried through: the helper dropped it, so every `failed: true`
    // fixture silently rendered as a healthy empty level.
    failed: l.failed ?? false,
  }))
  render(
    <LocaleProvider>
      <DashaTimeline levels={full} currentAt={currentAt} onDrillDown={onDrillDown} />
    </LocaleProvider>,
  )
  return onDrillDown
}

describe('DashaTimeline', () => {
  it('renders every mahadasha', () => {
    renderTimeline()
    expect(screen.getAllByRole('listitem')).toHaveLength(9)
  })

  /**
   * Proportional, not equal columns.
   *
   * Venus runs 20 years and the Sun 6 — a 3.3:1 ratio that the screen
   * exists to convey. Equal columns look tidier and are a lie about the
   * thing people come here to see. Read off the inline width, since
   * jsdom computes no layout.
   */
  it('sizes each period by its real length', () => {
    renderTimeline()

    const widths = new Map<string, number>()
    for (const item of screen.getAllByRole('listitem')) {
      const planet = within(item).getByRole('button').textContent!.trim()
      widths.set(planet, Number.parseFloat((item as HTMLElement).style.width))
    }

    expect(widths.get('Venus')! / widths.get('Sun')!).toBeCloseTo(20 / 6, 2)
    expect(widths.get('Saturn')! / widths.get('Ketu')!).toBeCloseTo(19 / 7, 2)
  })

  it('marks the current period, and only that one', () => {
    renderTimeline()

    const current = screen.getAllByRole('button').filter((b) => b.getAttribute('aria-current'))
    expect(current).toHaveLength(1)
    expect(current[0]).toHaveAccessibleName(/Jupiter mahadasha/i)
  })

  // Colour is not the only carrier: the label says so and aria-current
  // exposes it.
  it('says "current period" in the label, not only in the fill', () => {
    renderTimeline()
    expect(screen.getByRole('button', { name: /current period/i })).toBeInTheDocument()
  })

  /**
   * The shape of the label, not one hardcoded date.
   *
   * Written with a literal span first — "Jan 1994 – Jan 2001" — which
   * failed: seven Vimshottari years from 1 January 1994 is 2556.75 days
   * and lands in December 2000. The component was right and the test's
   * arithmetic was wrong, which is a good argument for asserting the
   * FORMAT rather than restating the fixture's rounding in a second
   * place where it can disagree.
   *
   * Month and year and nothing finer: a dasha boundary is computed to
   * the second, and showing it to the second implies a birth time known
   * to the second.
   */
  it('labels each period with its planet and its span', () => {
    renderTimeline()

    const ketu = screen
      .getAllByRole('button')
      .find((b) => b.getAttribute('aria-label')?.startsWith('Ketu mahadasha'))

    expect(ketu?.getAttribute('aria-label')).toMatch(
      /^Ketu mahadasha, [A-Z][a-z]{2} \d{4} – [A-Z][a-z]{2} \d{4}$/,
    )
  })

  /**
   * The marker's POSITION, read off the element.
   *
   * Added after swapping it to `Date.now()` changed nothing any test
   * could see: every other assertion here reads `aria-current` on the
   * buttons, which comes from `spanAt`, so the drawn marker could point
   * anywhere — including at the browser's clock, which can be wrong by
   * years — and the suite stayed green.
   *
   * The instant used here is 70 of the cycle's 120 years in, so the
   * marker belongs at 0.583 of the track.
   */
  it('draws the marker at the server instant, not at the browser clock', () => {
    const spans = mahadashas()
    const from = Date.parse(spans[0]!.start)
    const to = Date.parse(spans[spans.length - 1]!.end)
    const at = from + (to - from) * 0.7

    const { container } = render(
      <LocaleProvider>
        <DashaTimeline
          levels={[{ periods: spans, loading: false }]}
          currentAt={at}
          onDrillDown={vi.fn()}
        />
      </LocaleProvider>,
    )

    const marker = container.querySelector('[aria-hidden="true"][style*="left"]')
    expect(marker, 'no marker was drawn').not.toBeNull()
    expect(Number.parseFloat((marker as HTMLElement).style.left)).toBeCloseTo(70, 1)
  })

  it('draws no marker at all when the instant is outside the cycle', () => {
    const { container } = render(
      <LocaleProvider>
        <DashaTimeline
          levels={[{ periods: mahadashas(), loading: false }]}
          currentAt={Date.UTC(1980, 0, 1)}
          onDrillDown={vi.fn()}
        />
      </LocaleProvider>,
    )

    expect(container.querySelector('[aria-hidden="true"][style*="left"]')).toBeNull()
  })

  /**
   * The marker is null outside the range, not pinned to an edge.
   *
   * A bar at position 0 for a date before the chart begins says "you are
   * at the start of your first mahadasha", which is a specific and wrong
   * claim about the one thing this screen is for.
   */
  it('marks nothing when the instant is outside the whole cycle', () => {
    renderTimeline([{ periods: mahadashas() }], Date.UTC(1980, 0, 1))

    const current = screen.getAllByRole('button').filter((b) => b.getAttribute('aria-current'))
    expect(current).toEqual([])
  })

  it('opens a detail sheet with the span and the elapsed fraction', async () => {
    const user = userEvent.setup()
    renderTimeline()

    await user.click(screen.getByRole('button', { name: /Jupiter mahadasha/i }))

    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveTextContent('Jupiter Mahadasha')
    expect(within(dialog).getByRole('progressbar')).toHaveAttribute('aria-valuenow')
  })

  // A bar with no value tells a screen reader nothing, and a bar with no
  // number beside it tells a colour-blind reader very little.
  it('gives the progress bar a value and writes the percentage out', async () => {
    const user = userEvent.setup()
    renderTimeline()

    await user.click(screen.getByRole('button', { name: /Jupiter mahadasha/i }))

    const bar = screen.getByRole('progressbar')
    const value = Number(bar.getAttribute('aria-valuenow'))
    expect(value).toBeGreaterThan(0)
    expect(value).toBeLessThan(100)
    expect(screen.getByText(new RegExp(`${value}% elapsed`))).toBeInTheDocument()
  })

  it('shows no progress bar for a period that is not current', async () => {
    const user = userEvent.setup()
    renderTimeline()

    await user.click(screen.getByRole('button', { name: /Ketu mahadasha/i }))
    expect(screen.queryByRole('progressbar')).toBeNull()
  })

  it('asks for the children of whatever was selected', async () => {
    const user = userEvent.setup()
    const onDrillDown = renderTimeline()

    await user.click(screen.getByRole('button', { name: /Venus mahadasha/i }))
    expect(onDrillDown).toHaveBeenCalledWith(1, 'maha-1')
  })

  it('does not try to drill past the third level', async () => {
    const user = userEvent.setup()
    const onDrillDown = renderTimeline([
      { periods: [] },
      { periods: [] },
      { periods: mahadashas().slice(0, 3) },
    ])

    await user.click(screen.getByRole('button', { name: /Ketu pratyantardasha/i }))
    expect(onDrillDown).not.toHaveBeenCalled()
  })

  // ── degradation ────────────────────────────────────────────────────

  /**
   * A period with unusable dates is dropped, not drawn at zero width.
   *
   * `Date.parse` gives NaN, NaN becomes `width: NaN%`, and every browser
   * drops that declaration — so the period vanishes from a timeline that
   * still looks complete, with nothing anywhere to say a sixteen-year
   * mahadasha is missing. This asserts the other eight still render, so
   * one bad row cannot blank the screen either.
   */
  it('drops a period with unusable dates without blanking the track', () => {
    const periods = mahadashas()
    periods[3] = { ...periods[3]!, end: 'not-a-date' }

    renderTimeline([{ periods }])

    expect(screen.getAllByRole('listitem')).toHaveLength(8)
    expect(screen.queryByRole('button', { name: /Moon mahadasha/i })).toBeNull()
    expect(screen.getByRole('button', { name: /Venus mahadasha/i })).toBeInTheDocument()
  })

  it('shows a skeleton while a level is loading', () => {
    renderTimeline([{ periods: [], loading: true }])
    expect(screen.getByText(/mahadasha/i)).toBeInTheDocument()
    expect(document.querySelector('[aria-busy="true"]')).not.toBeNull()
  })

  it('tells the reader to pick a parent before a child level has anything', () => {
    renderTimeline([{ periods: mahadashas() }, { periods: [] }])
    expect(screen.getByText(/choose a mahadasha above/i)).toBeInTheDocument()
  })
})

/**
 * The promise `astro-term-usage.test.ts` allows this file's computed
 * `term={…}` on.
 *
 * `LEVELS[].term` is typed `GlossaryKey`, so a typo cannot compile — but
 * a well-typed key can still point at the wrong entry, which is exactly
 * how the third house came to show the fourth house's meaning. So the
 * mapping is checked, not just the type.
 */
describe('dasha level terms', () => {
  it('defines a term for all three levels', () => {
    const missing = LEVELS.filter((l) => !defineTerm(l.term, 'en')).map((l) => l.name)
    expect(missing).toEqual([])
  })

  it('points each level at the entry named for that level', () => {
    const mismatched: string[] = []
    for (const level of LEVELS) {
      const entry = defineTerm(level.term, 'en')!
      if (!entry.name.toLowerCase().includes(level.name.toLowerCase())) {
        mismatched.push(`${level.name} -> "${entry.name}"`)
      }
    }
    expect(mismatched).toEqual([])
  })

  it('gives no two levels the same term', () => {
    expect(new Set(LEVELS.map((l) => l.term)).size).toBe(LEVELS.length)
  })
})

/**
 * A failed drill-down is not an un-drilled level.
 *
 * Before this, a failure reverted the track to `periods: []`, which
 * renders "Choose a mahadasha above to see its periods" — the message
 * for a level nobody has opened, shown to someone who just opened one.
 * No error, nothing to retry, and the reader told to do the thing they
 * had done.
 */
describe('a level whose fetch failed', () => {
  it('says so instead of asking the reader to choose a parent', () => {
    renderTimeline([
      { periods: mahadashas() },
      { periods: [], loading: false, failed: true },
    ])

    expect(screen.getByRole('alert')).toHaveTextContent(/could not be loaded/i)
    expect(screen.queryByText(/choose a mahadasha above/i)).toBeNull()
  })

  it('offers a retry that names the level', async () => {
    const user = userEvent.setup()
    const onRetryLevel = vi.fn()

    render(
      <LocaleProvider>
        <DashaTimeline
          levels={[
            { periods: mahadashas(), loading: false },
            { periods: [], loading: false, failed: true },
            { periods: [], loading: false },
          ]}
          currentAt={IN_JUPITER}
          onDrillDown={vi.fn()}
          onRetryLevel={onRetryLevel}
        />
      </LocaleProvider>,
    )

    await user.click(screen.getByRole('button', { name: /try again/i }))
    expect(onRetryLevel).toHaveBeenCalledWith(1)
  })

  // An alert, so a screen reader is told rather than silently landing on
  // a track whose content changed underneath it.
  it('announces the failure', () => {
    renderTimeline([{ periods: mahadashas() }, { periods: [], failed: true }])
    expect(screen.getByRole('alert')).toBeInTheDocument()
  })
})

