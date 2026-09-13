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
 * Mirrored in apps/web/tailwind.config.ts. Change both together.
 */
export const colors = {
  base: '#0B1026',
  surface: '#141B35',
  elevated: '#1C2545',
  border: '#252F52',

  accent: { DEFAULT: '#6B4FBB', soft: '#8B6FD8', dim: '#4A3580' },
  gold: { DEFAULT: '#D4A857', soft: '#E5C078', dim: '#9A7A3E' },

  ink: { DEFAULT: '#F2F3F8', muted: '#9AA3C0', faint: '#5E6785' },

  ok: '#4ADE80',
  warn: '#FBBF24',
  danger: '#F87171',
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
