import { describe, expect, it } from 'vitest'

import { defineTerm, GLOSSARY, GLOSSARY_KEYS, hasTerm } from './index'
import { PREDICTIVE_PHRASES, type Entry, type Locale } from './types'

/**
 * The safety posture starts in the static copy, not at the model.
 *
 * That line is the spec's, and it is the reason this file exists. In
 * Phase 5 this corpus becomes the seed of the RAG knowledge base — the
 * text a model is grounded in and paraphrases back. A corpus written in
 * "you will marry in 2027" teaches every later summariser to speak that
 * way, and no amount of prompt engineering reliably unteaches it.
 *
 * So the framing rule is enforced mechanically here, on every entry, in
 * every locale, rather than trusted to whoever writes the next one.
 */

const LOCALES: Locale[] = ['en', 'hi']

function everyEntry(): Array<{ key: string; locale: Locale; entry: Entry }> {
  return GLOSSARY_KEYS.flatMap((key) =>
    LOCALES.map((locale) => ({ key, locale, entry: GLOSSARY[key][locale] })),
  )
}

function allText(entry: Entry): string {
  return `${entry.name} ${entry.short} ${entry.long}`
}

describe('framing', () => {
  // "You will" states a fact about somebody's future. "Is traditionally
  // associated with" states a fact about a tradition. The first is a
  // claim this product cannot support, and in several domains must not
  // make at all.
  it('never uses predictive framing', () => {
    const offences: string[] = []

    for (const { key, locale, entry } of everyEntry()) {
      const text = allText(entry).toLowerCase()
      for (const phrase of PREDICTIVE_PHRASES) {
        if (text.includes(phrase)) {
          offences.push(`${key}.${locale} contains "${phrase}"`)
        }
      }
    }

    expect(
      offences,
      'Predictive framing in the corpus is not a style problem. Phase 5 grounds a ' +
        'model in this text, and a model paraphrases the voice it is given.',
    ).toEqual([])
  })

  // Both guards are worthless if their patterns stopped matching
  // anything real. These prove they still fire on text shaped like the
  // corpus.
  it('would catch an unhedged interpretive claim if it appeared', () => {
    const interpretive = /associated with|read for|read as|signifies|indicates/i
    const hedged = /traditionally|tradition|conventionally|is said to|are said to/i

    const planted = 'The tenth house signifies a successful career.'
    expect(interpretive.test(planted)).toBe(true)
    expect(hedged.test(planted)).toBe(false)
  })

  it('would catch predictive framing if it appeared', () => {
    const planted = 'This placement means you will receive money in 2027.'.toLowerCase()
    const caught = PREDICTIVE_PHRASES.filter((phrase) => planted.includes(phrase))

    expect(caught, 'the matcher no longer recognises a plainly predictive sentence')
      .toContain('you will')
  })

  // A hedge is what makes a claim about a tradition rather than about
  // the reader. Not every entry needs one — "a quarter of a nakshatra"
  // asserts nothing about anybody — but every entry that makes an
  // interpretive claim does.
  it('hedges every interpretive claim in English', () => {
    // Five verbs, and deliberately NOT bare "means".
    //
    // The first version included it and flagged two entries that say
    // «"Rasi chart" also means the main birth chart» and «the name means
    // "one hundred and twenty"». Both are LEXICAL — a glossary defining
    // a word — not claims connecting a chart feature to someone's life.
    //
    // A guard with false positives is one people weaken or delete, so
    // it went. The residual gap is a sentence like "this placement means
    // good fortune", which is unhedged and would pass. That shape has
    // not appeared, and the predictive-phrase list above catches the
    // dangerous version of it ("means you will…"). If it ever does
    // appear, tighten this to the subject rather than the verb.
    const interpretive = /associated with|read for|read as|signifies|indicates/i
    const hedged = /traditionally|tradition|conventionally|is said to|are said to/i

    const unhedged = GLOSSARY_KEYS.filter((key) => {
      const text = allText(GLOSSARY[key].en)
      return interpretive.test(text) && !hedged.test(text)
    })

    expect(
      unhedged,
      'these entries make an interpretive claim without attributing it to the ' +
        'tradition, which states it as fact about the reader instead',
    ).toEqual([])
  })
})

describe('completeness', () => {
  // The spec asks for roughly sixty terms. The count is asserted so that
  // deleting the awkward ones — the ones nobody wants to write Hindi for
  // — is a failing test rather than a quiet shrink.
  it('defines at least sixty terms', () => {
    expect(GLOSSARY_KEYS.length).toBeGreaterThanOrEqual(60)
  })

  it('has both locales for every term', () => {
    const missing: string[] = []
    for (const key of GLOSSARY_KEYS) {
      for (const locale of LOCALES) {
        if (!GLOSSARY[key]?.[locale]) missing.push(`${key}.${locale}`)
      }
    }
    expect(missing).toEqual([])
  })

  // A Hindi entry that is still the English string is worse than a
  // missing one: it looks translated, so nobody goes back to it.
  it('has genuinely translated Hindi, not copied English', () => {
    const untranslated = GLOSSARY_KEYS.filter(
      (key) => GLOSSARY[key].hi.long === GLOSSARY[key].en.long,
    )
    expect(
      untranslated,
      'the Hindi long form is byte-identical to the English one — an entry that ' +
        'looks translated is one nobody revisits',
    ).toEqual([])
  })

  it('has Devanagari in every Hindi entry', () => {
    const devanagari = /[ऀ-ॿ]/
    const latinOnly = GLOSSARY_KEYS.filter((key) => !devanagari.test(allText(GLOSSARY[key].hi)))
    expect(latinOnly).toEqual([])
  })

  // Both forms are required. An entry with only a long form cannot be
  // shown where users meet it — a tooltip — and one with only a short
  // form has nothing behind the tap.
  it('gives every entry a name, a short form and a long form', () => {
    const incomplete: string[] = []
    for (const { key, locale, entry } of everyEntry()) {
      if (!entry.name.trim()) incomplete.push(`${key}.${locale}.name`)
      if (!entry.short.trim()) incomplete.push(`${key}.${locale}.short`)
      if (!entry.long.trim()) incomplete.push(`${key}.${locale}.long`)
    }
    expect(incomplete).toEqual([])
  })

  // A "short" form longer than the tooltip it goes in is a long form
  // wearing the wrong name, and it will be truncated on a phone.
  it('keeps every short form short enough for a tooltip', () => {
    const tooLong = everyEntry()
      .filter(({ entry }) => entry.short.length > 90)
      .map(({ key, locale, entry }) => `${key}.${locale} (${entry.short.length} chars)`)
    expect(tooLong).toEqual([])
  })

  it('says something in every long form', () => {
    const thin = everyEntry()
      .filter(({ entry }) => entry.long.length < 40)
      .map(({ key, locale }) => `${key}.${locale}`)
    expect(thin).toEqual([])
  })
})

describe('lookup', () => {
  it('returns the entry for a known term', () => {
    expect(defineTerm('sade_sati', 'en')?.name).toBe('Sade Sati')
    expect(defineTerm('sade_sati', 'hi')?.name).toBe('साढ़े साती')
  })

  // Null, not the key and not a throw. A missing definition renders as
  // no tooltip; rendering "sade_sati_rising" mid-sentence is worse than
  // rendering nothing, and crashing on a screen somebody is reading is
  // worse than both.
  it('returns null for an unknown term rather than the key or an error', () => {
    expect(defineTerm('not_a_term', 'en')).toBeNull()
    expect(() => defineTerm('not_a_term', 'en')).not.toThrow()
  })

  it('reports whether a term exists without materialising it', () => {
    expect(hasTerm('nakshatra')).toBe(true)
    expect(hasTerm('not_a_term')).toBe(false)
  })

  // Object.prototype has these; a naive `GLOSSARY[key]` check would say
  // yes to all of them and then hand a Function to a tooltip.
  it('is not fooled by inherited object properties', () => {
    for (const key of ['constructor', 'toString', '__proto__', 'hasOwnProperty']) {
      expect(hasTerm(key), key).toBe(false)
      expect(defineTerm(key, 'en'), key).toBeNull()
    }
  })
})
