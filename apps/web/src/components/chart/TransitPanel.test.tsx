import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { TransitPanel } from './TransitPanel'
import { RETROGRADE_MARK } from './glyphs'
import type { NatalTransits, TransitPosition } from '@/lib/astrology-api'
import { LocaleProvider } from '@/lib/i18n/context'

function position(overrides: Partial<TransitPosition> = {}): TransitPosition {
  return {
    planet: 'Saturn',
    sign: 'Pisces',
    sign_index: 11,
    degree: 12.5,
    longitude: 342.5,
    is_retrograde: false,
    timestamp: '2026-09-16T12:00:00Z',
    house_from_moon: 12,
    ...overrides,
  }
}

function data(overrides: Partial<NatalTransits> = {}): NatalTransits {
  return {
    at: '2026-09-16T12:00:00Z',
    natal_moon_sign: 'Aries',
    transits: [position()],
    sade_sati: {
      is_active: true,
      phase: 'rising',
      saturn_sign: 'Pisces',
      houses_from_moon: 12,
      started_at: '2023-01-17T00:00:00Z',
      ends_at: '2030-06-03T00:00:00Z',
    },
    ...overrides,
  }
}

function renderPanel(d: NatalTransits) {
  return render(
    <LocaleProvider>
      <TransitPanel data={d} />
    </LocaleProvider>,
  )
}

/**
 * Scoped to the transit list by name.
 *
 * The Sade Sati indicator below is also a list, so a bare
 * `getByRole('listitem')` matches its three phases as well — which is
 * how this was written first, and it failed with "found multiple
 * elements" rather than silently passing. Naming the list fixed both the
 * test and the landmark a screen reader lands on.
 */
function transitItems() {
  return within(screen.getByRole('list', { name: /transiting planets/i })).queryAllByRole(
    'listitem',
  )
}

describe('TransitPanel', () => {
  it('lists each transiting planet with its sign and degree', () => {
    renderPanel(data({ transits: [position({ planet: 'Jupiter', sign: 'Gemini', degree: 8.7 })] }))

    const item = transitItems()[0]!
    expect(item).toHaveTextContent('Jupiter')
    expect(item).toHaveTextContent('Gemini')
    expect(item).toHaveTextContent("8°42'")
  })

  /**
   * Which frame the houses are counted from, said out loud.
   *
   * Gochara counts from the Moon's sign, not the ascendant. "Saturn in
   * your 12th" means two different things depending on where you count
   * from, and a reader comparing this screen with a printed chart has no
   * way to tell which unless it says so.
   */
  it('states that houses are counted from the natal Moon, and which sign that is', () => {
    renderPanel(data({ natal_moon_sign: 'Taurus' }))

    expect(screen.getByText(/counted from your Moon in Taurus/i)).toBeInTheDocument()
  })

  it('gives each row one accessible name rather than four fragments', () => {
    renderPanel(data({ transits: [position({ planet: 'Saturn', house_from_moon: 12 })] }))

    expect(
      screen.getByLabelText('Saturn, in Pisces, 12th house from your Moon'),
    ).toBeInTheDocument()
  })

  it('includes retrograde in the accessible name, not only as a glyph', () => {
    renderPanel(data({ transits: [position({ is_retrograde: true })] }))

    expect(screen.getByLabelText(/retrograde/)).toBeInTheDocument()
    expect(transitItems()[0]!).toHaveTextContent(RETROGRADE_MARK)
  })

  /**
   * The instant the positions were COMPUTED for, not asked for.
   *
   * `data.at` is the query time — api-service defaults it to `now()` —
   * while each row carries the six-hourly slot the refresher wrote it
   * at. This panel rendered `data.at`, so a sky up to six hours old was
   * stamped as computed this second, and a worker down for three days
   * still read "computed just now".
   *
   * The original test set `at` EQUAL to the row timestamp, so the two
   * were indistinguishable and it asserted only `/2026/`. They differ
   * here by design — that is the whole point.
   */
  /**
   * Proved by what the output DEPENDS ON, not by a literal time.
   *
   * The rendered string is local — `12:00Z` reads as "5:30 PM" in
   * Asia/Calcutta — so asserting a literal makes the test pass or fail
   * on the machine's timezone rather than on the code. These two assert
   * the dependency directly: changing `at` must change nothing, and
   * changing the row timestamp must change the line.
   */
  it('ignores the request instant entirely', () => {
    const row = '2026-09-16T12:00:00Z'

    const first = render(
      <LocaleProvider>
        <TransitPanel
          data={data({ at: '2026-09-16T17:55:00Z', transits: [position({ timestamp: row })] })}
        />
      </LocaleProvider>,
    )
    const withLateAt = screen.getByText(/computed/i).textContent
    first.unmount()

    render(
      <LocaleProvider>
        <TransitPanel
          data={data({ at: '2026-09-16T12:00:01Z', transits: [position({ timestamp: row })] })}
        />
      </LocaleProvider>,
    )
    const withEarlyAt = screen.getByText(/computed/i).textContent

    expect(
      withLateAt,
      'the request instant changed the line, so `data.at` is still being rendered',
    ).toBe(withEarlyAt)
  })

  it('tracks the row timestamp', () => {
    const first = render(
      <LocaleProvider>
        <TransitPanel
          data={data({ transits: [position({ timestamp: '2026-09-16T00:00:00Z' })] })}
        />
      </LocaleProvider>,
    )
    const early = screen.getByText(/computed/i).textContent
    first.unmount()

    render(
      <LocaleProvider>
        <TransitPanel
          data={data({ transits: [position({ timestamp: '2026-09-16T18:00:00Z' })] })}
        />
      </LocaleProvider>,
    )
    const late = screen.getByText(/computed/i).textContent

    expect(early, 'the row timestamp does not drive the line').not.toBe(late)
  })

  /**
   * The OLDEST row, because a partial refresh can leave one planet
   * behind and the panel is only as fresh as its stalest position.
   */
  it('reports the oldest position when a refresh was partial', () => {
    const only = render(
      <LocaleProvider>
        <TransitPanel
          data={data({ transits: [position({ timestamp: '2026-09-16T06:00:00Z' })] })}
        />
      </LocaleProvider>,
    )
    const oldestAlone = screen.getByText(/computed/i).textContent
    only.unmount()

    render(
      <LocaleProvider>
        <TransitPanel
          data={data({
            transits: [
              position({ planet: 'Saturn', timestamp: '2026-09-16T06:00:00Z' }),
              position({ planet: 'Jupiter', timestamp: '2026-09-16T18:00:00Z' }),
            ],
          })}
        />
      </LocaleProvider>,
    )

    expect(
      screen.getByText(/computed/i).textContent,
      'the freshest row was reported, overstating how current the set is',
    ).toBe(oldestAlone)
  })

  it('omits the line rather than inventing one when no row has a usable timestamp', () => {
    renderPanel(data({ transits: [position({ timestamp: 'not-a-date' })] }))
    expect(screen.queryByText(/computed/i)).toBeNull()
  })

  it('says nothing has been computed rather than showing an empty list', () => {
    renderPanel(data({ transits: [] }))

    expect(screen.queryByRole('list', { name: /transiting planets/i })).toBeNull()
    expect(screen.getByText(/no transit positions have been computed/i)).toBeInTheDocument()
  })

  it('includes the Sade Sati indicator', () => {
    renderPanel(data())
    expect(screen.getByText(/sade sati/i)).toBeInTheDocument()
    expect(screen.getByText(/— current phase/i)).toBeInTheDocument()
  })
})

/**
 * The nine grahas are listed in the traditional order, not alphabetically.
 *
 * ── Why this needed a test rather than a tidy-up ──
 *
 * The list shipped alphabetical — Ju, Ke, Ma, Me, Mo, Ra, Sa, Su, Ve —
 * and that was nobody's decision. `ListTransitsAt` is a
 * `SELECT DISTINCT ON (planet)`, and Postgres requires the DISTINCT ON
 * expression to be leftmost in ORDER BY. So `ORDER BY planet` is load
 * bearing for correctness, the alphabetical order leaks out of it, and
 * the obvious fix — reorder the query — would break the deduplication
 * that stops a stale row shadowing a fresh one.
 *
 * Sorting therefore lives in the view, and this pins it there. Without a
 * test the next person to touch this file sees `[...].sort()` on data
 * that arrives sorted-looking and removes it.
 *
 * It matters because a reader checking this screen against a printed
 * kundli reads down two lists at once. Every panchanga prints Sun first.
 */
describe('the order the planets are listed in', () => {
  const ALPHABETICAL = [
    'Jupiter', 'Ketu', 'Mars', 'Mercury', 'Moon',
    'Rahu', 'Saturn', 'Sun', 'Venus',
  ]

  it('is traditional even when the data arrives alphabetically', () => {
    // Exactly what the query returns today.
    renderPanel(data({ transits: ALPHABETICAL.map((planet) => position({ planet })) }))

    /*
      Read from the abbreviation cell, not by searching the row text.
      Every row ends "Nth from Moon", so a substring match for a planet
      name finds "Moon" in all nine of them — which is how the first
      version of this assertion passed a list that was still wrong.
    */
    const order = transitItems().map((item) => item.firstElementChild?.textContent?.trim())

    expect(
      order,
      'the transiting planets are listed alphabetically. That is the order ' +
        '`SELECT DISTINCT ON (planet)` forces on the query, not an order ' +
        'anyone chose to display — see GRAHA_ORDER in glyphs.ts.',
    ).toEqual(['Su', 'Mo', 'Ma', 'Me', 'Ju', 'Ve', 'Sa', 'Ra', 'Ke'])
  })

  it('does not reorder the caller\'s array', () => {
    /*
      `sort()` mutates. Sorting props during render is a side effect,
      and React's StrictMode double-render turns that into a real bug
      rather than a style complaint.
    */
    const transits = ALPHABETICAL.map((planet) => position({ planet }))
    const before = transits.map((p) => p.planet)

    renderPanel(data({ transits }))

    expect(
      transits.map((p) => p.planet),
      'the panel sorted the array it was handed, in place',
    ).toEqual(before)
  })

  it('puts an unrecognised body last rather than first', () => {
    // A tenth graha should appear at the end of a list the reader
    // already understands, not displace the Sun.
    renderPanel(
      data({
        transits: [position({ planet: 'Chiron' }), position({ planet: 'Sun' })],
      }),
    )

    const items = transitItems()
    expect(items[0]).toHaveTextContent('Sun')
    expect(items[1]).toHaveTextContent('Chiron')
  })
})
