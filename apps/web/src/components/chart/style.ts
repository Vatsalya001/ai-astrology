import type { ChartStyle } from '@ayana/astrology-geometry'

/**
 * Which chart styles the product can actually draw.
 *
 * ── The gap this closes ──
 *
 * `user_preferences.chart_style` is a free string, the settings screen
 * offered `['north', 'south', 'east']`, and the dictionary translates
 * "East" into both locales — while `@ayana/astrology-geometry` exports
 * `ChartStyle = 'north' | 'south'` and has no East Indian geometry at
 * all.
 *
 * So a reader could pick East, have it saved, and — the moment a chart
 * screen existed — be handed a style nothing knows how to render. It was
 * invisible only because no screen mounted `ChartSVG` until now.
 *
 * East Indian is a real style and belongs in the product; it is a
 * different polygon set in the geometry package, which is where it has
 * to be built. Until then the picker does not offer it, because offering
 * a choice the product cannot honour is worse than offering two.
 */
export const CHART_STYLES: readonly ChartStyle[] = ['north', 'south'] as const

export const DEFAULT_CHART_STYLE: ChartStyle = 'north'

/**
 * A stored preference, narrowed to something renderable.
 *
 * The fallback is not defensive programming for its own sake: accounts
 * created before this change can already hold `'east'`, and the value
 * arrives from the API as a `string`. Returning the default beats
 * rendering nothing, and beats throwing inside a render.
 *
 * It falls back SILENTLY on purpose. A banner saying "your chosen style
 * is unavailable" on every chart view would be correct and useless — the
 * reader cannot act on it, and the honest fix is to build the style.
 */
export function toChartStyle(stored: string | null | undefined): ChartStyle {
  return CHART_STYLES.includes(stored as ChartStyle)
    ? (stored as ChartStyle)
    : DEFAULT_CHART_STYLE
}
