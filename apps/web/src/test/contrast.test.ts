import { describe, expect, it } from 'vitest'
import { colors, semanticColors, MIN_CONTRAST_RATIO } from '@ayana/ui'

/**
 * WCAG contrast for every text token, against every surface it is
 * actually painted on.
 *
 * `MIN_CONTRAST_RATIO` was declared in packages/ui with the comment
 * "recorded as a value rather than a comment so a future token change
 * can be checked against it programmatically" — and then nothing checked
 * it. The first time it was computed, `ink.faint` came out at 3.36:1 on
 * base and 3.03:1 on surface, well under the 4.5:1 it claimed.
 *
 * `text-ink-faint` is not decoration. It carries the footer's medical
 * disclaimer, the hero's "sign-up opens in Phase 1", and the status
 * page's note about feature flags. Prose at 3:1 is unreadable for a
 * large number of people and merely uncomfortable for everyone else.
 */

// Written without array indexing on purpose: tsconfig has
// noUncheckedIndexedAccess, so `linear[0]` is `number | undefined` and
// the arithmetic does not compile. Vitest does not typecheck, so this
// only surfaced at `next build`.
function channel(hex: string, offset: number): number {
  const raw = parseInt(hex.replace('#', '').slice(offset, offset + 2), 16) / 255
  return raw <= 0.03928 ? raw / 12.92 : ((raw + 0.055) / 1.055) ** 2.4
}

/** WCAG 2.1 relative luminance. */
function luminance(hex: string): number {
  return (
    0.2126 * channel(hex, 0) + 0.7152 * channel(hex, 2) + 0.0722 * channel(hex, 4)
  )
}

function contrast(a: string, b: string): number {
  const la = luminance(a)
  const lb = luminance(b)
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05)
}

/** Every background a token may legitimately sit on. */
const SURFACES = {
  base: colors.base,
  surface: colors.surface,
  elevated: colors.elevated,
}

describe('the contrast helper itself', () => {
  // Anchors: if these drift, every assertion below is meaningless.
  it('reports 21:1 for black on white', () => {
    expect(contrast('#000000', '#FFFFFF')).toBeCloseTo(21, 1)
  })

  it('reports 1:1 for a colour against itself', () => {
    expect(contrast('#0B1026', '#0B1026')).toBeCloseTo(1, 5)
  })

  it('is symmetric', () => {
    expect(contrast('#F2F3F8', '#0B1026')).toBeCloseTo(
      contrast('#0B1026', '#F2F3F8'),
      5,
    )
  })
})

describe('text tokens meet WCAG AA on every surface', () => {
  const textTokens = {
    'ink.DEFAULT': colors.ink.DEFAULT,
    'ink.muted': colors.ink.muted,
    'ink.faint': colors.ink.faint,
    gold: colors.gold.DEFAULT,
    'gold.soft': colors.gold.soft,
    'accent.soft': colors.accent.soft,
    ok: colors.ok,
    warn: colors.warn,
    danger: colors.danger,
  }

  for (const [tokenName, token] of Object.entries(textTokens)) {
    for (const [surfaceName, surface] of Object.entries(SURFACES)) {
      it(`${tokenName} on ${surfaceName}`, () => {
        const ratio = contrast(token, surface)
        expect(
          ratio,
          `${tokenName} (${token}) on ${surfaceName} (${surface}) is ${ratio.toFixed(2)}:1, below the ${MIN_CONTRAST_RATIO}:1 this project commits to`,
        ).toBeGreaterThanOrEqual(MIN_CONTRAST_RATIO)
      })
    }
  }
})

describe('semantic foreground/background pairs', () => {
  // shadcn pairs these by construction, so a mismatch here ships as
  // unreadable button text rather than as a missing style.
  const pairs: Array<[string, string, string]> = [
    ['primary', semanticColors.primary.foreground, semanticColors.primary.DEFAULT],
    ['secondary', semanticColors.secondary.foreground, semanticColors.secondary.DEFAULT],
    ['destructive', semanticColors.destructive.foreground, semanticColors.destructive.DEFAULT],
    ['card', semanticColors.card.foreground, semanticColors.card.DEFAULT],
    ['popover', semanticColors.popover.foreground, semanticColors.popover.DEFAULT],
    ['muted', semanticColors.muted.foreground, semanticColors.muted.DEFAULT],
    ['accent', colors.accent.foreground, colors.accent.DEFAULT],
  ]

  for (const [name, fg, bg] of pairs) {
    it(`${name} foreground is readable on ${name}`, () => {
      const ratio = contrast(fg, bg)
      expect(
        ratio,
        `${name}: ${fg} on ${bg} is ${ratio.toFixed(2)}:1`,
      ).toBeGreaterThanOrEqual(MIN_CONTRAST_RATIO)
    })
  }
})

describe('the semantic layer is derived, not retyped', () => {
  // The whole point of deriving semanticColors from colors is that a
  // palette edit cannot leave the semantic layer pointing at a stale
  // hex. Tailwind drops unknown colour classes silently, so that drift
  // would be invisible to both `tsc` and the build.
  it('maps onto the palette rather than introducing new values', () => {
    expect(semanticColors.background).toBe(colors.base)
    expect(semanticColors.foreground).toBe(colors.ink.DEFAULT)
    expect(semanticColors.primary.DEFAULT).toBe(colors.gold.DEFAULT)
    expect(semanticColors.muted.foreground).toBe(colors.ink.muted)
    expect(semanticColors.destructive.DEFAULT).toBe(colors.danger)
    expect(semanticColors.input).toBe(colors.border)
    expect(semanticColors.ring).toBe(colors.gold.DEFAULT)
  })
})

describe('translucent backgrounds composite before they are judged', () => {
  /**
   * Badges paint their text over `bg-<tone>/10`, not over the raw
   * surface. Checking the token against the surface alone measures a
   * combination that never renders — and in the accent badge's case it
   * was the composite that failed hardest: 3.55:1 over a card.
   */
  function composite(fg: string, alpha: number, bg: string): string {
    const raw = (hex: string, offset: number) =>
      parseInt(hex.replace('#', '').slice(offset, offset + 2), 16)
    const mix = (offset: number) =>
      Math.round(raw(fg, offset) * alpha + raw(bg, offset) * (1 - alpha))
    return (
      '#' + [0, 2, 4].map((o) => mix(o).toString(16).padStart(2, '0')).join('')
    )
  }

  it('composites correctly at the extremes', () => {
    expect(composite('#FFFFFF', 1, '#000000')).toBe('#ffffff')
    expect(composite('#FFFFFF', 0, '#000000')).toBe('#000000')
  })

  // tone → [text, tint] as declared in components/ui.tsx
  const badgeTones: Array<[string, string, string]> = [
    ['accent', colors.accent.soft, colors.accent.DEFAULT],
    ['gold', colors.gold.soft, colors.gold.DEFAULT],
    ['ok', colors.ok, colors.ok],
    ['warn', colors.warn, colors.warn],
    ['danger', colors.danger, colors.danger],
  ]

  for (const [tone, text, tint] of badgeTones) {
    for (const [surfaceName, surface] of Object.entries(SURFACES)) {
      it(`${tone} badge text over its own tint on ${surfaceName}`, () => {
        const background = composite(tint, 0.1, surface)
        const ratio = contrast(text, background)
        expect(
          ratio,
          `${tone} badge: ${text} on composite ${background} is ${ratio.toFixed(2)}:1`,
        ).toBeGreaterThanOrEqual(MIN_CONTRAST_RATIO)
      })
    }
  }
})
