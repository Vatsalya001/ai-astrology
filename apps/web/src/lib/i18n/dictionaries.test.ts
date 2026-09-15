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
    expect(interpolate(en.home.welcomeNamed, { name: 'Priya' })).toContain('Priya')
    expect(interpolate(en.verify.resendIn, { seconds: 12 })).toContain('12')
    expect(interpolate(en.settings.deleteScheduled, { date: '2026-09-22' })).toContain('2026-09-22')
    // And in Hindi, where the placeholder sits in a different position.
    expect(interpolate(dictionaries.hi.home.welcomeNamed, { name: 'प्रिया' })).toContain('प्रिया')
  })
})
