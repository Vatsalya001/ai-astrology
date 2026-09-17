import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'

import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { northIndianHouses, southIndianSigns } from '@ayana/astrology-geometry'

import { ChartSVG } from './ChartSVG'
import { parseChartData } from './parse'
import { planetAbbreviation } from './glyphs'
import type { ChartData } from './types'
import { en } from '@/lib/i18n/dictionaries'

/**
 * Gate item 1: "Kundli dashboard renders a complete, correct chart for
 * all 30 fixtures."
 *
 * `ChartSVG.test.tsx` checks the renderer against one hand-built chart.
 * That proves the mechanism; it cannot prove the renderer survives real
 * output, and real output is where the awkward cases are — the two
 * fixtures with no birth time, the planet sitting on a sign cusp, the
 * Moon on a nakshatra boundary, the southern-hemisphere and
 * quarter-hour-offset charts.
 *
 * These are the same golden files `services/astro` is frozen against, so
 * they are the engine's actual answers rather than something invented
 * for this test. Reading them here also means a change to the engine's
 * payload shape breaks the RENDERER's test, not only Python's — which is
 * the direction that was missing.
 */

const FIXTURES = join(process.cwd(), '..', '..', 'tests', 'fixtures', 'charts', 'expected')

interface Fixture {
  name: string
  chart: ChartData
  hasAscendant: boolean
}

function loadFixtures(): Fixture[] {
  return readdirSync(FIXTURES)
    .sort()
    .map((name) => {
      const raw = JSON.parse(readFileSync(join(FIXTURES, name, 'expected_d1.json'), 'utf8'))

      // Through the real parser, not a bespoke mapper. A test that
      // hand-converts the fixture would pass while `parse.ts` — the code
      // that actually runs — was broken.
      const chart = parseChartData(raw.rasi)
      if (!chart) throw new Error(`${name}: parseChartData refused the golden fixture`)

      return { name, chart, hasAscendant: chart.ascendant !== null }
    })
}

const FIXTURE_LIST = loadFixtures()

describe('every golden fixture', () => {
  it('found all thirty', () => {
    // A directory read that returned nothing would make every test below
    // iterate an empty list and report clean.
    expect(FIXTURE_LIST).toHaveLength(30)
  })

  it('parses through the real boundary parser', () => {
    // loadFixtures throws on refusal, so reaching here means all 30
    // parsed. This states the count the engine guarantees.
    for (const f of FIXTURE_LIST) {
      expect(f.chart.planets.length, f.name).toBe(9)
    }
  })

  /**
   * 28 of the 30 have a birth time. The other two are deliberate: a chart
   * with no ascendant has no houses either, and the South Indian layout
   * keys on the sign so it still draws.
   */
  it('splits into the expected known-time and unknown-time groups', () => {
    const withTime = FIXTURE_LIST.filter((f) => f.hasAscendant)
    expect(withTime).toHaveLength(28)
    expect(FIXTURE_LIST.filter((f) => !f.hasAscendant)).toHaveLength(2)
  })
})

describe.each(FIXTURE_LIST)('$name', ({ name, chart, hasAscendant }) => {
  /**
   * The South Indian chart keys on the SIGN, so it draws with or without
   * a birth time — which is exactly why it is the style that still works
   * for the two time-unknown fixtures.
   */
  it('renders all nine planets in the South Indian style', () => {
    const { unmount } = render(
      <ChartSVG chart={chart} style="south" captionText={en.chart.srTableCaption} />,
    )

    const table = screen.getByRole('table')
    for (const planet of chart.planets) {
      expect(within(table).getByRole('row', { name: new RegExp(planet.planet) }), planet.planet)
        .toBeInTheDocument()
    }
    unmount()
  })

  it(
    hasAscendant
      ? 'renders all nine planets in the North Indian style'
      : 'refuses to draw a North Indian chart without a birth time',
    () => {
      const { container, unmount } = render(
        <ChartSVG chart={chart} style="north" captionText={en.chart.srTableCaption} />,
      )

      if (!hasAscendant) {
        /*
          A North Indian chart IS the houses counted from the ascendant.
          Without one there is nothing to draw, and drawing an empty
          diamond would present a guess as a chart. The component says so
          instead — see its own header.
        */
        expect(container.querySelector('svg'), name).toBeNull()
        unmount()
        return
      }

      const table = screen.getByRole('table')
      for (const planet of chart.planets) {
        expect(within(table).getByRole('row', { name: new RegExp(planet.planet) }), planet.planet)
          .toBeInTheDocument()
      }
      unmount()
    },
  )

  /**
   * Where the glyph is actually DRAWN, not what the table says.
   *
   * This assertion first read the visually-hidden table, and bucketing
   * the North chart by `signIndex` instead of `house` — the classic
   * north/south mix-up, and the single most consequential bug this
   * renderer can have — passed all 123 tests. The table renders
   * `planet.house` straight from the data, so it is identical however
   * the diagram is drawn. It was checking the input, not the output.
   *
   * Reading the `<text>` node's coordinates and testing them against the
   * geometry package's own polygons is the only thing that observes the
   * drawing. Across 28 fixtures this is ~250 real placements.
   */
  it('draws every planet inside its HOUSE region, North Indian', () => {
    if (!hasAscendant) return

    const { container, unmount } = render(
      <ChartSVG chart={chart} style="north" captionText={en.chart.srTableCaption} />,
    )

    const misplaced: string[] = []
    for (const planet of chart.planets) {
      if (planet.house === null) continue

      const at = glyphAt(container, planetAbbreviation(planet.planet))
      const region = northIndianHouses().find((cell) => cell.house === planet.house)!
      if (!inside(region.polygon, at)) {
        misplaced.push(`${planet.planet} (house ${planet.house}) at ${at.x},${at.y}`)
      }
    }

    expect(
      misplaced,
      `${name}: in a North Indian chart the houses are fixed, so a planet belongs to ` +
        'its HOUSE region — not its sign.',
    ).toEqual([])
    unmount()
  })

  /**
   * And the mirror: South Indian cells are fixed SIGNS.
   *
   * Together these two cannot both pass if the bucketing key is wrong,
   * because for most planets the house and the sign are different
   * regions.
   */
  it('draws every planet inside its SIGN cell, South Indian', () => {
    const { container, unmount } = render(
      <ChartSVG chart={chart} style="south" captionText={en.chart.srTableCaption} />,
    )

    const misplaced: string[] = []
    for (const planet of chart.planets) {
      const at = glyphAt(container, planetAbbreviation(planet.planet))
      const cell = southIndianSigns().find((c) => c.sign === planet.signIndex)!
      if (!inside(cell.polygon, at)) {
        misplaced.push(`${planet.planet} (${planet.sign}) at ${at.x},${at.y}`)
      }
    }

    expect(misplaced, `${name}: South Indian cells are fixed signs.`).toEqual([])
    unmount()
  })

  it('names every planet with its sign in the accessible table', () => {
    const { unmount } = render(
      <ChartSVG chart={chart} style="south" captionText={en.chart.srTableCaption} />,
    )

    const table = screen.getByRole('table')
    for (const planet of chart.planets) {
      const row = within(table).getByRole('row', { name: new RegExp(planet.planet) })
      expect(row, `${planet.planet} in ${name}`).toHaveTextContent(planet.sign)
    }
    unmount()
  })
})

/**
 * Where the glyph for a planet was actually drawn.
 *
 * Exact match after stripping the retrograde mark, not `startsWith`: the
 * South Indian chart labels each cell with the first three letters of
 * its sign, so `startsWith('Sa')` finds "Sag" — the Sagittarius label —
 * before it finds Saturn. That is how the equivalent helper in
 * `ChartSVG.test.tsx` failed on its first run.
 */
function glyphAt(container: HTMLElement, abbreviation: string): { x: number; y: number } {
  const text = [...container.querySelectorAll('text')].find(
    (node) => (node.textContent ?? '').replace('\u211e', '') === abbreviation,
  )
  if (!text) throw new Error(`no glyph rendered for ${abbreviation}`)
  return { x: Number(text.getAttribute('x')), y: Number(text.getAttribute('y')) }
}

/** Ray casting. The polygons are convex, but this does not need to know. */
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
