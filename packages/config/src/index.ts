/**
 * Constants shared across the TypeScript apps.
 *
 * Values that must agree between web and mobile, and that are not
 * secrets and not server-owned. Anything the server decides — feature
 * flags, plan limits — comes from /api/v1/meta instead, so it can change
 * without shipping a client.
 */

/** Where the API lives. The clients talk ONLY to the Go API. */
export const API_URL =
  process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:4000'

/**
 * Request timeouts, in milliseconds.
 *
 * Chart generation is slower than a normal read because it may be a cold
 * computation in astro-service. Chat is far slower still: it is a
 * streaming LLM response, and cutting it off at a normal timeout would
 * truncate answers mid-sentence.
 */
export const TIMEOUTS = {
  default: 8_000,
  chartGeneration: 30_000,
  chat: 120_000,
} as const

/** Supported interface languages. Hinglish is a first-class option. */
export const LOCALES = ['en', 'hi', 'hinglish'] as const
export type Locale = (typeof LOCALES)[number]
export const DEFAULT_LOCALE: Locale = 'en'

/**
 * Chart rendering styles.
 *
 * Not cosmetic: North Indian users expect a diamond and South Indian
 * users expect a grid. Showing the wrong one makes the product feel
 * foreign, so this is chosen per user rather than picked globally.
 */
export const CHART_STYLES = ['north', 'south', 'east'] as const
export type ChartStyle = (typeof CHART_STYLES)[number]
export const DEFAULT_CHART_STYLE: ChartStyle = 'north'

/** Divisional charts the UI can render. D1 is the birth chart. */
export const CHART_TYPES = ['D1', 'D9', 'D10'] as const
export type ChartType = (typeof CHART_TYPES)[number]
