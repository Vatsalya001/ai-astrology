import { readdirSync, readFileSync, statSync } from 'node:fs'
import { extname, join } from 'node:path'

import { describe, expect, it } from 'vitest'

import { hasTerm } from '@ayana/content'

/**
 * Task 3.12's acceptance condition: "every jargon term in the UI resolves".
 *
 * `AstroTerm` degrades an unknown term to plain text without throwing,
 * without logging and without a visual difference a reviewer would spot
 * — which is the right behaviour and the reason a typo in a term key is
 * invisible. `<AstroTerm term="nakshtra">` renders a perfectly ordinary
 * word on a perfectly ordinary page and nothing anywhere complains.
 *
 * So the call sites are read from source and checked against the corpus.
 * A scan rather than a type because `term` is deliberately `string`: the
 * degradation path has to stay reachable for it to be worth testing, and
 * narrowing the prop to `GlossaryKey` would make the unknown-term case
 * unrepresentable and the test above unwritable.
 *
 * Non-literal usages (`term={key}`) cannot be resolved this way. They
 * are collected separately and asserted empty, so the first one is a
 * decision somebody makes on purpose rather than a hole that opens
 * quietly.
 */

// `process.cwd()`, not `import.meta.url`. Vitest transforms this module
// before jsdom runs it, and `import.meta.url` is not a file: URL by the
// time it gets here — `fileURLToPath` throws "The URL must be of scheme
// file" and the whole suite fails to collect. A suite that never runs
// reports zero failures, which is how this nearly shipped green.
const SRC = join(process.cwd(), 'src')

function sourceFiles(dir: string): string[] {
  const out: string[] = []
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) {
      out.push(...sourceFiles(path))
      continue
    }
    if (!['.ts', '.tsx'].includes(extname(name))) continue
    // The AstroTerm tests use unresolvable keys on purpose — that is
    // what they are testing.
    if (name.includes('.test.')) continue
    out.push(path)
  }
  return out
}

/** `<AstroTerm term="x"` with either quote style. */
const LITERAL = /<AstroTerm[^>]*?\bterm=["']([^"']+)["']/gs

/**
 * `<AstroTerm term={...}` — a value this scan cannot follow.
 *
 * No `g` flag, on purpose. A global regex carries `lastIndex` between
 * calls, so `DYNAMIC.test(a) && DYNAMIC.test(b)` returns true then
 * FALSE for two files that both match: the second search resumes past
 * the end of the first string. This was written with `/gs` and passed
 * its break test because that test planted exactly one offending file.
 * `matchAll` below needs its `g`; `.test()` must not have one.
 */
const DYNAMIC = /<AstroTerm[^>]*?\bterm=\{/s

function scan() {
  const literals: Array<{ file: string; term: string }> = []
  const dynamic: string[] = []

  for (const file of sourceFiles(SRC)) {
    const text = readFileSync(file, 'utf8')
    const relative = file.slice(SRC.length)

    for (const match of text.matchAll(LITERAL)) {
      literals.push({ file: relative, term: match[1]! })
    }
    if (DYNAMIC.test(text)) dynamic.push(relative)
  }

  return { literals, dynamic }
}

describe('AstroTerm usage', () => {
  it('uses only terms the glossary defines', () => {
    const unresolved = scan()
      .literals.filter(({ term }) => !hasTerm(term))
      .map(({ file, term }) => `${file}: "${term}"`)

    expect(
      unresolved,
      'AstroTerm renders an unknown term as plain text with nothing to report it, so a ' +
        'typo in a term key ships silently. Add the entry to @ayana/content, or fix ' +
        'the spelling.',
    ).toEqual([])
  })

  /**
   * Files where `term={…}` is computed, and why each is safe anyway.
   *
   * The rule for being on this list is not "it looked fine". It is that
   * the value's SOURCE is typed `GlossaryKey`, so a typo is a compile
   * error — a stronger guarantee than this scan gives — and that a test
   * checks the mapping itself, because a well-typed key can still point
   * at the wrong entry.
   *
   * This list started empty and every entry was added by this test
   * failing on a real commit, which is the only way it should grow.
   */
  const COMPUTED_TERMS_ALLOWED: Record<string, string> = {
    '/components/chart/PlanetTable.tsx':
      'dignity().term is GlossaryKey | null; format.test.ts asserts every real dignity has one',
    '/components/chart/HouseList.tsx':
      'HOUSE_TERMS is (GlossaryKey | null)[]; HouseList.test.tsx asserts all twelve are present, ' +
      'distinct, and named for the house they sit on',
    '/components/chart/VargaSwitcher.tsx':
      'Varga.term is GlossaryKey; varga.test.ts asserts every chart name resolves',
    '/components/chart/DashaTimeline.tsx':
      'LEVELS[].term is GlossaryKey; DashaTimeline.test.tsx asserts all three resolve',
    '/components/chart/StyleSwitcher.tsx':
      'STYLE_TERMS is Record<ChartStyle, GlossaryKey>; StyleSwitcher.test.tsx asserts both resolve',
    '/components/chart/SadeSatiIndicator.tsx':
      'PHASES[].term is GlossaryKey; SadeSatiIndicator.test.tsx asserts all three resolve',
  }

  it('has no computed term props outside the files that justify one', () => {
    const unjustified = scan().dynamic.filter((file) => !(file in COMPUTED_TERMS_ALLOWED))

    expect(
      unjustified,
      'A computed `term={…}` is outside what a source scan can check, so it reopens ' +
        'exactly the hole this file closes. If one is genuinely needed, type its source ' +
        'as GlossaryKey, add a test for the mapping, and add the file to ' +
        'COMPUTED_TERMS_ALLOWED with that reason.',
    ).toEqual([])
  })

  // An allowlist nobody prunes is a list of files that stopped existing.
  it('does not allow computed terms in files that no longer have any', () => {
    const dynamic = new Set(scan().dynamic)
    const stale = Object.keys(COMPUTED_TERMS_ALLOWED).filter((file) => !dynamic.has(file))

    expect(
      stale,
      'these files are exempted from the computed-term check and no longer contain one',
    ).toEqual([])
  })

  // A scan that found nothing passes both assertions above forever. This
  // proves the regex still matches the syntax the app is written in — the
  // first thing to break when the component is renamed or re-exported.
  it('recognises the call syntax it is looking for', () => {
    const sample = `
      <AstroTerm term="nakshatra" />
      <AstroTerm term='sade_sati'>Sade Sati</AstroTerm>
      <AstroTerm
        term="dasha"
        className="x"
      />
    `
    const found = [...sample.matchAll(LITERAL)].map((m) => m[1])
    expect(found).toEqual(['nakshatra', 'sade_sati', 'dasha'])

    expect(DYNAMIC.test('<AstroTerm term={key} />')).toBe(true)
  })

  // The regression for the `lastIndex` bug above. `.test()` on a global
  // regex resumes from where the previous call stopped, so the second of
  // two offending files reads as clean. Two calls, both true.
  it('detects a computed prop in every file, not just the first', () => {
    expect(DYNAMIC.test('<AstroTerm term={a} />')).toBe(true)
    expect(DYNAMIC.test('<AstroTerm term={b} />')).toBe(true)
    expect(DYNAMIC.flags).not.toContain('g')
  })

  it('reads real files, not an empty directory', () => {
    const files = sourceFiles(SRC)
    expect(files.length).toBeGreaterThan(20)
    expect(files.some((f) => f.endsWith('AstroTerm.tsx'))).toBe(true)
  })
})
