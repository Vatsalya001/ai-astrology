/**
 * The performance budget, enforced.
 *
 * Task 3.19: "Bundle check fails the build if exceeded."
 *
 * ── Why it gzips the files itself ──
 *
 * `next build` reports first-load JS UNCOMPRESSED, and the spec's budget
 * is in gzipped KB. Those differ by roughly 3.3x here, so checking the
 * reported number against a gzip budget would pass everything forever.
 *
 * ── Why plain ESM and not TypeScript ──
 *
 * It runs as part of `task build`, in CI, on a machine that may have
 * installed production dependencies only. Reaching for a TS runtime here
 * would add a dependency whose absence turns the gate into a skip — and
 * a build gate that quietly does not run is the failure mode this whole
 * file exists to prevent. Node executes this as-is.
 *
 * ── Why it reads the manifest rather than the build's printed table ──
 *
 * The table is formatted for people and has been reformatted between
 * Next versions. `.next/diagnostics/route-bundle-stats.json` is the same
 * data as structured JSON, and a missing file fails loudly below rather
 * than yielding zero routes and a cheerful pass.
 */

import { gzipSync } from 'node:zlib'
import { readFileSync, existsSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

/*
  Resolved from this file, not from process.cwd().

  The script is run by `task`, by npm, and by CI from three different
  working directories. A cwd-relative path silently resolves to a
  non-existent manifest in two of them — which this script would then
  report as "no routes", i.e. a pass.
*/
const here = dirname(fileURLToPath(import.meta.url))
const webRoot = join(here, '..')

const STATS = join(webRoot, '.next', 'diagnostics', 'route-bundle-stats.json')
const BUDGETS = join(webRoot, 'bundle-budget.json')

const KB = 1024

function fail(message) {
  console.error(`\n✗ ${message}\n`)
  process.exit(1)
}

if (!existsSync(STATS)) {
  fail(
    `no build stats at ${STATS}.\n` +
      `  Run \`next build\` first. This is a hard failure rather than a skip:\n` +
      `  a budget check that silently passes when it cannot measure anything\n` +
      `  is worse than no budget check, because it reports success.`,
  )
}

const stats = JSON.parse(readFileSync(STATS, 'utf8'))
const budgets = JSON.parse(readFileSync(BUDGETS, 'utf8'))

if (stats.length === 0) {
  fail('the build stats name no routes; there is nothing to check')
}

/**
 * Gzipped first-load JS for one route.
 *
 * Chunks are de-duplicated by path: a chunk listed twice is downloaded
 * once, and counting it twice would inflate every route by the size of
 * whatever the manifest happened to repeat.
 *
 * Each chunk is compressed on its own rather than concatenated, which
 * matches how a browser receives them — one response per chunk, each
 * compressed separately. Concatenating first would let gzip find
 * cross-chunk redundancy no real client benefits from, and report a
 * number smaller than anything a user ever downloads.
 */
function gzippedBytes(route) {
  const seen = new Set()
  const missing = []
  let bytes = 0

  for (const rel of route.firstLoadChunkPaths) {
    if (seen.has(rel)) continue
    seen.add(rel)

    const path = join(webRoot, rel)
    if (!existsSync(path)) {
      missing.push(rel)
      continue
    }
    bytes += gzipSync(readFileSync(path), { level: 9 }).byteLength
  }

  return { bytes, missing }
}

const measured = stats.map((route) => {
  const { bytes, missing } = gzippedBytes(route)
  const kb = bytes / KB
  const budgetKB = budgets.routes[route.route] ?? budgets.defaultBudgetKB
  return { route: route.route, kb, budgetKB, overBy: kb - budgetKB, missing }
})

measured.sort((a, b) => b.kb - a.kb)

// ─── report ──────────────────────────────────────────────────────────

console.log('First-load JS, gzipped:\n')
console.log(`  ${'route'.padEnd(36)}${'size'.padStart(9)}${'budget'.padStart(9)}`)

for (const row of measured) {
  const flag = row.overBy > 0 ? ' ✗' : ''
  console.log(
    `  ${row.route.padEnd(36)}${`${row.kb.toFixed(1)} KB`.padStart(9)}` +
      `${`${row.budgetKB} KB`.padStart(9)}${flag}`,
  )
}

/*
  A chunk the manifest names but the build did not produce is a failure,
  not a rounding error. It means the route is measured short by however
  much that chunk weighs — so the check would report a pass built on a
  number it could not actually compute.
*/
const withMissing = measured.filter((row) => row.missing.length > 0)
if (withMissing.length > 0) {
  console.error('\nChunks named by the manifest but absent from the build:')
  for (const row of withMissing) {
    console.error(`  ${row.route}: ${row.missing.join(', ')}`)
  }
  fail('the measurement is incomplete, so a pass would mean nothing')
}

// A budget for a route that no longer exists is a line nobody is
// checking, sitting in the file looking like coverage.
const known = new Set(measured.map((row) => row.route))
const stale = Object.keys(budgets.routes).filter((route) => !known.has(route))
if (stale.length > 0) {
  console.error('\nBudgets naming routes that the build does not produce:')
  for (const route of stale) console.error(`  ${route}`)
  fail('remove them, or fix the route name — a budget for a route that does not exist checks nothing')
}

// ─── the spec's target, reported but not enforced ────────────────────

const overSpec = measured.filter((row) => row.kb > budgets.specTargetKB)
if (overSpec.length > 0) {
  console.log(
    `\nNote: ${overSpec.length} route(s) exceed the spec's ${budgets.specTargetKB} KB target.\n` +
      `  The framework floor alone — React, react-dom and the Next client runtime —\n` +
      `  is larger than the headroom that target leaves for product code. Tracked in\n` +
      `  PROJECT_STATUS.md as an unmet spec item; see bundle-budget.json for why the\n` +
      `  enforced numbers differ.`,
  )
}

// ─── the gate ────────────────────────────────────────────────────────

const over = measured.filter((row) => row.overBy > 0)
if (over.length > 0) {
  console.error('\nOver budget:')
  for (const row of over) {
    console.error(
      `  ${row.route}: ${row.kb.toFixed(1)} KB against ${row.budgetKB} KB ` +
        `(+${row.overBy.toFixed(1)} KB)`,
    )
  }
  fail(
    'the bundle grew past its budget.\n' +
      '  Find what was added — usually one import pulling in a library or a\n' +
      '  corpus — and either remove it, load it lazily, or raise the number in\n' +
      '  bundle-budget.json WITH a reason in the diff.',
  )
}

console.log(`\n✓ ${measured.length} routes, all within budget`)
