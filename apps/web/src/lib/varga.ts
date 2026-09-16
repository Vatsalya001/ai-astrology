import type { GlossaryKey } from '@ayana/content'

/**
 * The divisional charts the product shows, in one place.
 *
 * In `lib/` rather than in `components/chart/` because both the API
 * client and the switcher need it, and a client importing from a
 * component directory is the wrong direction.
 *
 * `astro-service` computes more divisionals than these; D1, D9 and D10
 * are what Phase 3 renders, and the switcher is driven by this list so
 * adding a fourth is one entry rather than a component change.
 */

export interface Varga {
  /** The wire value. Must match `services/api/internal/charts/service.go`. */
  type: string
  /** Sanskrit name, which is what a reader of these charts looks for. */
  name: string
  /**
   * Glossary key, so the name itself is tappable.
   *
   * `GlossaryKey`, not `string`: the switcher passes this straight to
   * `AstroTerm`, which renders an unknown term as plain text without
   * complaining. The source scan in `astro-term-usage.test.ts` cannot
   * follow a computed prop, so the type carries the guarantee instead.
   */
  term: GlossaryKey
  /** What this chart is read for, in one line. */
  purpose: string
}

/**
 * ── Why the purpose line is here at all ──
 *
 * "D9" means nothing to somebody seeing their chart for the first time,
 * and a switcher offering three unexplained codes invites the reader to
 * press one, see a completely different diagram, and conclude the app is
 * broken. The Sanskrit name plus one line of what it is read for is the
 * difference between a control and a puzzle.
 *
 * The framing is traditional, not predictive, for the same reason the
 * glossary is: Phase 5 grounds a model in this copy too.
 */
export const VARGAS: readonly Varga[] = [
  {
    type: 'D1',
    name: 'Rasi',
    term: 'rasi',
    purpose: 'The birth chart itself — everything else is derived from it.',
  },
  {
    type: 'D9',
    name: 'Navamsa',
    term: 'navamsa',
    purpose: 'Traditionally read for marriage, partnership and inner strength.',
  },
  {
    type: 'D10',
    name: 'Dasamsa',
    term: 'dasamsa',
    purpose: 'Traditionally read for career, profession and public standing.',
  },
] as const

export type VargaType = (typeof VARGAS)[number]['type']

export const DEFAULT_VARGA = 'D1'

export function varga(type: string): Varga | undefined {
  return VARGAS.find((v) => v.type === type)
}
