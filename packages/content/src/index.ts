export { GLOSSARY, GLOSSARY_KEYS, type GlossaryKey } from './glossary'
export { YOGAS, YOGA_KEYS, describeYoga, hasYoga, type YogaKey } from './yogas'
export {
  PREDICTIVE_PHRASES,
  SENSITIVE_DOMAINS,
  type Entry,
  type Locale,
  type Localised,
} from './types'

import { GLOSSARY, type GlossaryKey } from './glossary'
import type { Entry, Locale } from './types'

/**
 * Look up a glossary term.
 *
 * Returns `null` rather than throwing or falling back to the key. A
 * missing definition should render as no tooltip at all, not as the
 * string "sade_sati_rising" in the middle of a sentence — and not as a
 * crash on a screen the user was reading.
 *
 * That silence has a cost: a typo in a term key renders as an ordinary
 * word and nothing reports it. The gate requires every term used in the
 * UI to resolve, and the guard lives where the call sites are —
 * `apps/web/src/components/astro-term-usage.test.ts` reads them out of
 * source and checks each against this corpus.
 */
export function defineTerm(key: string, locale: Locale): Entry | null {
  const entry = (GLOSSARY as Record<string, Record<Locale, Entry>>)[key]
  return entry?.[locale] ?? null
}

/** Whether a key has a definition, without materialising it. */
export function hasTerm(key: string): key is GlossaryKey {
  return Object.prototype.hasOwnProperty.call(GLOSSARY, key)
}
