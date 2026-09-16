import { describe, expect, it } from 'vitest'

import { parseChartData } from './parse'

function planet(overrides: Record<string, unknown> = {}) {
  return {
    planet: 'Saturn',
    longitude: 319.78,
    sign: 'Aquarius',
    sign_index: 10,
    degree: 19.78,
    house: 11,
    nakshatra: 'Shatabhisha',
    nakshatra_index: 23,
    pada: 3,
    is_retrograde: false,
    is_combust: false,
    dignity: 'own_sign',
    ...overrides,
  }
}

function houses() {
  return Array.from({ length: 12 }, (_, i) => ({
    house: i + 1,
    sign: 'Aries',
    sign_index: 0,
    lord: 'Mars',
    planets: [],
  }))
}

describe('parseChartData', () => {
  it('reads a complete chart', () => {
    const parsed = parseChartData({
      ascendant: { sign: 'Aries', sign_index: 0, degree: 12.5, nakshatra: 'Ashwini', pada: 2 },
      houses: houses(),
      planets: [planet()],
    })

    expect(parsed?.planets[0]).toMatchObject({
      planet: 'Saturn',
      sign: 'Aquarius',
      signIndex: 10,
      house: 11,
      pada: 3,
      dignity: 'own_sign',
    })
    expect(parsed?.ascendant?.sign).toBe('Aries')
    expect(parsed?.houses).toHaveLength(12)
  })

  /**
   * The reason this function exists.
   *
   * astro-service sends `house: 0` when the birth time is unknown and
   * documents it as "there are no houses to be in". Carried through, it
   * renders as "0th house" — a position that does not exist, stated as
   * confidently as a real one.
   */
  it('turns house 0 into null rather than a 0th house', () => {
    const parsed = parseChartData({
      ascendant: null,
      houses: null,
      planets: [planet({ house: 0 })],
    })

    expect(parsed?.planets[0]?.house).toBeNull()
  })

  it('distinguishes no houses from an empty house list', () => {
    const noTime = parseChartData({ ascendant: null, houses: null, planets: [planet()] })
    expect(noTime?.houses).toBeNull()
    expect(noTime?.ascendant).toBeNull()
  })

  // Eight houses rendered as the complete set is a chart that is
  // silently wrong. Better to say there are none.
  it('rejects a partial house list instead of showing it as complete', () => {
    const parsed = parseChartData({
      ascendant: null,
      houses: houses().slice(0, 8),
      planets: [planet()],
    })
    expect(parsed?.houses).toBeNull()
  })

  // One unreadable planet drops that planet. A chart missing Ketu is
  // still worth showing; a blank screen is not.
  it('drops an unreadable planet without dropping the chart', () => {
    const parsed = parseChartData({
      planets: [planet(), { planet: 'Ketu' }, planet({ planet: 'Sun' })],
    })
    expect(parsed?.planets.map((p) => p.planet)).toEqual(['Saturn', 'Sun'])
  })

  // ── refusals ──

  it('returns null rather than throwing on a blob it cannot use', () => {
    for (const input of [null, undefined, 42, 'chart', [], {}, { planets: 'none' }]) {
      expect(() => parseChartData(input), JSON.stringify(input)).not.toThrow()
      expect(parseChartData(input), JSON.stringify(input)).toBeNull()
    }
  })

  it('returns null when every planet is unreadable', () => {
    expect(parseChartData({ planets: [{ planet: 'Ketu' }, {}] })).toBeNull()
  })

  it('is not fooled by an inherited property', () => {
    // `{}.constructor` is truthy, so a naive presence check would accept
    // a Function where a sign name belongs.
    expect(parseChartData({ planets: [planet({ sign: undefined })] })).toBeNull()
  })

  it('rejects an out-of-range sign index', () => {
    for (const bad of [-1, 12, 1.5, '3', null]) {
      expect(parseChartData({ planets: [planet({ sign_index: bad })] }), `${bad}`).toBeNull()
    }
  })

  it('rejects a non-finite degree', () => {
    for (const bad of [Number.NaN, Number.POSITIVE_INFINITY, '19.78']) {
      expect(parseChartData({ planets: [planet({ degree: bad })] }), `${bad}`).toBeNull()
    }
  })

  it('treats a missing retrograde flag as not retrograde, not as truthy', () => {
    const parsed = parseChartData({ planets: [planet({ is_retrograde: 'yes' })] })
    // A string is not a boolean. Accepting it would mark every planet
    // retrograde on a payload that merely used the wrong type.
    expect(parsed?.planets[0]?.isRetrograde).toBe(false)
  })
})
