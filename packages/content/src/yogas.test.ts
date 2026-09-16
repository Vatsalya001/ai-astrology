import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import { YOGAS, YOGA_KEYS, describeYoga, hasYoga } from './yogas'
import { PREDICTIVE_PHRASES, type Entry, type Locale } from './types'

const LOCALES: Locale[] = ['en', 'hi']

function everyEntry(): Array<{ key: string; locale: Locale; entry: Entry }> {
  return YOGA_KEYS.flatMap((key) => LOCALES.map((locale) => ({ key, locale, entry: YOGAS[key][locale] })))
}

function allText(entry: Entry): string {
  return `${entry.name} ${entry.short} ${entry.long}`
}

/**
 * The keys are a contract with Python, and nothing else checks it.
 *
 * `YogaResult.name` arrives as a display string and that string is the
 * lookup key. Rename "Chandra-Mangal Yoga" to "Chandra Mangal Yoga" on
 * either side and the card renders a heading with no description —
 * which looks like a yoga nobody wrote about, not like a bug. It is the
 * same failure mode as Phase 2's chart type that never reached
 * astro-service: structurally perfect, silently wrong.
 *
 * Reading the Python is crude and it is a monorepo, so it works.
 */
const YOGA_PY = join(process.cwd(), '..', '..', 'services', 'astro', 'app', 'core', 'yoga.py')

function engineYogaNames(): string[] {
  const source = readFileSync(YOGA_PY, 'utf8')

  // Two shapes: `name="X Yoga"` on a Yoga(...), and the MAHAPURUSHA map
  // whose VALUES are the names. A pattern matching only the first misses
  // all five Panch Mahapurusha yogas, which is how this was written the
  // first time — it reported four names and looked fine.
  const direct = [...source.matchAll(/name="([^"]+Yoga)"/g)].map((m) => m[1]!)
  const mahapurusha = [...source.matchAll(/Planet\.\w+:\s*"([^"]+Yoga)"/g)].map((m) => m[1]!)

  return [...new Set([...direct, ...mahapurusha])]
}

describe('the engine contract', () => {
  it('describes every yoga astro-service can emit', () => {
    const undescribed = engineYogaNames().filter((name) => !hasYoga(name))

    expect(
      undescribed,
      'astro-service emits these and the corpus has no entry, so they render as a ' +
        'heading with no description — which reads as an oversight rather than a bug.',
    ).toEqual([])
  })

  it('describes nothing the engine cannot emit', () => {
    const engine = new Set(engineYogaNames())
    const orphans = YOGA_KEYS.filter((key) => !engine.has(key))

    expect(
      orphans,
      'these are described and unreachable — either the engine dropped a detector or ' +
        'the name drifted, and both are worth knowing about.',
    ).toEqual([])
  })

  /**
   * Proves the reader above found something, and found ALL of it.
   *
   * Written with only the `name="…"` pattern first, which matched four
   * names and reported a clean comparison against a corpus that happened
   * to contain those four. The five Panch Mahapurusha yogas live in a
   * dict whose values are the names and were invisible to it.
   */
  it('actually read the Python, including the Mahapurusha map', () => {
    const names = engineYogaNames()

    expect(names.length, `only found ${names.join(', ')}`).toBe(11)
    expect(names).toContain('Gajakesari Yoga')
    expect(names, 'the MAHAPURUSHA dict was not read').toContain('Hamsa Yoga')
  })
})

describe('completeness', () => {
  // The gate asks for at least ten described in both locales.
  it('describes at least ten yogas', () => {
    expect(YOGA_KEYS.length).toBeGreaterThanOrEqual(10)
  })

  it('has both locales for every yoga', () => {
    const missing: string[] = []
    for (const key of YOGA_KEYS) {
      for (const locale of LOCALES) {
        if (!YOGAS[key]?.[locale]) missing.push(`${key}.${locale}`)
      }
    }
    expect(missing).toEqual([])
  })

  it('has genuinely translated Hindi, not copied English', () => {
    const untranslated = YOGA_KEYS.filter((key) => YOGAS[key].hi.long === YOGAS[key].en.long)
    expect(untranslated).toEqual([])
  })

  it('has Devanagari in every Hindi entry', () => {
    const devanagari = /[ऀ-ॿ]/
    expect(YOGA_KEYS.filter((key) => !devanagari.test(allText(YOGAS[key].hi)))).toEqual([])
  })

  it('says what the configuration IS in every short form', () => {
    const thin = everyEntry()
      .filter(({ entry }) => entry.short.length < 15)
      .map(({ key, locale }) => `${key}.${locale}`)
    expect(thin).toEqual([])
  })

  it('keeps every short form short enough for a card', () => {
    const tooLong = everyEntry()
      .filter(({ entry }) => entry.short.length > 90)
      .map(({ key, locale, entry }) => `${key}.${locale} (${entry.short.length})`)
    expect(tooLong).toEqual([])
  })
})

describe('framing', () => {
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

  /**
   * The asymmetry that makes this corpus riskier than the glossary.
   *
   * Most of these are traditionally read as fortunate and one is
   * traditionally read as difficult, which is exactly where copy slips
   * into telling somebody their chart is bad. "The classical texts say
   * so" is not a defence when the classical texts were not written to be
   * delivered to a stranger by an app at two in the morning.
   */
  it('never characterises a combination as bad for the reader', () => {
    const condemning =
      /\b(unfortunate|misfortune|suffer|suffering|poverty|doomed|cursed|disaster|miserable|loneliness|lonely|deprived|ruin)\b/i

    const offences = everyEntry()
      .filter(({ entry }) => condemning.test(allText(entry)))
      .map(({ key, locale }) => `${key}.${locale}`)

    expect(
      offences,
      'a paragraph about misfortune delivered to a stranger is harm, not information',
    ).toEqual([])
  })

  // Proves the matcher still recognises the register it exists to catch.
  it('would catch a condemning description if one appeared', () => {
    const condemning =
      /\b(unfortunate|misfortune|suffer|suffering|poverty|doomed|cursed|disaster|miserable|loneliness|lonely|deprived|ruin)\b/i

    expect(condemning.test('This yoga brings poverty and loneliness.')).toBe(true)
    expect(condemning.test(YOGAS['Kemadruma Yoga'].en.long)).toBe(false)
  })

  /**
   * The difficult one carries its own cancellation.
   *
   * Every serious text on Kemadruma lists the placements that undo it,
   * and every frightening summary drops them. Including them is the
   * difference between a description and a verdict — and it is a fact
   * about the tradition, not a softening.
   */
  it('says Kemadruma is cancelled by common placements', () => {
    const entry = YOGAS['Kemadruma Yoga'].en.long
    expect(entry).toMatch(/cancel/i)
    expect(entry, 'the cancelling placements are named, not merely alluded to').toMatch(
      /Jupiter|angle from the Moon/i,
    )
  })

  it('hedges every interpretive claim in English', () => {
    const interpretive = /associated with|read for|read as|reads it for|signifies|indicates/i
    const hedged = /traditionally|tradition|conventionally|classical|texts/i

    const unhedged = YOGA_KEYS.filter((key) => {
      const text = allText(YOGAS[key].en)
      return interpretive.test(text) && !hedged.test(text)
    })

    expect(unhedged).toEqual([])
  })
})

describe('lookup', () => {
  it('returns the description for a known yoga', () => {
    expect(describeYoga('Raja Yoga', 'en')?.name).toBe('Raja Yoga')
    expect(describeYoga('Raja Yoga', 'hi')?.name).toBe('राज योग')
  })

  it('returns null for an unknown yoga rather than the name or an error', () => {
    expect(describeYoga('Not A Yoga', 'en')).toBeNull()
    expect(() => describeYoga('Not A Yoga', 'en')).not.toThrow()
  })

  it('is not fooled by inherited object properties', () => {
    for (const key of ['constructor', 'toString', '__proto__', 'hasOwnProperty']) {
      expect(hasYoga(key), key).toBe(false)
      expect(describeYoga(key, 'en'), key).toBeNull()
    }
  })
})
