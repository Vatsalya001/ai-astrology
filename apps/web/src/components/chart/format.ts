import type { GlossaryKey } from '@ayana/content'

/**
 * Turning chart numbers into the strings a chart is read in.
 *
 * Pure and separate from the components, because the interesting bugs
 * here are arithmetic and a rendering test would bury them.
 */

/**
 * A degree within a sign, as degrees and arcminutes: `15°14'`.
 *
 * ── Truncated, not rounded ──
 *
 * Both conventions are defensible in general; only one is correct here.
 * A sign spans [0°, 30°) — there is no 30th degree of Leo. Rounding the
 * arcminutes carries: 29.9999° gives 59.994', which rounds to 60', which
 * has to become `30°00'` — a position that does not exist, printed on a
 * chart, at the exact boundary where a planet is about to change sign
 * and the reader is most likely to be looking.
 *
 * Truncation cannot carry, and it also matches how the tradition reads a
 * degree: 15°14' means "in the 16th degree of the sign", which is a
 * statement about the interval the planet is inside, not about the
 * nearest whole minute.
 *
 * ── Multiply first, and then a nudge ──
 *
 * The obvious spelling takes the fractional part and multiplies it:
 * `Math.floor((degree - Math.floor(degree)) * 60)`. That subtraction
 * loses low bits, and the loss is downward — `8.7 - 8` is
 * `0.6999999999999993`, so an exact 8°42' truncates to 8°41'.
 *
 * Measured across all 1800 exact arcminute positions in a sign:
 *
 *   subtract then multiply   790 wrong by one arcminute   (44%)
 *   multiply first            24 wrong by one arcminute
 *   multiply first + 1e-9      0
 *
 * So: one multiplication, then a nudge far below any precision the
 * ephemeris offers (1e-9 arcminutes is 6e-8 arcseconds) and far above
 * double-rounding noise at this magnitude. It cannot push 29.9999° over
 * the carry — that is 1799.994 arcminutes, nowhere near 1800.
 */
export function formatDegree(degree: number): string {
  const totalMinutes = Math.floor(degree * 60 + 1e-9)
  const whole = Math.floor(totalMinutes / 60)
  const minutes = totalMinutes % 60
  return `${whole}°${String(minutes).padStart(2, '0')}'`
}

/**
 * The closed dignity vocabulary from `astro-service`, as English and as
 * a glossary key.
 *
 * `neutral` deliberately has no glossary key: "neutral" is not a term
 * of art, it is the absence of one, and a tappable definition reading
 * "the planet is not exalted, debilitated, in its own sign or in
 * moolatrikona" teaches nobody anything. It renders as an em dash, the
 * way printed charts leave the column blank.
 */
export const DIGNITIES: Record<string, { label: string; term: GlossaryKey | null }> = {
  exalted: { label: 'Exalted', term: 'exalted' },
  debilitated: { label: 'Debilitated', term: 'debilitated' },
  own_sign: { label: 'Own sign', term: 'own_sign' },
  moolatrikona: { label: 'Moolatrikona', term: 'moolatrikona' },
  neutral: { label: '—', term: null },
}

/**
 * An unknown dignity renders as itself rather than as nothing.
 *
 * `astro-service` owns this vocabulary and could extend it. Swallowing
 * a value this map has not heard of would silently blank a column that
 * the engine had something to say in; showing the raw string is ugly
 * for one release and visible to whoever has to fix it.
 */
export function dignity(value: string): { label: string; term: GlossaryKey | null } {
  return DIGNITIES[value] ?? { label: value, term: null }
}

/**
 * `Rohini 2` — nakshatra and pada.
 *
 * Pada is part of the reading, not decoration: it selects the navamsa
 * sign, so a nakshatra without its pada is a third of the information.
 */
export function formatNakshatra(nakshatra: string, pada: number): string {
  return `${nakshatra} ${pada}`
}
