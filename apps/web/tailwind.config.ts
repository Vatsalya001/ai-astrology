import type { Config } from 'tailwindcss'
import animate from 'tailwindcss-animate'
import { colors, semanticColors } from '@ayana/ui'

/**
 * Design tokens for the AI Astrology Companion.
 *
 * The brief from the spec: modern, premium, mystical, trustworthy,
 * minimal, warm — and explicitly NOT a cheap astrology app. The palette
 * below is midnight navy with deep purple and gold, which reads as a
 * premium technology product that happens to be about astrology.
 *
 * The values themselves live in `packages/ui` and are imported below —
 * the spec requires them defined once, and mobile (Phase 10) needs the
 * same palette without a Tailwind dependency. Do not hardcode a hex
 * anywhere in a component, and do not restate one here.
 */
/*
  `base` is deliberately NOT a Tailwind colour.

  Tailwind ships `text-base` as a FONT SIZE (1rem). Naming a colour
  `base` generates a second `text-base` — this time `color: #0B1026` —
  and the two collide on the same class name. The colour wins.

  The effect was invisible in review and catastrophic in use: every
  input in the product sets `text-base` on purpose, because iOS Safari
  zooms the viewport for any field under 16px and strands the user
  there. So every one of them was ALSO painting its text #0B1026, which
  is `base` — the page background. Users typed into a box and watched
  nothing appear. Contrast ratio 1:1.

  Nothing catches this. `tsc` sees a valid string, Tailwind emits both
  rules happily, `next build` succeeds, and the class name looks correct
  in the diff. It is the same family of failure as an unknown colour
  class being dropped, which `frontend.md` already warns about, except
  louder: the class is not dropped, it is honoured twice with different
  meanings.

  The colour is still reachable as `bg-background` / `text-foreground`
  through semanticColors, which is the name a component should be using
  anyway — and `colors.base` is untouched for TypeScript callers such as
  the contrast tests.
*/
const { base: _base, ...paletteWithoutBase } = colors

const config: Config = {
  content: ['./src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        ...paletteWithoutBase,
        ...semanticColors,
      },

      fontFamily: {
        sans: ['var(--font-sans)', 'system-ui', 'sans-serif'],
        serif: ['var(--font-serif)', 'Georgia', 'serif'],
        mono: ['var(--font-mono)', 'ui-monospace', 'monospace'],
      },

      borderRadius: {
        lg: '0.75rem',
        md: '0.5rem',
        sm: '0.375rem',
      },

      backgroundImage: {
        'radial-glow':
          'radial-gradient(circle at 50% 0%, rgba(107,79,187,0.22), transparent 62%)',
        'gold-line':
          'linear-gradient(90deg, transparent, rgba(212,168,87,0.55), transparent)',
      },

      keyframes: {
        twinkle: {
          '0%, 100%': { opacity: '0.15' },
          '50%': { opacity: '0.85' },
        },
        'fade-up': {
          from: { opacity: '0', transform: 'translateY(12px)' },
          to: { opacity: '1', transform: 'translateY(0)' },
        },
        orbit: {
          from: { transform: 'rotate(0deg)' },
          to: { transform: 'rotate(360deg)' },
        },
        'pulse-ring': {
          '0%': { transform: 'scale(0.95)', opacity: '0.7' },
          '70%': { transform: 'scale(1.3)', opacity: '0' },
          '100%': { transform: 'scale(1.3)', opacity: '0' },
        },
      },
      animation: {
        twinkle: 'twinkle 4s ease-in-out infinite',
        'fade-up': 'fade-up 0.5s ease-out both',
        orbit: 'orbit 24s linear infinite',
        'pulse-ring': 'pulse-ring 2s cubic-bezier(0.4, 0, 0.6, 1) infinite',
      },
    },
  },
  plugins: [animate],
}

export default config
