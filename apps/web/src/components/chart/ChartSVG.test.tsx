import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { northIndianHouses, southIndianSigns } from '@ayana/astrology-geometry'

import { ChartSVG } from './ChartSVG'
import { ordinal, planetLabel } from './glyphs'
import type { ChartData, PlanetPlacement } from './types'

/**
 * What a rendered chart has to get right that geometry tests cannot see:
 * which planet lands in which region, what a screen reader is told, and
 * whether meaning survives without colour.
 *
 * Layout, focus order and computed styles are deliberately NOT here —
 * jsdom lies about all three, and the Phase 3 spec puts visual
 * regression in Playwright for exactly that reason.
 */

function planet(overrides: Partial<PlanetPlacement> & { planet: string }): PlanetPlacement {
  return {
    sign: 'Aries',
    signIndex: 0,
    degree: 10,
    house: 1,
    nakshatra: 'Ashwini',
    pada: 1,
    isRetrograde: false,
    isCombust: false,
    dignity: 'neutral',
    ...overrides,
  } as PlanetPlacement
}

// Pisces rising, which puts Aries in the 2nd house and Aquarius in the
// 12th. Taken from fixture 001 so the placements are a real chart's.
const CHART: ChartData = {
  ascendant: { sign: 'Pisces', signIndex: 11, degree: 22.8, nakshatra: 'Revati', pada: 2 },
  houses: null,
  planets: [
    planet({ planet: 'Sun', sign: 'Aquarius', signIndex: 10, house: 12, degree: 29.5 }),
    planet({ planet: 'Saturn', sign: 'Capricorn', signIndex: 9, house: 11, degree: 12.3, isRetrograde: true }),
    planet({ planet: 'Mercury', sign: 'Aquarius', signIndex: 10, house: 12, degree: 28.1, isCombust: true }),
  ],
}

const NO_TIME: ChartData = { ascendant: null, houses: null, planets: CHART.planets }

describe('ChartSVG', () => {
  it('renders every planet in both styles', () => {
    for (const style of ['north', 'south'] as const) {
      const { unmount } = render(<ChartSVG chart={CHART} style={style} />)
      const table = screen.getByRole('table')
      for (const name of ['Sun', 'Saturn', 'Mercury']) {
        expect(within(table).getByRole('rowheader', { name }), `${style}: ${name}`).toBeInTheDocument()
      }
      unmount()
    }
  })

  // The chart is a group, not an img. role="img" is a LEAF in the
  // accessibility tree — its children, including every planet button,
  // would not be exposed at all. Following the spec's two a11y lines
  // literally produces a chart whose buttons no screen reader reaches.
  it('exposes the chart as a group so its children stay reachable', () => {
    render(<ChartSVG chart={CHART} style="south" onPlanetTap={vi.fn()} />)

    const chart = screen.getByRole('group')
    expect(chart).toHaveAccessibleName(/South Indian birth chart/i)
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    expect(screen.getAllByRole('button').length).toBeGreaterThan(0)
  })

  it('summarises the chart in one sentence, not forty', () => {
    render(<ChartSVG chart={CHART} style="north" />)
    const name = screen.getByRole('group').getAttribute('aria-labelledby')!
    const summary = document.getElementById(name)!

    expect(summary.textContent).toContain('Pisces rising')
    expect(summary.textContent).toContain('Sun in Aquarius')
    expect(summary.textContent).toContain('3 planets placed')
    // The detail belongs in the table. A summary that reads every
    // placement is one a user has to sit through before reaching
    // anything they asked for.
    expect(summary.textContent).not.toContain('Ashwini')
  })

  it('duplicates the data as a captioned table', () => {
    render(<ChartSVG chart={CHART} style="north" />)
    const table = screen.getByRole('table')

    // The caption is what makes the repetition read as deliberate. A
    // screen-reader user meeting the same nine planets twice with no
    // explanation reasonably concludes something is broken.
    expect(table).toHaveAccessibleName(/same data as the chart above/i)

    const saturn = within(table).getByRole('row', { name: /Saturn/ })
    expect(saturn).toHaveTextContent('Capricorn')
    expect(saturn).toHaveTextContent('11th')
    expect(saturn).toHaveTextContent('retrograde')
  })

  it('includes the ascendant in the table, and omits it when there is none', () => {
    const { unmount } = render(<ChartSVG chart={CHART} style="south" />)
    expect(within(screen.getByRole('table')).getByRole('rowheader', { name: 'Ascendant' }))
      .toBeInTheDocument()
    unmount()

    render(<ChartSVG chart={NO_TIME} style="south" />)
    expect(within(screen.getByRole('table')).queryByRole('rowheader', { name: 'Ascendant' }))
      .not.toBeInTheDocument()
  })

  // Colour is never the only carrier. Roughly 8% of men cannot
  // distinguish red from green, and both of these change how a
  // placement is read.
  it('marks retrograde with a glyph and combustion with a ring', () => {
    const { container } = render(<ChartSVG chart={CHART} style="south" />)

    expect(container.textContent).toContain('℞')
    expect(container.querySelector('circle')).toBeInTheDocument()
  })

  it('names retrograde and combustion in words too', () => {
    const table = render(<ChartSVG chart={CHART} style="north" />).getByRole('table')
    expect(within(table).getByRole('row', { name: /Mercury/ })).toHaveTextContent('combust')
  })

  // Without a birth time there is no rising sign, so a North Indian
  // chart — whose entire structure is houses counted from it — cannot be
  // drawn. An empty diamond looks like a loading state or a bug.
  it('refuses to draw a North Indian chart without a birth time, and says why', () => {
    render(<ChartSVG chart={NO_TIME} style="north" />)

    expect(screen.getByText(/needs an exact birth time/i)).toBeInTheDocument()
    expect(screen.queryByRole('group')).not.toBeInTheDocument()
  })

  it('still draws a South Indian chart without a birth time', () => {
    render(<ChartSVG chart={NO_TIME} style="south" />)

    expect(screen.getByRole('group')).toBeInTheDocument()
    const name = screen.getByRole('group').getAttribute('aria-labelledby')!
    expect(document.getElementById(name)!.textContent).toContain('rising sign unknown')
  })
})

describe('interaction', () => {
  it('reports the whole planet when one is tapped', async () => {
    const onPlanetTap = vi.fn()
    const user = userEvent.setup()

    render(<ChartSVG chart={CHART} style="south" onPlanetTap={onPlanetTap} />)
    await user.click(screen.getByRole('button', { name: planetLabel(CHART.planets[1]!) }))

    expect(onPlanetTap).toHaveBeenCalledWith(CHART.planets[1])
  })

  it('works from the keyboard, with Enter and with Space', async () => {
    const onPlanetTap = vi.fn()
    const user = userEvent.setup()

    render(<ChartSVG chart={CHART} style="south" onPlanetTap={onPlanetTap} />)
    const saturn = screen.getByRole('button', { name: planetLabel(CHART.planets[1]!) })

    saturn.focus()
    await user.keyboard('{Enter}')
    await user.keyboard(' ')

    expect(onPlanetTap).toHaveBeenCalledTimes(2)
  })

  it('reports the house number when a region is tapped', async () => {
    const onHouseTap = vi.fn()
    const user = userEvent.setup()

    render(<ChartSVG chart={CHART} style="north" onHouseTap={onHouseTap} />)
    await user.click(screen.getByRole('button', { name: /^11th house/ }))

    expect(onHouseTap).toHaveBeenCalledWith(11)
  })

  // A focusable element that does nothing is a tab stop a keyboard user
  // pays for and gets nothing back from. Twelve of them, on a chart.
  it('adds no tab stops when there is nothing to tap', () => {
    render(<ChartSVG chart={CHART} style="north" />)
    expect(screen.queryAllByRole('button')).toHaveLength(0)
  })
})

describe('highlight', () => {
  // The hook Phase 5's "Why am I seeing this?" depends on. Built now
  // because retrofitting it means touching every render path.
  it('marks the named planets and houses', () => {
    const { container } = render(
      <ChartSVG chart={CHART} style="north" highlight={['Saturn', 'house:10']} />,
    )

    const marked = container.querySelectorAll('.fill-gold')
    expect(marked.length, 'the highlighted planet should be emphasised').toBeGreaterThan(0)
    expect(
      container.querySelectorAll('.fill-gold\\/15').length,
      'the highlighted house should be shaded',
    ).toBe(1)
  })

  it('ignores a house number outside 1..12 rather than shading nothing silently', () => {
    const { container } = render(
      <ChartSVG chart={CHART} style="north" highlight={['house:13', 'house:0']} />,
    )
    expect(container.querySelectorAll('.fill-gold\\/15')).toHaveLength(0)
  })
})

describe('ordinal', () => {
  // 11th, 12th and 13th are the exceptions every naive implementation
  // renders as 11st, 12nd and 13rd — and houses go to 12.
  it.each([
    [1, '1st'], [2, '2nd'], [3, '3rd'], [4, '4th'],
    [11, '11th'], [12, '12th'], [13, '13th'],
    [21, '21st'], [22, '22nd'], [23, '23rd'],
  ])('renders %i as %s', (value, expected) => {
    expect(ordinal(value)).toBe(expected)
  })
})

describe('planetLabel', () => {
  it('says everything the glyph shows, in words', () => {
    expect(
      planetLabel({
        planet: 'Saturn', sign: 'Aquarius', house: 10,
        degree: 12.4, isRetrograde: true, isCombust: false,
      }),
    ).toBe('Saturn in Aquarius, 10th house, 12 degrees, retrograde')
  })

  it('rounds the degree — a chart is read to the degree', () => {
    expect(
      planetLabel({
        planet: 'Sun', sign: 'Leo', house: 1,
        degree: 12.741, isRetrograde: false, isCombust: false,
      }),
    ).toContain('13 degrees')
  })
})

// ─── the placement itself ────────────────────────────────────────────

/**
 * The property that IS the difference between the two styles, and the
 * one every other test in this file missed.
 *
 * North Indian: houses are fixed, signs rotate — so a planet is drawn in
 * its HOUSE region. South Indian: signs are fixed — so the same planet
 * is drawn in its SIGN cell. Grouping by the wrong one produces a chart
 * that is complete, plausible and wrong, and it passed all 27 tests
 * above: they check the table and the labels, never which polygon a
 * glyph lands in.
 */
describe('planets land in the right region', () => {
  /**
   * Where the glyph for a planet was actually drawn.
   *
   * Exact match after stripping the retrograde mark, not `startsWith`.
   * The South Indian chart labels each cell with the first three letters
   * of its sign, so `startsWith('Sa')` finds "Sag" — the Sagittarius
   * label — before it finds Saturn, and reports the planet as being in
   * the wrong cell. That is how this helper failed on its first run.
   */
  function glyphAt(container: HTMLElement, abbreviation: string): { x: number; y: number } {
    const text = [...container.querySelectorAll('text')].find(
      (node) => (node.textContent ?? '').replace('℞', '') === abbreviation,
    )
    if (!text) throw new Error(`no glyph rendered for ${abbreviation}`)
    return { x: Number(text.getAttribute('x')), y: Number(text.getAttribute('y')) }
  }

  function inside(polygon: { x: number; y: number }[], point: { x: number; y: number }): boolean {
    let within = false
    for (let i = 0, j = polygon.length - 1; i < polygon.length; j = i++) {
      const a = polygon[i]!
      const b = polygon[j]!
      if (a.y > point.y === b.y > point.y) continue
      if (point.x < ((b.x - a.x) * (point.y - a.y)) / (b.y - a.y) + a.x) within = !within
    }
    return within
  }

  it('draws a planet in its HOUSE region in the North Indian chart', () => {
    const { container } = render(<ChartSVG chart={CHART} style="north" />)

    // Saturn is in the 11th house and in Capricorn (sign 9). Those are
    // different regions, which is what makes this test able to fail.
    const saturn = glyphAt(container, 'Sa')
    const eleventh = northIndianHouses().find((cell) => cell.house === 11)!

    expect(
      inside(eleventh.polygon, saturn),
      `Saturn was drawn at (${saturn.x}, ${saturn.y}), outside the 11th house. ` +
        'In a North Indian chart the houses are fixed and a planet belongs to its ' +
        'house region, not its sign.',
    ).toBe(true)
  })

  it('draws the same planet in its SIGN cell in the South Indian chart', () => {
    const { container } = render(<ChartSVG chart={CHART} style="south" />)

    const saturn = glyphAt(container, 'Sa')
    const capricorn = southIndianSigns().find((cell) => cell.sign === 9)!

    expect(
      inside(capricorn.polygon, saturn),
      `Saturn was drawn at (${saturn.x}, ${saturn.y}), outside Capricorn. ` +
        'In a South Indian chart the signs are fixed and a planet belongs to its ' +
        'sign cell, not its house.',
    ).toBe(true)
  })

  it('stacks two planets sharing a region without overlapping', () => {
    // Sun and Mercury are both in Aquarius, both in the 12th — the
    // ordinary case, not an exotic one.
    const { container } = render(<ChartSVG chart={CHART} style="south" />)
    const sun = glyphAt(container, 'Su')
    const mercury = glyphAt(container, 'Me')

    expect(sun.x === mercury.x && sun.y === mercury.y).toBe(false)
  })
})
