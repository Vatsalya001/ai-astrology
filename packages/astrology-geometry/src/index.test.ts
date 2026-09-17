import { describe, expect, it } from 'vitest'

import {
  area,
  glyphPosition,
  houseOfSign,
  northIndianHouses,
  pointInPolygon,
  signOfHouse,
  signedArea2,
  SIGN_COUNT,
  SIZE,
  southIndianCentre,
  southIndianSigns,
  toPoints,
  type HouseNumber,
  type Point,
  type SignIndex,
} from './index'

/**
 * North Indian geometry is fiddly, and its failure mode is a chart that
 * looks *almost* right — one region slightly too big, two overlapping by
 * a sliver, a corner triangle on the wrong side of a diagonal. None of
 * that is visible in a rendered-component assertion, and none of it is
 * obvious by eye.
 *
 * So the tests here check the polygons AS polygons: do the twelve tile
 * the square exactly, does any pair overlap, is house 1 where the
 * convention demands. A mistyped coordinate breaks at least one of those
 * and cannot break none of them.
 */

const signs = [...Array(SIGN_COUNT).keys()] as SignIndex[]
const houses = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12] as HouseNumber[]

// ─── the rotation ────────────────────────────────────────────────────

describe('houseOfSign', () => {
  it("counts inclusively — the ascendant's own sign is house 1", () => {
    expect(houseOfSign(9, 9)).toBe(1)
    expect(houseOfSign(10, 9)).toBe(2)
  })

  it('wraps backwards without going negative', () => {
    // JavaScript's % keeps the dividend's sign, so 0 - 11 is -11 and
    // yields -10 without the second fold. Every chart with an ascendant
    // later in the zodiac than a planet hits this.
    expect(houseOfSign(11, 0)).toBe(12)
    expect(houseOfSign(0, 11)).toBe(2)
  })

  it('puts the opposite sign in the 7th', () => {
    expect(houseOfSign(3, 9)).toBe(7)
  })

  // The property a reflected or truncated rotation fails: it still
  // returns numbers in 1..12, but two signs collide and a house vanishes.
  it('is a permutation for every ascendant', () => {
    for (const ascendant of signs) {
      const produced = signs.map((sign) => houseOfSign(sign, ascendant))
      expect(new Set(produced).size, `ascendant ${ascendant}`).toBe(SIGN_COUNT)
      expect([...produced].sort((a, b) => a - b)).toEqual(houses)
    }
  })
})

describe('signOfHouse', () => {
  it('is the exact inverse of houseOfSign', () => {
    for (const ascendant of signs) {
      for (const house of houses) {
        expect(houseOfSign(signOfHouse(house, ascendant), ascendant)).toBe(house)
      }
    }
  })
})

// ─── North Indian ────────────────────────────────────────────────────

describe('northIndianHouses', () => {
  const cells = northIndianHouses()

  it('returns houses 1 to 12, in order', () => {
    expect(cells.map((c) => c.house)).toEqual(houses)
  })

  // The convention. House 1 anywhere else is a chart a North Indian
  // reader cannot use, and it would look perfectly fine to anyone else.
  it('puts house 1 in the top-centre rhombus', () => {
    const first = cells[0]!
    const centre = centroid(first.polygon)
    expect(centre.x).toBeCloseTo(SIZE / 2, 5)
    expect(centre.y).toBeLessThan(SIZE / 2)
    expect(first.polygon).toHaveLength(4)
  })

  it('puts house 4 west, house 7 south and house 10 east', () => {
    // Anticlockwise from the top. Running them clockwise swaps 4 and 10
    // and every planet placement with them.
    expect(centroid(cells[3]!.polygon).x).toBeLessThan(SIZE / 2)
    expect(centroid(cells[6]!.polygon).y).toBeGreaterThan(SIZE / 2)
    expect(centroid(cells[9]!.polygon).x).toBeGreaterThan(SIZE / 2)
  })

  it('has four rhombi and eight triangles', () => {
    const quads = cells.filter((c) => c.polygon.length === 4).map((c) => c.house)
    const triangles = cells.filter((c) => c.polygon.length === 3).map((c) => c.house)
    expect(quads).toEqual([1, 4, 7, 10])
    expect(triangles).toHaveLength(8)
  })

  // The strongest check available without rendering: twelve regions that
  // tile a square have areas summing to exactly the square's area. A
  // coordinate typo makes one region too big and its neighbour too small
  // — and that sums to the same total only if the error is a shared edge
  // moving, which the overlap test below catches instead.
  it('tiles the square exactly', () => {
    const total = cells.reduce((sum, cell) => sum + area(cell.polygon), 0)
    expect(total).toBeCloseTo(SIZE * SIZE, 6)
  })

  it('has no degenerate region', () => {
    for (const cell of cells) {
      expect(area(cell.polygon), `house ${cell.house}`).toBeGreaterThan(0)
    }
  })

  it('winds every polygon the same way', () => {
    // Mixed winding renders identically but breaks any later use of
    // fill-rule, hit-testing or path reversal — the kind of thing that
    // surfaces in Phase 10 under a different SVG engine.
    const signs = cells.map((cell) => Math.sign(signedArea2(cell.polygon)))
    expect(new Set(signs).size).toBe(1)
  })

  it('has no two regions overlapping', () => {
    // Sampled rather than computed analytically: a polygon-intersection
    // routine here would be more code than the thing it checks, and a
    // dense sample finds any overlap big enough to see.
    //
    // The offsets matter. The first version stepped 1.7 from 0.85 in
    // both axes, which lands every sample where x === y — exactly on the
    // diagonal that houses 2 and 3 share — and ray casting is ambiguous
    // for a point on an edge. It reported an overlap that does not
    // exist. Different offsets and steps per axis keep samples off both
    // diagonals and off the midlines.
    for (const point of interiorSamples()) {
      const containing = cells.filter((cell) => contains(cell.polygon, point))
      expect(
        containing.length,
        `point (${point.x.toFixed(3)}, ${point.y.toFixed(3)}) is inside houses ` +
          `${containing.map((c) => c.house).join(', ')} — regions must not overlap`,
      ).toBeLessThanOrEqual(1)
    }
  })

  it('covers every interior point', () => {
    // The other half of tiling. Together with the overlap test this says
    // each point belongs to exactly one house, which is what "tiles"
    // means and what the area sum alone does not prove.
    const uncovered = interiorSamples().filter(
      (point) => !cells.some((cell) => contains(cell.polygon, point)),
    )
    expect(
      uncovered.slice(0, 5),
      `${uncovered.length} interior points belong to no house`,
    ).toEqual([])
  })

  it('keeps every label and glyph anchor inside its own region', () => {
    for (const cell of cells) {
      expect(contains(cell.polygon, cell.label), `house ${cell.house} label`).toBe(true)
      expect(
        contains(cell.polygon, cell.glyphAnchor),
        `house ${cell.house} glyph anchor`,
      ).toBe(true)
    }
  })
})

// ─── South Indian ────────────────────────────────────────────────────

describe('southIndianSigns', () => {
  const cells = southIndianSigns()

  it('places all twelve signs exactly once', () => {
    expect(cells).toHaveLength(SIGN_COUNT)
    expect(new Set(cells.map((c) => c.sign)).size).toBe(SIGN_COUNT)
  })

  // Fixed positions are the whole point of the style: a reader's eye
  // goes to a known cell for a known sign. Moving one makes the chart
  // unreadable to exactly the people who asked for this layout.
  it('puts Pisces top-left and runs clockwise', () => {
    const at = (row: number, column: number) =>
      cells.find((c) => c.row === row && c.column === column)?.sign

    expect(at(0, 0)).toBe(11) // Pisces
    expect(at(0, 1)).toBe(0) // Aries
    expect(at(0, 3)).toBe(2) // Gemini
    expect(at(1, 3)).toBe(3) // Cancer
    expect(at(3, 3)).toBe(5) // Virgo
    expect(at(3, 0)).toBe(8) // Sagittarius
    expect(at(1, 0)).toBe(10) // Aquarius
  })

  it('leaves the middle four cells hollow', () => {
    for (const row of [1, 2]) {
      for (const column of [1, 2]) {
        expect(
          cells.some((c) => c.row === row && c.column === column),
          `cell (${row}, ${column}) must be part of the hollow centre`,
        ).toBe(false)
      }
    }
  })

  it('gives every sign an equal square', () => {
    const side = SIZE / 4
    for (const cell of cells) {
      expect(area(cell.polygon), `sign ${cell.sign}`).toBeCloseTo(side * side, 6)
      expect(cell.polygon).toHaveLength(4)
    }
  })

  it('tiles the ring exactly, leaving the centre for the hollow', () => {
    const total = cells.reduce((sum, cell) => sum + area(cell.polygon), 0)
    const centre = area(southIndianCentre())
    expect(total + centre).toBeCloseTo(SIZE * SIZE, 6)
  })

  it('keeps labels and glyph anchors inside their cells', () => {
    for (const cell of cells) {
      expect(contains(cell.polygon, cell.label), `sign ${cell.sign} label`).toBe(true)
      expect(contains(cell.polygon, cell.glyphAnchor), `sign ${cell.sign} glyphs`).toBe(true)
    }
  })

  it('has no cell overlapping the hollow centre', () => {
    const centre = southIndianCentre()
    for (const cell of cells) {
      expect(
        contains(centre, centroid(cell.polygon)),
        `sign ${cell.sign} sits over the hollow centre`,
      ).toBe(false)
    }
  })
})

// ─── glyph stacking ──────────────────────────────────────────────────

describe('glyphPosition', () => {
  const cell = southIndianSigns()[0]!

  /**
   * Centred on the anchor, not grown down from it.
   *
   * This asserted `first === anchor`, which only holds when the stack
   * grows downward — and growing downward is what drew the third planet
   * in a narrow North Indian triangle OUTSIDE its own house. See the
   * containment tests below.
   *
   * A single glyph still sits exactly on the anchor; two straddle it.
   */
  it('centres the stack on the anchor', () => {
    const alone = glyphPosition(cell, 0, 'south', 1)
    expect(alone).toEqual({ x: cell.glyphAnchor.x, y: cell.glyphAnchor.y })

    const first = glyphPosition(cell, 0, 'south', 2)
    const second = glyphPosition(cell, 1, 'south', 2)

    expect(second.y).toBeGreaterThan(first.y)
    expect(second.x).toBe(first.x)
    // Straddling: their midpoint is the anchor.
    expect((first.y + second.y) / 2).toBeCloseTo(cell.glyphAnchor.y, 6)
  })

  // A stellium of six planets in one sign is ordinary, not exotic —
  // Phase 2's fixtures have them. A single column overflows the cell.
  it('wraps into a second column past four', () => {
    const fourth = glyphPosition(cell, 3, 'south', 5)
    const fifth = glyphPosition(cell, 4, 'south', 5)
    expect(fifth.x).toBeGreaterThan(fourth.x)
    // The fifth is alone in its column, so it sits on the anchor's row.
    expect(fifth.y).toBeCloseTo(cell.glyphAnchor.y, 6)
  })

  it('never returns the same position twice for nine planets', () => {
    const seen = new Set(
      Array.from({ length: 9 }, (_, i) => {
        const point = glyphPosition(cell, i, 'south', 9)
        return `${point.x},${point.y}`
      }),
    )
    expect(seen.size).toBe(9)
  })
})

describe('toPoints', () => {
  it('renders an SVG points attribute', () => {
    expect(toPoints([{ x: 0, y: 0 }, { x: 50, y: 25 }])).toBe('0,0 50,25')
  })

  it('rounds so a diff is readable', () => {
    expect(toPoints([{ x: 1 / 3, y: 2 / 3 }])).toBe('0.333,0.667')
  })
})

// ─── helpers ─────────────────────────────────────────────────────────

/**
 * Interior sample points that avoid every shared edge.
 *
 * The North Indian regions meet on y = x, y = SIZE - x, x = SIZE/2 and
 * y = SIZE/2, plus the four sides of the inner rhombus. Any sample
 * landing on one of those is ambiguous under ray casting and produces a
 * failure that looks like an overlap. Prime-ish offsets and unequal
 * steps keep the grid off all of them.
 */
function interiorSamples(): Point[] {
  const points: Point[] = []
  for (let x = 0.37; x < SIZE; x += 1.31) {
    for (let y = 0.61; y < SIZE; y += 1.73) {
      // Belt and braces: skip anything that still lands on a diagonal or
      // a midline, rather than trusting the offsets to have covered it.
      if (Math.abs(x - y) < 1e-9) continue
      if (Math.abs(x + y - SIZE) < 1e-9) continue
      if (Math.abs(x - SIZE / 2) < 1e-9 || Math.abs(y - SIZE / 2) < 1e-9) continue
      points.push({ x, y })
    }
  }
  return points
}

function centroid(polygon: Point[]): Point {
  const sum = polygon.reduce(
    (acc, point) => ({ x: acc.x + point.x, y: acc.y + point.y }),
    { x: 0, y: 0 },
  )
  return { x: sum.x / polygon.length, y: sum.y / polygon.length }
}

/** Ray casting. Points exactly on an edge count as outside. */
function contains(polygon: Point[], point: Point): boolean {
  let inside = false
  for (let i = 0, j = polygon.length - 1; i < polygon.length; j = i++) {
    const a = polygon[i]!
    const b = polygon[j]!
    const straddles = a.y > point.y !== b.y > point.y
    if (!straddles) continue
    const crossing = ((b.x - a.x) * (point.y - a.y)) / (b.y - a.y) + a.x
    if (point.x < crossing) inside = !inside
  }
  return inside
}

/**
 * Glyphs stay inside the region they belong to.
 *
 * This is a geometry property, so it belongs here rather than in a React
 * test — but it was found by rendering all 30 golden fixtures and
 * checking each drawn glyph against its own polygon, because nothing
 * here asked the question.
 *
 * The bug: `glyphPosition` stacked downward from the centroid, and the
 * narrow North Indian triangles taper. House 11's apex is at x=75, so at
 * the glyph column x=81.25 the polygon spans only y in [18.75, 31.25] —
 * and the third of three planets landed at y=33, drawn in the
 * NEIGHBOURING HOUSE. A reader sees a planet in the wrong house.
 *
 * It was invisible because the existing stacking test used two planets,
 * which fits, and the visually-hidden table renders the house from the
 * data and is therefore correct however the diagram is drawn.
 */
describe('glyphs stay inside their region', () => {
  // The package's own predicate, not a copy — two implementations of
  // point-in-polygon is two chances to get the winding wrong, and the
  // test would then be checking its own copy rather than the code.
  const inside = pointInPolygon

  // Nine planets is the real maximum — a chart has exactly nine grahas,
  // and all nine can share one house.
  const COUNTS = [1, 2, 3, 4, 5, 6, 7, 8, 9]

  it('keeps every North Indian glyph inside its house, for every crowding', () => {
    const escaped: string[] = []

    for (const cell of northIndianHouses()) {
      for (const total of COUNTS) {
        for (let i = 0; i < total; i++) {
          const at = glyphPosition(cell, i, 'north', total)
          if (!inside(cell.polygon, at)) {
            escaped.push(`house ${cell.house}, glyph ${i + 1} of ${total} at ${at.x},${at.y}`)
          }
        }
      }
    }

    expect(
      escaped.slice(0, 8),
      `${escaped.length} glyph position(s) fall outside their own house. A glyph drawn ` +
        'outside its region is a planet shown in the wrong house.',
    ).toEqual([])
  })

  it('keeps every South Indian glyph inside its sign cell, for every crowding', () => {
    const escaped: string[] = []

    for (const cell of southIndianSigns()) {
      for (const total of COUNTS) {
        for (let i = 0; i < total; i++) {
          const at = glyphPosition(cell, i, 'south', total)
          if (!inside(cell.polygon, at)) {
            escaped.push(`sign ${cell.sign}, glyph ${i + 1} of ${total} at ${at.x},${at.y}`)
          }
        }
      }
    }

    expect(escaped.slice(0, 8)).toEqual([])
  })

  // Two glyphs in one region must not land on each other, which is the
  // property the centring must not break.
  it('never places two glyphs of the same group at the same point', () => {
    for (const cell of northIndianHouses()) {
      for (const total of COUNTS) {
        const seen = new Set<string>()
        for (let i = 0; i < total; i++) {
          const at = glyphPosition(cell, i, 'north', total)
          const key = `${at.x},${at.y}`
          expect(seen.has(key), `house ${cell.house}: glyph ${i} collides at ${key}`).toBe(false)
          seen.add(key)
        }
      }
    }
  })
})

