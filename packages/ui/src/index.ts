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
