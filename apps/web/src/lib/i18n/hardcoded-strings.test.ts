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
 * ── The ratchet, and what it caught ──
 *
 * This shipped one PR ago as a per-file budget seeded at the debt of the
 * day: 58 literals across 19 files, asserted with `<=` so the numbers
 * could only fall. They have now fallen to zero for every Phase 3 file,
 * and `BUDGET` is empty.
 *
 * It stays empty. The rule is now the simple one the gate item states:
 * no file carries a hardcoded UI string except the seven below, each
 * for a reason that is structural rather than a preference.
 */

const SRC = join(process.cwd(), 'src')

/**
 * English by design, not by neglect.
 *
 * The first three are documented in `dictionaries.ts` and the reasoning
 * is sound: translating a liability disclaimer produces a document
 * nobody has reviewed in a language nobody has checked, and the status
 * page is an internal operations screen carrying `noindex` whose
 * audience reads English by construction.
 *
 * The last three cannot reach the dictionary at all, for reasons that
 * are structural rather than preferences:
 *
 *   layout.tsx      the skip link must be in the SERVER html so it works
 *                   before hydration — a keyboard user on a slow
 *                   connection is exactly who it is for. It is rendered
 *                   outside LocaleProvider on purpose.
 *
 *   error.tsx       the app-wide error boundary. Calling `useLocale()`
 *                   here means that if the thing which threw was the
 *                   provider, or anything it needs, the error page
 *                   throws too. An error boundary that can throw is not
 *                   an error boundary.
 *
 *   not-found.tsx   a Server Component, because it exports `metadata` —
 *                   the App Router does not allow that from a client
 *                   component, and `useLocale()` is a client hook.
 *                   Translating it means splitting one file into a
 *                   server wrapper and a client body to move four
 *                   strings on a 404 page. Recorded as a deliberate
 *                   trade rather than as a rule, because it is one.
 */
const ENGLISH_BY_DESIGN = new Set([
  'app/terms/page.tsx',
  'app/privacy/page.tsx',
  'app/status/page.tsx',
  'app/status/loading.tsx',
  'app/layout.tsx',
  'app/error.tsx',
  'app/not-found.tsx',
])

/**
 * Empty, and meant to stay that way.
 *
 * This held nineteen entries for one PR. Every Phase 3 file is now at
 * zero, so the first assertion below — "none in any file without a
 * budget" — has become the whole rule.
 *
 * A new entry here is not forbidden. It is a statement that somebody
 * shipped untranslated copy and wrote down how much, which is far
 * better than shipping it silently. But it should be rare and it should
 * come back down.
 */
const BUDGET: Record<string, number> = {}

/**
 * A JSX text node that reads like a sentence.
 *
 * `>Some words<` with a capital, at least eight characters and a space
 * in it. Deliberately narrow: single words like `>Chart<` are usually
 * labels whose translation is a separate argument, and catching them
 * would triple the count without tripling the signal.
 *
 * ── Why `()` and `=` are excluded ──
 *
 * Without them the pattern spans real code: the `>` of an arrow function
 * or a generic and the `<` of the next one, capturing things like
 * `Date.now())\n  const [levels, setLevels] = useState`. Two of the
 * nineteen files in the original budget were on the list for exactly
 * that reason and had no hardcoded copy at all, which means the first
 * published count was two files too pessimistic. Prose does not contain
 * parentheses or equals signs often enough to matter; code does.
 *
 * Semicolons are NOT excluded, because HTML entities need them —
 * `&rsquo;` is a semicolon inside a perfectly ordinary sentence.
 */
const JSX_TEXT = />\s*([A-Z][A-Za-z][^<>{}()=]{6,})\s*</g

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
   * Proves the matcher still matches — against REAL project files.
   *
   * A scanner that stopped recognising JSX reports a clean codebase and
   * satisfies every assertion above, which is indistinguishable from
   * success. This used to assert the total was greater than zero, and
   * that check died the moment the retrofit finished: the codebase being
   * clean is the goal, not a symptom.
   *
   * So the liveness check runs against `app/terms/page.tsx` — exempt
   * from the count, English by design, and full of the exact prose this
   * pattern exists to find. It survives the rest of the app being clean,
   * which is the whole point.
   */
  it('still recognises the syntax it is looking for', () => {
    const sample = '<p>This is a sentence.</p><span>Another one here</span>'
    expect([...sample.matchAll(JSX_TEXT)]).toHaveLength(2)

    const terms = readFileSync(join(SRC, 'app', 'terms', 'page.tsx'), 'utf8')
    const inTerms = [...terms.matchAll(JSX_TEXT)].filter((m) => /\s/.test(m[1]!))
    expect(
      inTerms.length,
      'the scanner found nothing in terms/page.tsx, which is full of English prose — ' +
        'it has stopped working',
    ).toBeGreaterThan(3)

  })
})
