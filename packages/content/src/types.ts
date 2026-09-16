/**
 * The static interpretation layer.
 *
 * Written content, not model output. Two reasons from the spec, and a
 * third that matters more than either:
 *
 *   1. Free and instant — no token cost, no latency, and no chance of a
 *      hallucination on text that never changes.
 *   2. Phase 5's RAG knowledge base is seeded from exactly this corpus,
 *      so writing it as structured, typed content now is work that is
 *      not repeated later.
 *   3. **The safety posture starts here.** The AI rules forbid
 *      predictive framing and guaranteed outcomes; a corpus written in
 *      "you will marry in 2027" teaches every later summariser to speak
 *      that way, because it is the text the model is grounded in.
 *      `safety.test.ts` enforces the framing mechanically rather than
 *      trusting each entry's author.
 */

/**
 * The locales this corpus is written in.
 *
 * Deliberately NOT `@ayana/config`'s `Locale`, which declares a third —
 * `hinglish` — that nothing in the product currently reaches: the
 * language picker is driven by `LOCALE_NAMES`, keyed off the web app's
 * `dictionaries`, and those are `en` and `hi`.
 *
 * Importing the config type would make `Record<Locale, Entry>` demand a
 * Hinglish entry for all sixty-odd terms, which would get satisfied by
 * copying the English — content that looks translated is content nobody
 * revisits, and `safety.test.ts` already refuses that for Hindi.
 *
 * So the divergence is real and intentional, and it is held by a test
 * rather than by this comment: `apps/web/src/components/AstroTerm.test.tsx`
 * asserts the corpus covers every locale the picker can offer. That
 * matters because `AstroTerm` degrades a term with no entry to plain
 * text, silently — correct for one missing word, catastrophic for a
 * whole locale. Adding a dictionary for Hinglish fails that test on the
 * same commit, which is when the corpus needs to grow.
 */
export type Locale = 'en' | 'hi'

/**
 * One piece of written content.
 *
 * `short` is what fits on a card or in a tooltip; `long` is the detail
 * sheet. Both are required — an entry with only a long form cannot be
 * shown in the place users actually meet it, and one with only a short
 * form has nothing behind the tap.
 */
export interface Entry {
  name: string
  short: string
  long: string
}

/** Every entry, in every locale. */
export type Localised<Key extends string> = Record<Key, Record<Locale, Entry>>

/**
 * Framing the corpus must never use.
 *
 * Exported so the safety test and any future authoring tool share one
 * list rather than drifting into two. Matched case-insensitively as
 * whole phrases.
 *
 * These are not stylistic preferences. "You will" states a fact about
 * someone's future; "is traditionally associated with" states a fact
 * about a tradition. The first is a claim this product cannot support
 * and in several domains must not make at all.
 */
export const PREDICTIVE_PHRASES = [
  'you will',
  'will happen',
  'is certain',
  'is guaranteed',
  'guarantees',
  'destined to',
  'must marry',
  'will die',
  'will be cured',
  'will recover',
  'will conceive',
  'will win the case',
  'will become rich',
] as const

/**
 * Domains where even a hedged prediction is off limits.
 *
 * From the AI rules: no guaranteed outcomes on medical, marriage,
 * pregnancy, death, legal or financial matters. The corpus may name
 * these areas — a house genuinely signifies marriage — but may not
 * assert an outcome in them.
 */
export const SENSITIVE_DOMAINS = [
  'medical',
  'marriage',
  'pregnancy',
  'death',
  'legal',
  'financial',
] as const
