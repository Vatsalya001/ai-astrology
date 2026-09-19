import { describe, expect, it } from 'vitest'

import { colors } from './index'

/**
 * The palette, against WCAG.
 *
 * ── Why this is a test and not a note in a design file ──
 *
 * Colours get nudged. Somebody darkens a surface to make a card "pop",
 * or dims a label because it felt loud, and nothing in the build
 * notices — the change is valid TypeScript, valid CSS, and renders
 * fine on the designer's bright monitor. It surfaces months later as
 * "why can't I read this on my phone outdoors".
 *
 * The ratios below were measured rather than assumed, and two of them
 * were wrong when first measured. See `grid`.
 */

/** Relative luminance, per WCAG 2.1 §relativeluminancedef. */
function luminance(hex: string): number {
  const h = hex.replace('#', '')
  const channel = (offset: number) => {
    const c = parseInt(h.slice(offset, offset + 2), 16) / 255
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * channel(0) + 0.7152 * channel(2) + 0.0722 * channel(4)
}

/** Contrast ratio, per WCAG 2.1 §contrast-ratiodef. */
export function contrast(foreground: string, background: string): number {
  const a = luminance(foreground)
  const b = luminance(background)
  const [hi, lo] = a > b ? [a, b] : [b, a]
  return (hi + 0.05) / (lo + 0.05)
}

const BASE = colors.base
const SURFACE = colors.surface
const ELEVATED = colors.elevated

describe('text contrast', () => {
  /*
    AA is 4.5:1 for body text. Every pairing here is real — each one
    appears somewhere in the product, and the comment says where, so a
    failure names a screen rather than a hex code.
  */
  const cases: Array<[string, string, string]> = [
    ['ink on base — body copy everywhere', colors.ink.DEFAULT, BASE],
    ['ink on surface — planet glyphs in the chart', colors.ink.DEFAULT, SURFACE],
    ['ink on elevated — text on cards', colors.ink.DEFAULT, ELEVATED],
    ['ink-muted on base — secondary copy', colors.ink.muted, BASE],
    ['ink-muted on surface — house numbers in the chart', colors.ink.muted, SURFACE],
    ['ink-faint on base — footers, disclaimers', colors.ink.faint, BASE],
    ['gold on base — links and the current period', colors.gold.DEFAULT, BASE],
    ['gold on surface', colors.gold.DEFAULT, SURFACE],
  ]

  for (const [what, fg, bg] of cases) {
    it(`${what} clears AA`, () => {
      const ratio = contrast(fg, bg)
      expect(
        ratio,
        `${fg} on ${bg} is ${ratio.toFixed(2)}:1, below the 4.5:1 AA floor`,
      ).toBeGreaterThanOrEqual(4.5)
    })
  }
})

describe('non-text contrast', () => {
  /**
   * WCAG 1.4.11: graphical objects needed to understand the content.
   *
   * The twelve-house grid is the most load-bearing graphic in this
   * product — without it the chart is a field of floating two-letter
   * abbreviations with no structure at all. So it is held to 3:1, not
   * treated as decoration.
   *
   * It FAILED when first measured. The grid was drawn with `border`
   * (#252F52), which is 1.30:1 against the chart's surface — visible on
   * a good monitor in a dark room and essentially absent otherwise. The
   * `grid` token exists because of that measurement.
   */
  it('the chart grid is visible against the chart background', () => {
    const ratio = contrast(colors.grid, SURFACE)
    expect(
      ratio,
      `the chart's grid lines are ${ratio.toFixed(2)}:1 against its background. ` +
        `WCAG 1.4.11 asks 3:1 for graphics required to understand the content, ` +
        `and the house grid is exactly that — without it the chart is ` +
        `abbreviations floating in a void.`,
    ).toBeGreaterThanOrEqual(3)
  })

  it('the chart grid is visible against the page background too', () => {
    // The centre polygon is filled with `base`, not `surface`, so the
    // lines cross both.
    expect(contrast(colors.grid, BASE)).toBeGreaterThanOrEqual(3)
  })

  /*
    The guard that makes the one above meaningful.

    Asserting only "grid clears 3:1" would pass if somebody set it to
    pure white — which clears every ratio and destroys the design. This
    pins the other end: the grid must stay STRUCTURALLY subordinate to
    the planet glyphs, which are the content.
  */
  it('the grid stays quieter than the planets it frames', () => {
    const grid = contrast(colors.grid, SURFACE)
    const glyphs = contrast(colors.ink.DEFAULT, SURFACE)
    expect(
      grid,
      'the grid is as loud as the planet glyphs; the chart reads as a cage ' +
        'with text in it rather than as positions on a frame',
    ).toBeLessThan(glyphs / 2)
  })
})

describe('the palette stays in one family', () => {
  /*
    Every colour is a navy, a gold or a state colour. A stray hue reads
    as a bug even when its contrast is fine — and "I just needed a
    blue" is how a design system stops being one.

    Checked as a hue range rather than an allowlist of hex values, so
    adjusting a lightness does not require editing this test.
  */
  function hue(hex: string): number {
    const h = hex.replace('#', '')
    const [r, g, b] = [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16) / 255)
    const max = Math.max(r!, g!, b!)
    const min = Math.min(r!, g!, b!)
    if (max === min) return 0
    const d = max - min
    let deg: number
    if (max === r) deg = ((g! - b!) / d) % 6
    else if (max === g) deg = (b! - r!) / d + 2
    else deg = (r! - g!) / d + 4
    return (deg * 60 + 360) % 360
  }

  it('the grid is the same navy as the rest of the structure', () => {
    const gridHue = hue(colors.grid)
    const borderHue = hue(colors.border)
    expect(
      Math.abs(gridHue - borderHue),
      `the grid is hue ${gridHue.toFixed(0)}° and the border it replaced is ` +
        `${borderHue.toFixed(0)}° — the grid was lightened, not recoloured, ` +
        `so these should be within a few degrees`,
    ).toBeLessThan(12)
  })
})
