import { readdirSync, readFileSync, statSync } from 'node:fs'
import { extname, join } from 'node:path'

import { describe, expect, it } from 'vitest'

/**
 * The Phase 1 gate item is "i18n scaffolding in place; no hardcoded UI
 * strings", and until this file existed nothing checked the second half.
 *
 * `dictionaries.test.ts` asserts the dictionary is internally consistent
 * — every key in both locales, nothing empty, Hindi actually different.
 * All true, and all of it passes while a screen renders English
 * literals and never opens the dictionary at all. That is what happened:
 * Phase 3 added six screens and ten components, and the copy went
 * straight into the JSX.
 *
 * ── A ratchet, not a pass ──
 *
 * The honest options were to retrofit ~58 strings across three merged
 * PRs inside an unrelated change, or to measure the debt and stop it
 * growing. This is the second. Every count below is what that file has
 * TODAY; the assertion is `≤`, so the numbers can only fall, and a file
 * not on the list may have none at all.
 *
 * The retrofit has a precise definition of done because of this file:
 * every number reaches zero and the list becomes the three documented
 * exceptions.
 *
 * ── Why it counts rather than forbidding ──
 *
 * The matcher is crude — JSX text nodes that look like a sentence — and
 * a crude matcher used as a hard gate gets argued with and then deleted.
 * As a budget it is useful even when slightly wrong: nobody disputes a
 * number that is only allowed to go down.
 */

const SRC = join(process.cwd(), 'src')

/**
 * Three files are English by design, not by neglect.
 *
 * `dictionaries.ts` states the reasoning for all three and it is sound:
 * translating a liability disclaimer produces a document nobody has
 * reviewed in a language nobody has checked, and the status page is an
 * internal operations screen carrying `noindex` whose audience reads
 * English by construction. They are exempt, not budgeted.
 */
const ENGLISH_BY_DESIGN = new Set([
  'app/terms/page.tsx',
  'app/privacy/page.tsx',
  'app/status/page.tsx',
  'app/status/loading.tsx',
])

/**
 * What each file has today. The assertion is `≤`.
 *
 * A file absent from this map must have zero. That is the part that
 * stops the debt spreading to new screens while the existing ones are
 * being paid down.
 */
const BUDGET: Record<string, number> = {
  // Phase 3 screens — the debt this file exists to measure.
  'app/kundli/dashas/page.tsx': 7,
  'app/kundli/planets/page.tsx': 7,
  'app/kundli/yogas/page.tsx': 6,
  'app/kundli/transits/page.tsx': 5,
  'app/home/page.tsx': 2,
  'components/home/CurrentPeriodCard.tsx': 4,
  'components/home/AskBox.tsx': 2,
  'components/home/TodayCard.tsx': 2,
  'components/chart/SadeSatiIndicator.tsx': 3,
  'components/chart/HouseList.tsx': 2,
  'components/chart/PlanetTable.tsx': 2,
  'components/chart/TransitPanel.tsx': 2,
  'components/chart/ChartSVG.tsx': 1,
  'components/chart/DashaTimeline.tsx': 1,
  'components/chart/YogaCard.tsx': 1,

  // Predates Phase 3.
  'app/error.tsx': 5,
  'app/not-found.tsx': 4,
  'app/auth/verify/page.tsx': 1,
  'app/layout.tsx': 1,
}

/**
 * A JSX text node that reads like a sentence.
 *
 * `>Some words<` with a capital, at least eight characters and a space
 * in it. Deliberately narrow: single words like `>Chart<` are usually
 * labels whose translation is a separate argument, and catching them
 * would triple the count without tripling the signal.
 */
const JSX_TEXT = />\s*([A-Z][A-Za-z][^<>{}]{6,})\s*</g

function sourceFiles(dir: string): string[] {
  const out: string[] = []
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) {
      out.push(...sourceFiles(path))
      continue
    }
    if (extname(name) !== '.tsx' || name.includes('.test.')) continue
    out.push(path)
  }
  return out
}

function countByFile(): Map<string, string[]> {
  const found = new Map<string, string[]>()

  for (const file of sourceFiles(SRC)) {
    const relative = file.slice(SRC.length + 1)
    if (ENGLISH_BY_DESIGN.has(relative)) continue

    const hits = [...readFileSync(file, 'utf8').matchAll(JSX_TEXT)]
      .map((m) => m[1]!.trim())
      .filter((s) => /\s/.test(s))

    if (hits.length > 0) found.set(relative, hits)
  }
  return found
}

describe('hardcoded UI strings', () => {
  it('has none in any file without a budget', () => {
    const unbudgeted = [...countByFile().entries()]
      .filter(([file]) => !(file in BUDGET))
      .map(([file, hits]) => `${file}: ${hits.length} — e.g. "${hits[0]}"`)

    expect(
      unbudgeted,
      'new UI copy belongs in `dictionaries.ts`. A hardcoded string is invisible to ' +
        'review — nobody notices "Enter your phone number" on a screen that takes an ' +
        'email until a user does.',
    ).toEqual([])
  })

  it('never exceeds any file’s budget', () => {
    const found = countByFile()
    const over: string[] = []

    for (const [file, budget] of Object.entries(BUDGET)) {
      const count = found.get(file)?.length ?? 0
      if (count > budget) over.push(`${file}: ${count} > ${budget}`)
    }

    expect(
      over,
      'these numbers may only go down. Adding copy to a screen that already carries ' +
        'this debt makes the retrofit larger; move the new string to the dictionary.',
    ).toEqual([])
  })

  /**
   * The budget is a debt, and a debt nobody is looking at is a budget
   * that quietly becomes permanent.
   *
   * This fails once a file is paid off, which is the only kind of
   * failure that is good news — and the fix is one deletion.
   */
  it('lists no file that has already been paid off', () => {
    const found = countByFile()
    const stale = Object.keys(BUDGET).filter((file) => !found.has(file))

    expect(
      stale,
      'these files now have no hardcoded strings — remove them from BUDGET so the ' +
        'ratchet cannot slip back',
    ).toEqual([])
  })

  /**
   * Proves the matcher still matches, and prints the total.
   *
   * A scanner that stopped recognising JSX would report a clean codebase
   * and satisfy every assertion above — the same failure mode as an
   * empty file list or a suite that never ran.
   */
  it('still recognises the syntax it is looking for', () => {
    const sample = '<p>This is a sentence.</p><span>Another one here</span>'
    expect([...sample.matchAll(JSX_TEXT)]).toHaveLength(2)

    const total = [...countByFile().values()].reduce((sum, hits) => sum + hits.length, 0)
    expect(total, 'the scanner found nothing at all — it has stopped working').toBeGreaterThan(
      0,
    )
  })
})
