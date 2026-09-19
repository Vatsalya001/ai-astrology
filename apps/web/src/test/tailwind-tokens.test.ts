import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import defaultColors from 'tailwindcss/colors'

import tailwindConfig from '../../tailwind.config'

/**
 * Tailwind colour classes that do not mean what they say.
 *
 * Two failures shipped to a user on the same screen, and neither `tsc`,
 * `next build`, ESLint nor the visual-regression suite noticed either.
 * Both are invisible in a diff because the class name reads correctly.
 *
 * ── 1. A colour token that collides with a built-in utility ──
 *
 * Tailwind ships `text-base` as a FONT SIZE. The palette also had a
 * colour named `base`, so `text-base` was generated a second time as
 * `color: #0B1026` — and that one won.
 *
 * `#0B1026` is the page background. Every `<Input>` in the product sets
 * `text-base` deliberately (iOS Safari zooms the viewport for any field
 * under 16px and strands the user at that zoom), so every input painted
 * its text the exact colour of the page behind it. Contrast 1:1. You
 * typed and nothing appeared.
 *
 * ── 2. A colour class naming a token that does not exist ──
 *
 * `bg-surface-2` was used in five places. There is no `surface-2`.
 * Tailwind drops an unknown colour class in silence, so the birth-place
 * dropdown had no background at all and its suggestions were painted
 * straight onto the page beneath them.
 *
 * `frontend.md` already warns about the second. Neither warning is a
 * gate, which is what this file is for.
 */

const SRC = join(process.cwd(), 'src')

function tsxFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const path = join(dir, entry)
    // Skipped BEFORE recursing: the guards below quote the very class
    // names they reject, and a test that fails on its own error message
    // is a test nobody can keep.
    if (path === join(SRC, 'test')) return []
    if (statSync(path).isDirectory()) return tsxFiles(path)
    return path.endsWith('.tsx') || path.endsWith('.ts') ? [path] : []
  })
}

/** Flattens a colour map into the suffixes Tailwind generates. */
function flatten(palette: Record<string, unknown>, into: Set<string>): void {
  for (const [key, value] of Object.entries(palette)) {
    if (typeof value === 'string') {
      into.add(key)
      continue
    }
    if (value === null || typeof value !== 'object') continue
    // Nested scales: `gold.DEFAULT` is `gold`, `gold.soft` is `gold-soft`.
    for (const shade of Object.keys(value as Record<string, unknown>)) {
      into.add(shade === 'DEFAULT' ? key : `${key}-${shade}`)
    }
  }
}

/**
 * Every name a `bg-*` class may legitimately carry.
 *
 * Three sources, and all three are needed — leaving any one out makes
 * the test cry wolf, and a guard people learn to ignore is worse than
 * none:
 *
 *   - the project palette
 *   - Tailwind's DEFAULT palette, which `extend` keeps (the print route
 *     is deliberately `bg-white`, because paper is)
 *   - `theme.extend.backgroundImage`, which shares the `bg-` prefix —
 *     `bg-radial-glow` is a gradient, not a colour
 */
function backgroundNames(): Set<string> {
  const names = new Set<string>()
  flatten((tailwindConfig.theme?.extend?.colors ?? {}) as Record<string, unknown>, names)
  flatten(defaultColors as unknown as Record<string, unknown>, names)
  for (const key of Object.keys(tailwindConfig.theme?.extend?.backgroundImage ?? {})) {
    names.add(key)
  }
  return names
}

/** Just the project palette — for the collision check. */
function paletteNames(): Set<string> {
  const names = new Set<string>()
  flatten((tailwindConfig.theme?.extend?.colors ?? {}) as Record<string, unknown>, names)
  return names
}

describe('no colour token collides with a built-in utility', () => {
  /*
    Tailwind's font-size scale. A colour sharing one of these names
    produces two rules for one class, and which wins is a function of
    emission order rather than anything a reader could predict.

    This is the specific list because `text-*` is where the collision is
    both most likely and most damaging: it is the one namespace whose
    built-in values are short, generic English words.
  */
  const FONT_SIZES = [
    'xs', 'sm', 'base', 'lg', 'xl',
    '2xl', '3xl', '4xl', '5xl', '6xl', '7xl', '8xl', '9xl',
  ]

  it('none is named after a font size', () => {
    const collisions = [...paletteNames()].filter((name) => FONT_SIZES.includes(name))

    expect(
      collisions,
      'these colour tokens generate a `text-<name>` that collides with ' +
        "Tailwind's font-size utility of the same name. Both rules are " +
        'emitted and the colour wins, so any component using the class for ' +
        'sizing silently gets a colour it never asked for. Rename the token, ' +
        'or expose it through semanticColors only — see tailwind.config.ts.',
    ).toEqual([])
  })

  it('the rule would have caught the token that shipped', () => {
    // The guard, broken on purpose: `base` is exactly what this existed
    // to reject, and a version of this test that passed on it would be
    // worse than no test.
    expect(FONT_SIZES.includes('base')).toBe(true)
  })
})

describe('every bg- colour class names a real token', () => {
  /*
    `bg-` only, and deliberately so.

    It is the namespace where the silent drop actually bit, and its
    non-colour values are a short closed list — so this check is precise
    rather than noisy. `text-` would need to distinguish colours from
    sizes, alignments and transforms, and a guard that cries wolf is one
    people learn to skip.
  */
  const NON_COLOUR_BG = new Set([
    'transparent', 'current', 'inherit', 'none',
    'fixed', 'local', 'scroll',
    'clip', 'origin', 'blend',
    'bottom', 'center', 'left', 'right', 'top',
    'repeat', 'no-repeat',
    'auto', 'cover', 'contain',
  ])

  /*
    Utilities whose value carries further segments: `bg-gradient-to-r`,
    `bg-clip-text`, `bg-origin-border`. The capture is greedy, so these
    arrive whole and an exact-match set never sees them.
  */
  const NON_COLOUR_BG_PREFIXES = ['gradient', 'clip', 'origin', 'blend']

  it('no component references a colour the palette does not define', () => {
    const palette = backgroundNames()
    const unknown: string[] = []

    for (const file of tsxFiles(SRC)) {
      const source = readFileSync(file, 'utf8')

      for (const match of source.matchAll(/\bbg-([a-z][a-z0-9-]*)\b/g)) {
        const token = match[1]!
        // Opacity modifiers (`bg-gold/10`) are stripped by the pattern
        // already; prefixed variants (`hover:bg-gold`) match the bare
        // name because \b sits after the colon.
        if (NON_COLOUR_BG.has(token)) continue
        if (NON_COLOUR_BG_PREFIXES.some((p) => token.startsWith(`${p}-`))) continue
        if (palette.has(token)) continue
        // Arbitrary values, e.g. bg-[#0F1530].
        if (token.startsWith('[')) continue
        unknown.push(`${file.replace(SRC, 'src')}: bg-${token}`)
      }
    }

    expect(
      [...new Set(unknown)],
      'these name a colour the Tailwind palette does not define. Tailwind ' +
        'drops such a class in silence: the element renders with NO ' +
        'background, `tsc` is happy and the build succeeds. This is how the ' +
        'birth-place dropdown shipped transparent.',
    ).toEqual([])
  })
})
