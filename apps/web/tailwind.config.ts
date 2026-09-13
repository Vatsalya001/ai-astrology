import type { Config } from 'tailwindcss'

/**
 * Design tokens for the AI Astrology Companion.
 *
 * The brief from the spec: modern, premium, mystical, trustworthy,
 * minimal, warm — and explicitly NOT a cheap astrology app. The palette
 * below is midnight navy with deep purple and gold, which reads as a
 * premium technology product that happens to be about astrology.
 *
 * These values are the single source of truth. Do not hardcode a hex
 * anywhere in a component.
 */
const config: Config = {
  content: ['./src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        base: '#0B1026',      // midnight navy — page background
        surface: '#141B35',   // raised cards
        elevated: '#1C2545',  // hover / nested surfaces
        border: '#252F52',

        accent: {
          DEFAULT: '#6B4FBB', // deep purple
          soft: '#8B6FD8',
          dim: '#4A3580',
        },
        gold: {
          DEFAULT: '#D4A857',
          soft: '#E5C078',
          dim: '#9A7A3E',
        },

        ink: {
          DEFAULT: '#F2F3F8', // primary text
          muted: '#9AA3C0',   // secondary text
          faint: '#5E6785',   // tertiary / disabled
        },

        ok: '#4ADE80',
        warn: '#FBBF24',
        danger: '#F87171',
      },

      fontFamily: {
        sans: ['var(--font-sans)', 'system-ui', 'sans-serif'],
        serif: ['var(--font-serif)', 'Georgia', 'serif'],
        mono: ['var(--font-mono)', 'ui-monospace', 'monospace'],
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
  plugins: [],
}

export default config
