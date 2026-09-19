import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

/**
 * Nothing in this app may render a string as HTML.
 *
 * ── Why a structural guard rather than an escaping test ──
 *
 * `PHASE-03-KUNDLI-UI.md` §11 asks that "all rendered content escaped — a
 * user-supplied profile label cannot inject markup". Today that holds for
 * a reason nobody wrote: React escapes every JSX child, and there is not
 * one `dangerouslySetInnerHTML` in the codebase. The label reaches the
 * screen through `interpolate()`, which returns a plain string, and a
 * plain string in JSX is text.
 *
 * So the property is currently true *by absence*. A test that feeds a
 * hostile label through today's components would pass, prove that React
 * escapes — which was never in doubt — and say nothing about the day
 * somebody adds the one API that turns a string into markup.
 *
 * This asserts the absence instead. It is the difference between checking
 * the door is shut and checking there is no door.
 *
 * ── Why this matters more from Phase 5 ──
 *
 * The risk today is small precisely because the app renders nothing
 * untrusted: its own strings, and the user's own name. From Phase 5 the
 * chat renders MODEL output, and `.claude/rules/frontend.md` is explicit
 * that an LLM emitting `<img onerror=...>` is a real vector rather than a
 * hypothetical. The rule there — "sanitise on an allowlist" — needs a
 * deliberate, reviewed exception to this guard, which is exactly what a
 * failing test forces.
 *
 * If you are here because this test failed: that is the design. Sanitise
 * on an allowlist, put the call behind a named helper, and add it to
 * ALLOWED below with a comment saying what sanitises it.
 */

const SRC = join(process.cwd(), 'src')

/**
 * Call sites permitted to produce raw HTML, each with the sanitiser that
 * makes it safe. Empty today, and that is the point: the first entry
 * should be hard to add and obvious in a diff.
 */
const ALLOWED: ReadonlyArray<{ file: string; sanitisedBy: string }> = []

function sourceFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const path = join(dir, entry)
    if (statSync(path).isDirectory()) return sourceFiles(path)
    return /\.(ts|tsx)$/.test(path) ? [path] : []
  })
}

/**
 * Comments stripped, so the prose above — which names the very API it
 * rejects — is not itself a failure.
 *
 * Every guard in this repo has hit that trap. Block comments go
 * wholesale; line comments only when `//` opens the line, so a `https://`
 * inside a string cannot truncate a line and hide a real call after it.
 */
function stripComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '')
}

describe('no string is ever rendered as HTML', () => {
  /*
    WRITES only. Reading `innerHTML` is safe and useful — `error.test.tsx`
    asserts a leaked secret does not appear in the rendered HTML, which is
    a security test, and the first version of this guard flagged it.

    That is the failure mode to avoid above all others here: a guard that
    fires on correct code gets an exception added, then another, and then
    it is ignored. The dangerous operation is assignment, so the pattern
    matches assignment.
  */
  const RAW_HTML_APIS: ReadonlyArray<{ name: string; pattern: RegExp }> = [
    // Always a write — there is no reading form.
    { name: 'dangerouslySetInnerHTML', pattern: /dangerouslySetInnerHTML/ },
    { name: 'innerHTML assignment', pattern: /\.innerHTML\s*(=[^=]|\+=)/ },
    { name: 'outerHTML assignment', pattern: /\.outerHTML\s*(=[^=]|\+=)/ },
    { name: 'insertAdjacentHTML', pattern: /\.insertAdjacentHTML\s*\(/ },
  ]

  it('no component reaches for a raw-HTML API', () => {
    const offences: string[] = []

    for (const file of sourceFiles(SRC)) {
      // The guard's own file quotes every name it rejects.
      if (file === join(SRC, 'test', 'no-raw-html.test.ts')) continue

      const source = stripComments(readFileSync(file, 'utf8'))
      const relative = file.replace(SRC, 'src')
      if (ALLOWED.some((entry) => entry.file === relative)) continue

      for (const api of RAW_HTML_APIS) {
        if (api.pattern.test(source)) offences.push(`${relative}: ${api.name}`)
      }
    }

    expect(
      offences,
      'these render a string as HTML. A profile label, a place name or — ' +
        'from Phase 5 — a model response reaching one of these is a script ' +
        'injection, and React escaping everywhere else will not help. ' +
        'Sanitise on an allowlist, put it behind a named helper, and add ' +
        'the file to ALLOWED with the sanitiser named beside it.',
    ).toEqual([])
  })

  it('would catch the API it exists to reject', () => {
    /*
      The guard, broken on purpose. A `RAW_HTML_APIS` list that stopped
      matching — renamed by React, or fat-fingered here — would leave this
      suite green while the door stood open.
    */
    const hostile = 'export const X = () => <div dangerouslySetInnerHTML={{ __html: label }} />'
    expect(
      RAW_HTML_APIS.some((api) => api.pattern.test(stripComments(hostile))),
      'the rule does not match the exact call it was written to reject',
    ).toBe(true)

    // The DOM route, which a component holding a ref can still take.
    expect(
      RAW_HTML_APIS.some((api) => api.pattern.test('ref.current.innerHTML = label')),
      'assigning innerHTML directly is not caught',
    ).toBe(true)
  })

  it('does not fire on prose that merely names the API', () => {
    // Comment-stripping is load-bearing: the explanation of a rejected
    // pattern belongs beside the fix, in a product file.
    const commented = `/* never use dangerouslySetInnerHTML here */\nconst a = 1`
    expect(
      RAW_HTML_APIS.some((api) => api.pattern.test(stripComments(commented))),
      'a comment explaining the rule is being counted as a violation of it',
    ).toBe(false)

    /*
      And reading is not writing. error.test.tsx asserts a leaked secret
      does not appear in `container.innerHTML` — a security test, which
      the first version of this guard reported as a security defect.
    */
    expect(
      RAW_HTML_APIS.some((api) =>
        api.pattern.test('expect(container.innerHTML).not.toContain(secret)'),
      ),
      'reading innerHTML in an assertion is being flagged as a write',
    ).toBe(false)
  })
})
