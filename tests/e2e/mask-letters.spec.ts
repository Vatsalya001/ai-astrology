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
