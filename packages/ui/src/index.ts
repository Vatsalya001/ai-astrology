/**
 * Design tokens shared by web and (from Phase 10) mobile.
 *
 * COMPONENTS are deliberately NOT shared. react-native-web and similar
 * "write once" approaches produce components that are mediocre on both
 * platforms and a build system nobody understands. Share tokens, types
 * and logic; write platform-native UI.
 *
 * The one worthwhile exception is chart geometry (Phase 3), which is
 * pure maths with no React in it and ports directly to react-native-svg.
 */

/**
 * The palette. Midnight navy, deep purple, gold.
 *
 * Should read as a premium technology product that happens to be about
 * astrology — not a cheap astrology app.
 *
 * This file is the single source. apps/web/tailwind.config.ts imports
 * `colors` and `semanticColors` from here rather than restating them —
 * a mirrored palette drifts the first time someone edits one side, and
 * Tailwind gives no warning when it does.
 */
export const colors = {
  base: '#0B1026',
  surface: '#141B35',
  elevated: '#1C2545',
  border: '#252F52',

  // `soft` was #8B6FD8, which failed AA as badge text — 4.48:1 on base
  // and 3.55:1 on a card, even accounting for the badge's own bg-accent/10
  // tint. Lightness raised; hue (256°) and saturation unchanged.
  // `foreground` pairs with `bg-accent` in shadcn components.
  accent: { DEFAULT: '#6B4FBB', soft: '#9D85DE', dim: '#4A3580', foreground: '#F2F3F8' },
  gold: { DEFAULT: '#D4A857', soft: '#E5C078', dim: '#9A7A3E' },

  // `faint` was #5E6785 until a contrast check was actually run: 3.36:1
  // on base and 3.03:1 on surface, against the 4.5:1 AA needs. It is used
  // for real prose — the footer disclaimer, the feature-flag note — not
  // just decoration, so it has to clear AA rather than AA-large.
  // Lightness raised; hue (226°) and saturation unchanged.
  ink: { DEFAULT: '#F2F3F8', muted: '#9AA3C0', faint: '#858DA8' },

  /*
    The chart's structural lines.

    Not `border`. `border` is #252F52, which sits at 1.30:1 against the
    chart's `surface` background — measured, not estimated. WCAG 1.4.11
    asks for 3:1 on "graphical objects required to understand the
    content", and the twelve-house grid is the single most load-bearing
    graphic in this product: without it the diamond is a field of
    floating abbreviations.

    #5469B0 is 3.25:1 on surface and 3.61:1 on base. Same hue (226°) and
    saturation as the rest of the navy family, lightness raised until it
    cleared — so the chart reads as drawn rather than as washed out,
    without introducing a colour from outside the palette.

    ── Why the background was NOT lightened instead ──

    That was the first instinct and the measurements refused it. Lifting
    `surface` from #141B35 to #1E2847 drops `ink-faint` from 5.15:1 to
    4.40:1 — below AA — while the planet glyphs lose 2 points of an
    already-comfortable 15:1. The dark field is what makes the text
    legible; the lines were the problem all along.
  */
  grid: '#5469B0',

  /*
    Ink for paper, which is the one place this palette inverts.

    Every screen in the product is midnight navy with near-white text.
    The Phase 3 PDF is a sheet of A4 that someone may actually print, and
    printing a navy page costs a cartridge to produce something harder to
    read than the default.

    So `inkPrint` is a dark charcoal rather than pure black: #1A1F33 is
    the navy base lifted to a printable lightness, which keeps the brand
    hue on the page instead of dropping to a generic document. 14.8:1 on
    white, so the footer's 60%-opacity disclaimer still clears AA.

    Named separately rather than nested under `ink` because it is not a
    shade of the same thing — it is the opposite end, and `text-ink-print`
    on a navy screen would be invisible.

    The key is QUOTED and hyphenated on purpose. Tailwind derives the
    class name from the key verbatim, so `inkPrint` would produce
    `text-inkPrint` — and `text-ink-print`, which is what reads correctly
    in a component, would then name no colour at all. Tailwind drops an
    unknown colour class silently: tsc sees a valid string, next build
    succeeds, and the text renders in the inherited colour. There is a
    computed-style assertion in the e2e suite because nothing else
    catches this.
  */
  'ink-print': '#1A1F33',

  ok: '#4ADE80',
  warn: '#FBBF24',
  danger: '#F87171',
} as const

/**
 * The shadcn/ui semantic layer, derived from the palette above.
 *
 * shadcn components are written against semantic names (`bg-primary`,
 * `text-muted-foreground`). These map onto the palette rather than
 * introducing a second set of hexes, so one token change reaches both
 * vocabularies.
 *
 * Derived, not retyped: writing `colors.gold.DEFAULT` here means a
 * palette edit cannot leave the semantic layer pointing at the old
 * value. Tailwind drops a class naming an unknown colour silently, so
 * that drift would be invisible to `tsc` and to the build.
 */
export const semanticColors = {
  background: colors.base,
  foreground: colors.ink.DEFAULT,

  card: { DEFAULT: colors.surface, foreground: colors.ink.DEFAULT },
  popover: { DEFAULT: colors.elevated, foreground: colors.ink.DEFAULT },

  // Gold is the call-to-action colour throughout the product.
  primary: { DEFAULT: colors.gold.DEFAULT, foreground: colors.base },
  secondary: { DEFAULT: colors.surface, foreground: colors.ink.DEFAULT },

  muted: { DEFAULT: colors.elevated, foreground: colors.ink.muted },
  destructive: { DEFAULT: colors.danger, foreground: colors.base },

  input: colors.border,
  ring: colors.gold.DEFAULT,
} as const

/** 4px base scale. */
export const spacing = {
  xs: 4, sm: 8, md: 16, lg: 24, xl: 32, '2xl': 48, '3xl': 64,
} as const

export const radii = { sm: 8, md: 12, lg: 16, xl: 24, full: 9999 } as const

export const fonts = {
  sans: 'Inter',
  serif: 'Cormorant Garamond',
  mono: 'JetBrains Mono',
} as const

/**
 * Minimum contrast against `base`, per WCAG AA for body text.
 *
 * Recorded as a value rather than a comment so a future token change can
 * be checked against it programmatically.
 */
export const MIN_CONTRAST_RATIO = 4.5

/**
 * Status colours never carry meaning alone.
 *
 * Roughly 8% of men have red-green colour blindness, and they read
 * status pages too — so every status pairs a colour with a glyph.
 */
export const statusGlyphs = {
  ok: '●',
  degraded: '◐',
  error: '○',
  pending: '·',
} as const
