/**
 * Written astrology content: glossary, yoga descriptions, house and
 * planet meanings.
 *
 * This is EDITORIAL content, not AI output. Two reasons it lives here
 * rather than being generated:
 *
 *   1. It is free and instant. No token cost, no latency, and no
 *      hallucination risk on text that never changes.
 *   2. In Phase 5 this same corpus seeds the RAG knowledge base, so
 *      writing it as structured, sourced content now saves rebuilding
 *      it later.
 *
 * Framing rule, applied without exception: traditional ("is
 * traditionally associated with"), never predictive ("you will"). The
 * safety posture starts in the static copy, not at the model.
 *
 * Phase 3 fills this out to ~60 glossary terms and ~10 yogas in both
 * English and Hindi.
 */

import type { Locale } from '@ayana/config'

export interface LocalisedText {
  /** One line, shown inline. */
  short: string
  /** A paragraph, shown in a detail sheet. */
  long?: string
}

export interface GlossaryEntry {
  key: string
  term: Record<Locale, string>
  definition: Record<Locale, LocalisedText>
  /** Where the term comes from, for the Phase 5 knowledge base. */
  source?: string
}

/**
 * Seed entries. "Combust" is here first on purpose: it is the term most
 * likely to appear on a user's chart while meaning nothing to them, and
 * a one-tap definition is the difference between a chart that impresses
 * and one that intimidates.
 */
export const GLOSSARY: readonly GlossaryEntry[] = [
  {
    key: 'combust',
    term: { en: 'Combust', hi: 'अस्त', hinglish: 'Combust (Ast)' },
    definition: {
      en: {
        short: 'A planet close enough to the Sun to be hidden by its light.',
        long:
          'When a planet sits within a few degrees of the Sun it cannot be seen from Earth. ' +
          'Classical texts read this as the planet’s significations being obscured rather ' +
          'than absent — present in the chart, but working quietly.',
      },
      hi: { short: 'सूर्य के अत्यंत निकट होने से ग्रह का अस्त होना।' },
      hinglish: { short: 'Jab koi graha Sun ke bahut paas hota hai aur "chhup" jaata hai.' },
    },
    source: 'editorial',
  },
  {
    key: 'ascendant',
    term: { en: 'Ascendant', hi: 'लग्न', hinglish: 'Lagna' },
    definition: {
      en: {
        short: 'The zodiac sign rising on the eastern horizon at the moment of birth.',
        long:
          'The ascendant anchors the whole chart: it defines the first house, and every other ' +
          'house is counted from it. It moves roughly one degree every four minutes, which is ' +
          'why an accurate birth time matters so much.',
      },
      hi: { short: 'जन्म के समय पूर्वी क्षितिज पर उदय होती राशि।' },
      hinglish: { short: 'Janam ke waqt jo rashi east mein rise ho rahi thi.' },
    },
    source: 'editorial',
  },
] as const

export function glossaryEntry(key: string): GlossaryEntry | undefined {
  return GLOSSARY.find((e) => e.key === key)
}
