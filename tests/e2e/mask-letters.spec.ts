import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'

import { expect, test } from '@playwright/test'

/**
 * No two tests may share an identifier's first letter.
 *
 * `uniqueEmail` produces `g1789573370965-221422@example.com`, and the API
 * log masks it to `g***@example.com`. The mask keeps only the first
 * letter, so that letter IS the identity as far as the OTP watcher is
 * concerned. Two tests sharing one, running in parallel, read each
 * other's codes — and the failure reads as "wrong code", which looks
 * exactly like broken authentication rather than a test collision.
 *
 * This exists because I hit it twice in one sitting. The second time,
 * the letters I picked as free were produced by grepping for
 * `uniqueEmail('x')` literals, which missed all nine that
 * settings.spec.ts passes as a parameter to its own signUp helper.
 * A grep is only as good as the call shape someone happened to use;
 * this reads every form.
 */

// Relative to the repository root, the way otp-log.ts resolves
// `.run/api.log`. `import.meta.url` is the obvious spelling and does not
// load here — the nearest package.json is not `type: module`, and
// Playwright reports that as "No tests found" rather than as an import
// error. A whole spec file silently absent from the run is the worst
// failure mode a guard can have.
const E2E_DIR = join(process.cwd(), 'tests', 'e2e')

// Both shapes: a literal passed to uniqueEmail, and a letter handed to a
// per-file signUp helper. Adding a third shape means adding it here —
// and the count assertion below is what makes that noticeable.
const LETTER_PATTERNS = [
  /uniqueEmail\(\s*'([a-z])'/g,
  /signUp\(\s*[A-Za-z]+\s*,\s*'([a-z])'/g,
]

/**
 * The phone channel has the same problem with a different discriminator.
 *
 * `maskIdentifier` keeps three leading characters and three trailing
 * digits, and every Indian mobile masks to `+91********`, so the TAIL is
 * all that separates two concurrent phone signups. This went unchecked
 * until the email alphabet ran out and a spec had to switch channels —
 * at which point an unguarded thousand-value space is only better than a
 * guarded twenty-six-value one until two specs pick the same number.
 */
/*
  Two shapes here too, and the second was missing until it cost a run.

  The email check above learned this lesson and wrote it down — a grep
  for `uniqueEmail('x')` missed nine call sites that pass the letter to a
  per-file helper — but the fix was only applied to LETTER_PATTERNS. The
  phone check kept a single pattern, so two specs holding their tail in a
  `const PHONE_TAIL` were invisible to it.

  They collided on '744'. The guard passed. The failure surfaced in
  kundli-keyboard.spec.ts as a sign-up that never left /auth/verify,
  because the other spec's watcher had eaten its code — exactly the
  "looks like broken authentication" symptom this file exists to prevent,
  reported against an innocent spec.
*/
const TAIL_PATTERNS = [
  /uniquePhone\(\s*'(\d{3})'/g,
  /PHONE_TAIL\s*=\s*'(\d{3})'/g,
]

test('no two specs share an OTP mask letter', () => {
  const owners = new Map<string, Set<string>>()

  const specs = readdirSync(E2E_DIR).filter(
    // Not itself: the prose above quotes `uniqueEmail('x')` as an
    // example, and a guard that flags its own documentation is a guard
    // people learn to ignore.
    (name) => name.endsWith('.spec.ts') && name !== 'mask-letters.spec.ts',
  )

  for (const file of specs) {
    const source = readFileSync(join(E2E_DIR, file), 'utf8')

    for (const pattern of LETTER_PATTERNS) {
      // Fresh lastIndex per file: a /g regex reused across strings
      // resumes mid-way and silently skips matches.
      pattern.lastIndex = 0
      for (const match of source.matchAll(pattern)) {
        const letter = match[1]!
        if (!owners.has(letter)) owners.set(letter, new Set())
        owners.get(letter)!.add(file)
      }
    }
  }

  // The guard is worthless if the patterns stopped matching anything.
  expect(
    owners.size,
    'no mask letters were found at all — the patterns above no longer match ' +
      'how the specs request an identifier, so this test is checking nothing',
  ).toBeGreaterThan(10)

  const shared = [...owners.entries()]
    .filter(([, files]) => files.size > 1)
    .map(([letter, files]) => `'${letter}' in ${[...files].sort().join(' and ')}`)

  expect(
    shared,
    'these specs share an OTP mask letter. Running in parallel they read each ' +
      "other's codes, and the failure presents as \"wrong code\" — which looks " +
      'like broken authentication rather than a collision. Pick a letter no ' +
      'other spec uses.',
  ).toEqual([])
})

test('no two specs share an OTP phone mask tail', () => {
  const owners = new Map<string, Set<string>>()

  const specs = readdirSync(E2E_DIR).filter(
    (name) => name.endsWith('.spec.ts') && name !== 'mask-letters.spec.ts',
  )

  for (const file of specs) {
    const source = readFileSync(join(E2E_DIR, file), 'utf8')
    for (const pattern of TAIL_PATTERNS) {
      // Fresh lastIndex per file, as above: a /g regex reused across
      // strings resumes mid-way and silently skips matches.
      pattern.lastIndex = 0
      for (const match of source.matchAll(pattern)) {
        const tail = match[1]!
        if (!owners.has(tail)) owners.set(tail, new Set())
        owners.get(tail)!.add(file)
      }
    }
  }

  // Lower than the letter threshold on purpose: only a couple of specs
  // sign up by phone today. It is not zero, which is what would mean the
  // pattern has stopped matching.
  expect(
    owners.size,
    'no phone tails were found — the pattern no longer matches how specs ' +
      'request a number, so this test is checking nothing',
  ).toBeGreaterThan(0)

  const shared = [...owners.entries()]
    .filter(([, files]) => files.size > 1)
    .map(([tail, files]) => `'${tail}' in ${[...files].sort().join(' and ')}`)

  expect(
    shared,
    'these specs share a phone mask tail. Every Indian mobile masks to ' +
      '+91********, so the tail is the whole discriminator — sharing one has ' +
      'exactly the same effect as sharing an email letter.',
  ).toEqual([])
})
