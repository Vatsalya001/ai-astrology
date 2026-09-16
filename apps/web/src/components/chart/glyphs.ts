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
  house: number
  degree: number
  isRetrograde: boolean
  isCombust: boolean
}): string {
  const parts = [
    `${planet.planet} in ${planet.sign}`,
    `${ordinal(planet.house)} house`,
    `${Math.round(planet.degree)} degrees`,
  ]
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
