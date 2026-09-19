import { describe, expect, it } from 'vitest'

import { dictionaries, en, getDictionary, interpolate, LOCALE_NAMES } from './dictionaries'

/**
 * The gate item is "i18n scaffolding in place; no hardcoded UI strings".
 *
 * A dictionary that exists proves nothing — PR 8a shipped one and every
 * screen still called `getDictionary('en')` directly, which is a
 * hardcoded language wearing a dictionary's clothes. These assert the
 * properties that make it real.
 */

describe('every locale is complete', () => {
  // TypeScript already enforces this at compile time via `Dictionary`.
  // Asserting it at runtime too catches the case where someone satisfies
  // the type with an empty string to make the build pass.
  function flatten(obj: object, prefix = ''): Array<[string, string]> {
    return Object.entries(obj).flatMap(([key, value]) =>
      typeof value === 'string'
        ? [[`${prefix}${key}`, value] as [string, string]]
        : flatten(value as object, `${prefix}${key}.`),
    )
  }

  const englishKeys = flatten(en).map(([k]) => k)

  for (const [code, dict] of Object.entries(dictionaries)) {
    it(`${code} has every key English has`, () => {
      const keys = flatten(dict).map(([k]) => k)
      expect(keys.sort()).toEqual(englishKeys.sort())
    })

    it(`${code} has no empty strings`, () => {
      for (const [key, value] of flatten(dict)) {
        expect(value.trim(), `${code}.${key} is empty`).not.toBe('')
      }
    })
  }
})

describe('translations are actually translated', () => {
  // A copy-paste of the English is the failure mode that looks finished.
  // Proper nouns and shared words legitimately match, so this checks the
  // proportion rather than demanding every string differ.
  it('hi differs from en for most strings', () => {
    const enValues = Object.values(en.settings)
    const hiValues = Object.values(dictionaries.hi.settings)

    const identical = enValues.filter((v, i) => v === hiValues[i]).length
    expect(
      identical / enValues.length,
      'most Hindi settings strings are identical to the English — likely an untranslated copy',
    ).toBeLessThan(0.2)
  })

  it('option values are translated, not just their labels', () => {
    // "ज्योतिष पद्धति: Vedic / Western" reads as broken rather than
    // bilingual. This was shipped that way once.
    expect(dictionaries.hi.values.vedic).not.toBe(en.values.vedic)
    expect(dictionaries.hi.values.north).not.toBe(en.values.north)
    expect(dictionaries.hi.values.dark).not.toBe(en.values.dark)
  })
})

describe('locale selection', () => {
  it('falls back to English for anything unknown', () => {
    for (const bad of ['fr', '', 'EN', 'hi-IN', 'xx']) {
      expect(getDictionary(bad)).toBe(en)
    }
  })

  it('resolves the locales it does support', () => {
    expect(getDictionary('en')).toBe(en)
    expect(getDictionary('hi')).toBe(dictionaries.hi)
  })

  it('names each language in its own script', () => {
    // A language picker written in a language you cannot read is not a
    // picker.
    expect(LOCALE_NAMES.en).toBe('English')
    expect(LOCALE_NAMES.hi).toBe('हिन्दी')
  })
})

describe('interpolation', () => {
  it('substitutes placeholders', () => {
    expect(interpolate('Welcome, {name}', { name: 'Priya' })).toBe('Welcome, Priya')
    expect(interpolate('Resend in {seconds}s', { seconds: 30 })).toBe('Resend in 30s')
  })

  it('leaves an unknown placeholder visible rather than blank', () => {
    // A silently-empty gap reads as a rendering bug with no clue; the
    // literal token points at the missing value.
    expect(interpolate('Hi {missing}', {})).toBe('Hi {missing}')
  })

  it('substitutes every occurrence', () => {
    expect(interpolate('{a} and {a}', { a: 'x' })).toBe('x and x')
  })

  it('works on the real templates that use it', () => {
    expect(interpolate(en.home.greetWithName, { greeting: 'Hello', name: 'Priya' })).toContain(
      'Priya',
    )
    expect(interpolate(en.verify.resendIn, { seconds: 12 })).toContain('12')
    expect(interpolate(en.settings.deleteScheduled, { date: '2026-09-22' })).toContain('2026-09-22')
    // And in Hindi, where the placeholder sits in a different position.
    expect(
      interpolate(dictionaries.hi.home.greetWithName, { greeting: 'नमस्ते', name: 'प्रिया' }),
    ).toContain('प्रिया')
  })
})

/**
 * "a antardasha".
 *
 * The dasha screen said it for months. The string was
 * `'Choose a {parent} above to see its periods.'` — one template with a
 * hardcoded article, interpolating either "mahadasha" (correct) or
 * "antardasha" (not). No amount of interpolation fixes that, because the
 * article belongs to the word rather than the sentence, and adding an
 * `{article}` placeholder would export an English grammar rule into
 * every other locale — Hindi has no indefinite article at all.
 *
 * It is the kind of error a reader notices instantly and a reviewer
 * never does, because the template reads correctly in isolation and the
 * broken form only exists at runtime.
 *
 * Checked against the dictionary VALUES rather than the source file, so
 * the prose above — which quotes the mistake — is not itself a failure.
 * Two other guards in this repo had to learn that the hard way.
 */
describe('English indefinite articles', () => {
  /*
    a/e/i/o only. "u" is excluded wholesale: "a user", "a unique chart"
    and "a union" are all correct, because the rule is about the SOUND
    and "u" is the letter where sound and spelling disagree most often.
    A guard that fires on those gets silenced.
  */
  const WRONG_ARTICLE = /\ba (?=[aeio])/gi

  // Same exception in the other direction: these begin with a vowel and
  // take "a", because they are pronounced with a leading consonant.
  const CONSONANT_SOUNDED = /^(one|once|eu)/i

  function flat(obj: object, prefix = ''): Array<[string, string]> {
    return Object.entries(obj).flatMap(([key, value]) =>
      typeof value === 'string'
        ? [[`${prefix}${key}`, value] as [string, string]]
        : flat(value as object, `${prefix}${key}.`),
    )
  }

  it('never writes "a" before a vowel sound', () => {
    const offences: string[] = []

    for (const [key, value] of flat(en)) {
      WRONG_ARTICLE.lastIndex = 0
      let match: RegExpExecArray | null
      while ((match = WRONG_ARTICLE.exec(value)) !== null) {
        const following = value.slice(match.index + match[0].length)
        if (CONSONANT_SOUNDED.test(following)) continue
        offences.push(`${key}: "…${value.slice(match.index, match.index + 28)}…"`)
      }
    }

    expect(
      offences,
      'these read "a" where English needs "an". Usually a sign that an ' +
        'article was hardcoded into a template whose placeholder can hold ' +
        'more than one word — split the string per case rather than ' +
        'interpolating grammar.',
    ).toEqual([])
  })

  it('would catch the string that shipped', () => {
    // The guard, broken on purpose. Without this, a regex that silently
    // stopped matching would leave the suite green and the bug live.
    const shipped = 'Choose a antardasha above to see its periods.'
    WRONG_ARTICLE.lastIndex = 0
    expect(WRONG_ARTICLE.test(shipped)).toBe(true)

    WRONG_ARTICLE.lastIndex = 0
    expect(WRONG_ARTICLE.test(en.chart.dashaChooseAntardasha)).toBe(false)
  })
})
