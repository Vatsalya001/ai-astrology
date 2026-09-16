import type { HouseNumber, SignIndex } from '@ayana/astrology-geometry'

/**
 * The chart, as the renderer needs it.
 *
 * A narrower shape than what `GET /charts/{id}` returns, declared here
 * by the consumer. The API response carries meta, dashas, yogas and a
 * navamsa the chart drawing never looks at, and a component typed
 * against the whole envelope would break every time an unrelated field
 * moved.
 */

export interface PlanetPlacement {
  planet: string
  sign: string
  signIndex: SignIndex
  /** Degree within the sign, 0–30. */
  degree: number
  house: HouseNumber
  nakshatra: string
  pada: number
  isRetrograde: boolean
  isCombust: boolean
  dignity: string
}

export interface HousePlacement {
  house: HouseNumber
  sign: string
  signIndex: SignIndex
  lord: string
}

export interface AscendantPlacement {
  sign: string
  signIndex: SignIndex
  degree: number
  nakshatra: string
  pada: number
}

export interface ChartData {
  /**
   * Null when the birth time is unknown.
   *
   * Not an oversight and not a zero: without a birth time the rising
   * sign is a guess, and the whole product's position is that a guess
   * rendered as a fact is the worst outcome. A chart with no ascendant
   * has no houses either, and the renderer says so rather than drawing
   * an empty diamond.
   */
  ascendant: AscendantPlacement | null
  houses: HousePlacement[] | null
  planets: PlanetPlacement[]
}

/**
 * What to emphasise.
 *
 * `'Saturn'` marks a planet; `'house:10'` marks a house. Phase 5's
 * "Why am I seeing this?" passes exactly this — tapping an explanation
 * highlights the planets and houses it was derived from.
 *
 * Built now although nothing uses it yet, because retrofitting it later
 * means touching every render path.
 */
export type Highlight = string

export function isHouseHighlight(value: Highlight): boolean {
  return value.startsWith('house:')
}

export function highlightedHouse(value: Highlight): number | null {
  if (!isHouseHighlight(value)) return null
  const parsed = Number(value.slice('house:'.length))
  return Number.isInteger(parsed) && parsed >= 1 && parsed <= 12 ? parsed : null
}
