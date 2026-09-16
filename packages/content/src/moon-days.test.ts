import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import { MOON_DAYS, MOON_SIGNS, moonDay } from './moon-days'
import { PREDICTIVE_PHRASES, type Entry, type Locale } from './types'

const LOCALES: Locale[] = ['en', 'hi']

function everyEntry(): Array<{ key: string; locale: Locale; entry: Entry }> {
  return MOON_SIGNS.flatMap((key) =>
    LOCALES.map((locale) => ({ key, locale, entry: MOON_DAYS[key][locale] })),
  )
}

function allText(entry: Entry): string {
  return `${entry.name} ${entry.short} ${entry.long}`
}

/**
 * The sign names are a contract with Python, exactly like the yoga keys.
 *
 * `GET /astrology/transits` returns `sign` as a display string and that
 * string is the lookup. "Scorpius" instead of "Scorpio" on either side
 * produces a Today card with a sign and no line — which reads as a day
 * nobody wrote copy for rather than as a bug.
 */
const CONSTANTS_PY = join(process.cwd(), '..', '..', 'services', 'astro', 'app', 'core', 'constants.py')

function engineSignNames(): string[] {
  const source = readFileSync(CONSTANTS_PY, 'utf8')
  const block = /SIGNS[^=]*=\s*\(([\s\S]*?)\)/.exec(source)
  if (!block) return []
  return [...block[1]!.matchAll(/"([A-Za-z]+)"/g)].map((m) => m[1]!)
}

describe('the engine contract', () => {
  it('has a line for every sign astro-service names', () => {
    expect(new Set(MOON_SIGNS)).toEqual(new Set(engineSignNames()))
  })

  // Proves the reader found the tuple rather than comparing two empty
  // sets, which is what a changed constant name would produce.
  it('actually read the twelve sign names', () => {
    const names = engineSignNames()
    expect(names.length, `found ${names.join(', ')}`).toBe(12)
    expect(names).toContain('Scorpio')
  })
})

describe('completeness', () => {
  it('covers all twelve signs in both locales', () => {
    expect(MOON_SIGNS).toHaveLength(12)

    const missing: string[] = []
    for (const sign of MOON_SIGNS) {
      for (const locale of LOCALES) {
        if (!MOON_DAYS[sign]?.[locale]) missing.push(`${sign}.${locale}`)
      }
    }
    expect(missing).toEqual([])
  })

  it('has genuinely translated Hindi, not copied English', () => {
    expect(MOON_SIGNS.filter((s) => MOON_DAYS[s].hi.long === MOON_DAYS[s].en.long)).toEqual([])
  })

  it('has Devanagari in every Hindi entry', () => {
    const devanagari = /[ऀ-ॿ]/
    expect(MOON_SIGNS.filter((s) => !devanagari.test(allText(MOON_DAYS[s].hi)))).toEqual([])
  })

  // A line that fits the card. The Today card gives it one or two lines
  // before it starts pushing the rest of the dashboard down a phone.
  it('keeps every short line short enough for the card', () => {
    const tooLong = everyEntry()
      .filter(({ entry }) => entry.short.length > 100)
      .map(({ key, locale, entry }) => `${key}.${locale} (${entry.short.length})`)
    expect(tooLong).toEqual([])
  })

  it('gives every sign a distinct line', () => {
    const shorts = MOON_SIGNS.map((s) => MOON_DAYS[s].en.short)
    expect(new Set(shorts).size, 'two signs share a line').toBe(12)
  })
})

describe('framing', () => {
  /**
   * This is the copy closest to being a horoscope, so the rule matters
   * most here.
   *
   * It is the same line for everyone alive that day and it is about the
   * sky, not about the reader. "Your day will be steady" is a prediction
   * about a stranger from a planet's position; "traditionally read as a
   * steady day" is a statement about a tradition.
   */
  it('never uses predictive framing', () => {
    const offences: string[] = []
    for (const { key, locale, entry } of everyEntry()) {
      const text = allText(entry).toLowerCase()
      for (const phrase of PREDICTIVE_PHRASES) {
        if (text.includes(phrase)) offences.push(`${key}.${locale}: "${phrase}"`)
      }
    }
    expect(offences).toEqual([])
  })

  it('never addresses the reader in the second person', () => {
    // "your day", "you should", "you may find" — every one of them turns
    // a note about the sky into a claim about a person.
    const secondPerson = /\byou\b|\byour\b|\byours\b/i

    const offences = MOON_SIGNS.filter((s) => secondPerson.test(allText(MOON_DAYS[s].en))).map(
      (s) => s,
    )
    expect(
      offences,
      'this line is the same for everyone alive today; addressing the reader makes it ' +
        'sound like a reading of their chart',
    ).toEqual([])
  })

  it('would catch second-person framing if it appeared', () => {
    const secondPerson = /\byou\b|\byour\b|\byours\b/i
    expect(secondPerson.test('Your day will be steady and practical.')).toBe(true)
    expect(secondPerson.test(MOON_DAYS.Taurus.en.long)).toBe(false)
  })

  it('hedges every line', () => {
    const hedged = /traditionally|tradition|classical|texts/i
    const unhedged = MOON_SIGNS.filter((s) => !hedged.test(allText(MOON_DAYS[s].en)))
    expect(unhedged).toEqual([])
  })

  /**
   * Scorpio is where the Moon is debilitated, so it is this file's
   * Kemadruma: the entry most likely to be written as bad news.
   *
   * ── The matcher does not understand negation, on purpose ──
   *
   * Scorpio first read "intense rather than unfortunate", and this test
   * flagged it — correctly by its own rule, since the word is there.
   * The tempting fix is to teach the pattern about "rather than" and
   * "not", which makes it cleverer and makes every future failure an
   * argument about whether the negation was really a negation.
   *
   * The copy was reworded instead. A rule that says "these words do not
   * appear" is one anybody can check by reading; a rule that says "these
   * words do not appear except when negated" is one nobody can.
   */
  it('does not characterise any day as bad', () => {
    const condemning = /\b(unfortunate|misfortune|suffer|bad|avoid|danger|beware|inauspicious)\b/i
    const offences = everyEntry()
      .filter(({ entry }) => condemning.test(allText(entry)))
      .map(({ key, locale }) => `${key}.${locale}`)
    expect(offences).toEqual([])
  })

  it('names the Scorpio debilitation rather than hiding it, and reads it as intense', () => {
    const long = MOON_DAYS.Scorpio.en.long

    // Named: concealing an unfavourable classical fact is its own kind
    // of dishonesty, and a reader who finds it elsewhere trusts the app
    // less for having omitted it.
    expect(long).toMatch(/debilitat/i)
    expect(long).toMatch(/intense/i)
  })
})

describe('lookup', () => {
  it('returns the line for a known sign', () => {
    expect(moonDay('Taurus', 'en')?.name).toBe('Moon in Taurus')
    expect(moonDay('Taurus', 'hi')?.name).toBe('वृषभ राशि में चंद्रमा')
  })

  it('returns null for a sign name it does not have', () => {
    expect(moonDay('Ophiuchus', 'en')).toBeNull()
    expect(() => moonDay('Ophiuchus', 'en')).not.toThrow()
  })

  it('is not fooled by inherited object properties', () => {
    for (const key of ['constructor', 'toString', '__proto__']) {
      expect(moonDay(key, 'en'), key).toBeNull()
    }
  })
})
