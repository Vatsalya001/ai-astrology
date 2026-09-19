/**
 * Planet abbreviations and the marks that carry meaning.
 *
 * Two-letter Latin abbreviations, not the Unicode astrological symbols
 * (☉ ☽ ♂ ☿ ♃ ♀ ♄ ☊ ☋). Three reasons, in order of weight:
 *
 *   1. They are what every Indian chart uses, printed and on screen. A
 *      reader who knows these charts is looking for "Sa", not "♄".
 *   2. The Unicode symbols render inconsistently across Android fonts
 *      and are illegible at the sizes a twelfth of a phone screen
 *      allows.
 *   3. Screen readers pronounce several of them as "white circle" or
 *      skip them entirely.
 *
 * The full name still reaches assistive technology through the
 * aria-label; nothing depends on the abbreviation being understood.
 */
export const PLANET_ABBREVIATIONS: Record<string, string> = {
  Sun: 'Su',
  Moon: 'Mo',
  Mars: 'Ma',
  Mercury: 'Me',
  Jupiter: 'Ju',
  Venus: 'Ve',
  Saturn: 'Sa',
  Rahu: 'Ra',
  Ketu: 'Ke',
}

/**
 * Retrograde. A glyph, not a colour.
 *
 * "Never encode meaning in colour alone" is not a checkbox here: roughly
 * 8% of men have red-green colour blindness, and retrograde motion
 * changes how a placement is read. ℞ is the conventional mark and is
 * legible at small sizes.
 */
export const RETROGRADE_MARK = '℞'

/**
 * The order the nine grahas are named in. Not alphabetical.
 *
 * Sun, Moon, Mars, Mercury, Jupiter, Venus, Saturn, then the two lunar
 * nodes. It is the order of the Vimshottari sequence's parent scheme and
 * the order every panchanga, every printed kundli and every competing
 * product prints — so a reader comparing this screen against a paper
 * chart reads down two lists in step.
 *
 * ── Why this is needed at all ──
 *
 * The transit list arrived alphabetical: Ju, Ke, Ma, Me, Mo, Ra, Sa, Su,
 * Ve. That is not a display choice anybody made. `ListTransitsAt` is a
 * `SELECT DISTINCT ON (planet)`, and Postgres REQUIRES the DISTINCT ON
 * expression to be leftmost in ORDER BY — so `ORDER BY planet` is load
 * bearing for correctness and must not be touched to fix presentation.
 *
 * Hence sorting in the view. The natal table is unaffected because
 * astro-service already returns the grahas in this order; only rows that
 * have been through that query need re-ordering.
 */
export const GRAHA_ORDER = [
  'Sun',
  'Moon',
  'Mars',
  'Mercury',
  'Jupiter',
  'Venus',
  'Saturn',
  'Rahu',
  'Ketu',
] as const

const GRAHA_RANK: Record<string, number> = Object.fromEntries(
  GRAHA_ORDER.map((planet, index) => [planet, index]),
)

/**
 * Comparator for anything carrying a `planet` name.
 *
 * An unknown name sorts last rather than first: if astro-service ever
 * grows a tenth body, it appears at the end of a list the reader already
 * understands instead of displacing the Sun.
 */
export function byGrahaOrder(a: { planet: string }, b: { planet: string }): number {
  const rank = (planet: string) => GRAHA_RANK[planet] ?? GRAHA_ORDER.length
  return rank(a.planet) - rank(b.planet)
}

export function planetAbbreviation(planet: string): string {
  // Falls back to the first two letters rather than throwing. A planet
  // this map has not heard of should still appear on the chart — an
  // unknown name is a reason to render it plainly, not to lose it.
  return PLANET_ABBREVIATIONS[planet] ?? planet.slice(0, 2)
}

/**
 * The sentence a screen reader hears for one planet.
 *
 * Everything the glyph encodes visually, in words: position, house,
 * degree, and both of the marks that are otherwise carried by a glyph
 * and a ring. Degrees are rounded to whole numbers — a chart is read to
 * the degree, and "twelve point seven four one degrees" is noise.
 */
export function planetLabel(planet: {
  planet: string
  sign: string
  /** Null when the birth time is unknown — there is no house to name. */
  house: number | null
  degree: number
  isRetrograde: boolean
  isCombust: boolean
}): string {
  const parts = [`${planet.planet} in ${planet.sign}`]

  // Omitted, not rendered as "0th house" or as "no house". A reader
  // without a birth time is told once, on the screen, why the houses
  // are absent; repeating it on all nine planets is noise.
  if (planet.house !== null) parts.push(`${ordinal(planet.house)} house`)

  parts.push(`${Math.round(planet.degree)} degrees`)
  if (planet.isRetrograde) parts.push('retrograde')
  if (planet.isCombust) parts.push('combust')
  return parts.join(', ')
}

export function ordinal(value: number): string {
  // 11th, 12th and 13th are the exceptions every naive implementation
  // renders as 11st, 12nd and 13rd — and houses go up to 12.
  const lastTwo = value % 100
  if (lastTwo >= 11 && lastTwo <= 13) return `${value}th`

  switch (value % 10) {
    case 1:
      return `${value}st`
    case 2:
      return `${value}nd`
    case 3:
      return `${value}rd`
    default:
      return `${value}th`
  }
}
