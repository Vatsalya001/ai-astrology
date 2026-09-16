import type { HouseNumber, SignIndex } from '@ayana/astrology-geometry'

import type {
  AscendantPlacement,
  ChartData,
  HousePlacement,
  PlanetPlacement,
  YogaPlacement,
} from './types'

/**
 * `chart_data` from the API, into the shape the renderer takes.
 *
 * `Chart.chart_data` is `unknown` on purpose: it is a JSON blob the Go
 * service stores verbatim from astro-service, and typing it as the happy
 * path would mean every component trusts a field that may not be there.
 * This is the one place that trust is established.
 *
 * ── Why it is defensive about our own service ──
 *
 * The rule is "validate at the boundary", and this is a boundary even
 * though both sides are ours. The stored blob can predate a schema
 * change by however long a chart has been cached, and Phase 2 already
 * shipped a payload that was not what the caller thought it was — a D9
 * that was a relabelled D1, which no amount of type declaration caught
 * because the JSON was structurally perfect.
 *
 * So this returns `null` rather than throwing on a blob it cannot use.
 * A screen that says "this chart could not be read" is recoverable; an
 * exception inside a render is a blank page.
 */
export function parseChartData(raw: unknown): ChartData | null {
  if (!isRecord(raw)) return null

  const planets = raw.planets
  if (!Array.isArray(planets)) return null

  const parsedPlanets: PlanetPlacement[] = []
  for (const entry of planets) {
    const planet = parsePlanet(entry)
    // One unreadable planet drops that planet, not the chart. A chart
    // missing Ketu is still worth showing; a blank screen is not.
    if (planet) parsedPlanets.push(planet)
  }
  if (parsedPlanets.length === 0) return null

  return {
    ascendant: parseAscendant(raw.ascendant),
    houses: parseHouses(raw.houses),
    planets: parsedPlanets,
    yogas: parseYogas(raw.yogas),
  }
}

/**
 * Missing and empty both become an empty list, deliberately.
 *
 * A divisional chart carries no yogas at all — they are read from the
 * rasi — so the key is absent there, while a rasi with none present has
 * `[]`. Both mean "nothing to show" to this screen, and distinguishing
 * them would push a decision onto every caller that none of them can
 * act on differently.
 */
function parseYogas(raw: unknown): YogaPlacement[] {
  if (!Array.isArray(raw)) return []

  const yogas: YogaPlacement[] = []
  for (const entry of raw) {
    if (!isRecord(entry)) continue
    const name = str(entry.name)
    if (name === null) continue

    yogas.push({
      name,
      // Anything the engine did not send reads as `moderate` rather than
      // as `strong`: overstating a combination is the worse direction to
      // be wrong in.
      strength: str(entry.strength) ?? 'moderate',
      involvedPlanets: stringList(entry.involved_planets),
      involvedHouses: numberList(entry.involved_houses),
    })
  }
  return yogas
}

function stringList(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string') : []
}

function numberList(value: unknown): number[] {
  return Array.isArray(value)
    ? value.filter((v): v is number => typeof v === 'number' && Number.isInteger(v))
    : []
}

function parsePlanet(raw: unknown): PlanetPlacement | null {
  if (!isRecord(raw)) return null

  const name = str(raw.planet)
  const sign = str(raw.sign)
  const signIndex = signIndexOf(raw.sign_index)
  const degree = num(raw.degree)
  if (name === null || sign === null || signIndex === null || degree === null) return null

  return {
    planet: name,
    sign,
    signIndex,
    degree,
    house: houseNumberOf(raw.house),
    nakshatra: str(raw.nakshatra) ?? '',
    pada: num(raw.pada) ?? 0,
    isRetrograde: raw.is_retrograde === true,
    isCombust: raw.is_combust === true,
    dignity: str(raw.dignity) ?? 'neutral',
  }
}

/**
 * Null when the birth time is unknown, which is a different thing from
 * an empty list.
 *
 * An empty array renders as a house view with twelve nothings; null
 * renders as an explanation the reader can act on. The API distinguishes
 * them and so does this.
 */
function parseHouses(raw: unknown): HousePlacement[] | null {
  if (!Array.isArray(raw)) return null

  const houses: HousePlacement[] = []
  for (const entry of raw) {
    if (!isRecord(entry)) continue
    const house = houseNumberOf(entry.house)
    const sign = str(entry.sign)
    const signIndex = signIndexOf(entry.sign_index)
    if (house === null || sign === null || signIndex === null) continue
    houses.push({ house, sign, signIndex, lord: str(entry.lord) ?? '' })
  }

  // A partial house list is worse than none: eight houses rendered as
  // the complete set is a chart that is silently wrong.
  return houses.length === 12 ? houses : null
}

function parseAscendant(raw: unknown): AscendantPlacement | null {
  if (!isRecord(raw)) return null

  const sign = str(raw.sign)
  const signIndex = signIndexOf(raw.sign_index)
  const degree = num(raw.degree)
  if (sign === null || signIndex === null || degree === null) return null

  return {
    sign,
    signIndex,
    degree,
    nakshatra: str(raw.nakshatra) ?? '',
    pada: num(raw.pada) ?? 0,
  }
}

// ─── narrow helpers ──────────────────────────────────────────────────

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function str(value: unknown): string | null {
  return typeof value === 'string' && value.length > 0 ? value : null
}

function num(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function signIndexOf(value: unknown): SignIndex | null {
  return typeof value === 'number' && Number.isInteger(value) && value >= 0 && value <= 11
    ? (value as SignIndex)
    : null
}

/**
 * `0` becomes null, not house 0.
 *
 * astro-service sends `0` when the birth time is unknown and documents
 * it as "there are no houses to be in". Passing it through produces
 * "0th house" wherever an ordinal is taken — a position that does not
 * exist, stated as confidently as a real one.
 */
function houseNumberOf(value: unknown): HouseNumber | null {
  return typeof value === 'number' && Number.isInteger(value) && value >= 1 && value <= 12
    ? (value as HouseNumber)
    : null
}
