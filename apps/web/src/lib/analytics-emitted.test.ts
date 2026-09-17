import { readdirSync, readFileSync, statSync } from 'node:fs'
import { extname, join } from 'node:path'

import { describe, expect, it } from 'vitest'



/**
 * Every declared event is emitted from somewhere.
 *
 * `analytics-events.test.ts` checks the vocabulary against the specs —
 * that the names we declare are the names the phase documents ask for.
 * It passed for the whole of Phase 3 while all four Phase 3 events were
 * emitted from NOWHERE:
 *
 *   kundli_viewed          0 call sites
 *   chart_style_switched   0
 *   glossary_term_opened   0
 *   ai_chat_box_tapped     0
 *
 * "Analytics events emitted" is a Definition-of-Done line, and a
 * declared event with no call site satisfies every existing check while
 * producing no data at all. The failure is silent by construction: an
 * event that never fires looks exactly like a feature nobody uses, which
 * is the single most expensive way to be wrong about a product.
 *
 * ── Why a source scan ──
 *
 * The alternative is a runtime spy, which only sees events on code paths
 * a test happens to exercise — so an event wired to a screen with no
 * test would still read as missing. This asks the narrower question the
 * DoD actually asks: does a call site exist at all.
 */

const SRC = join(process.cwd(), 'src')

/**
 * The declared vocabulary, read from the source of truth.
 *
 * `EventMap` is a TypeScript interface, so it is erased at runtime and
 * there is nothing to import. `analytics-events.test.ts` solves that by
 * restating the names by hand, deliberately — it is checking the
 * vocabulary against the SPECS, so a hand list is the point.
 *
 * This test asks a different question: is every declared event wired?
 * For that, a hand list would be the wrong input — a name someone forgot
 * to copy here is exactly the name most likely to be unwired.
 */
const EVENT_MAP_TS = join(
  process.cwd(),
  '..',
  '..',
  'packages',
  'analytics',
  'src',
  'index.ts',
)

function declaredEvents(): string[] {
  const source = readFileSync(EVENT_MAP_TS, 'utf8')
  const start = source.indexOf('export interface EventMap {')
  if (start === -1) return []

  // To the closing brace at column 0 — the interface body.
  const end = source.indexOf('\n}', start)
  const body = source.slice(start, end)

  // `  event_name: { ... }` at two spaces of indent. Comments start with
  // `//` and are skipped by the leading-newline anchor.
  return [...body.matchAll(/\n {2}([a-z][a-z0-9_]*):\s*\{/g)].map((m) => m[1]!)
}

/**
 * Events that are declared and deliberately not emitted yet, each with
 * the phase that will emit them.
 *
 * Empty today. It exists because a later phase will legitimately declare
 * an event ahead of the feature — and when it does, the exemption should
 * be written down with a reason rather than the guard being deleted.
 */
const NOT_YET_EMITTED: Record<string, string> = {
  /*
    There is no map picker. `PlaceSearch` is a text box over the
    gazetteer, and the spec puts a map on the mobile app in Phase 10 —
    so this is a vocabulary entry waiting for a feature rather than a
    feature waiting for a call site.
  */
  place_selected_via_map: 'Phase 10 — the mobile app introduces a map picker',
}

function sourceFiles(dir: string): string[] {
  const out: string[] = []
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) {
      out.push(...sourceFiles(path))
      continue
    }
    if (!['.ts', '.tsx'].includes(extname(name))) continue
    // A test asserting an event name is not a call site.
    if (name.includes('.test.')) continue
    out.push(path)
  }
  return out
}

/**
 * Where events are actually sent from — BOTH services.
 *
 * The first version of this scanned only the web app and reported
 * thirteen events unemitted. Nine of those are sent by api-service:
 * `otp_requested` fires during a request the browser never sees the
 * inside of, and `account_deleted` fires from a worker hours later.
 *
 * `packages/analytics` is the SHARED vocabulary, not the web app's. A
 * guard that assumed one emitter would have forced nine correct events
 * onto an exemption list and quietly inverted its own meaning.
 */
const API_ANALYTICS_GO = join(
  process.cwd(),
  '..',
  '..',
  'services',
  'api',
  'internal',
  'platform',
  'analytics',
  'analytics.go',
)
const API_INTERNAL = join(process.cwd(), '..', '..', 'services', 'api', 'internal')

/** `track('some_event'` in the web app. */
function webEmitted(): Set<string> {
  const found = new Set<string>()
  /*
    `trackAsUser` too. Matching only `track(` missed it, and
    `birth_time_unknown_selected` — which IS emitted, through the async
    variant — read as unwired. A scanner that reports a correct call
    site as missing pushes real events onto an exemption list, which
    inverts the guard's meaning.
  */
  const CALL = /\btrack(?:AsUser)?\(\s*'([a-z0-9_]+)'/g

  for (const file of sourceFiles(SRC)) {
    for (const match of readFileSync(file, 'utf8').matchAll(CALL)) {
      found.add(match[1]!)
    }
  }
  return found
}

/**
 * Events api-service sends.
 *
 * Go names them through constants — `analytics.SignupCompleted` — so
 * this resolves the constant to its string and then requires the
 * constant to be REFERENCED somewhere outside its own declaration file.
 * A declared-and-unused Go constant is the same defect in the other
 * language, and counting it as emitted would hide it.
 */
function apiEmitted(): Set<string> {
  const declarations = readFileSync(API_ANALYTICS_GO, 'utf8')
  const byConstant = new Map<string, string>()
  for (const m of declarations.matchAll(/^\s*(\w+)\s+=\s*"([a-z0-9_.]+)"/gm)) {
    byConstant.set(m[1]!, m[2]!)
  }

  const used = new Set<string>()
  for (const file of goFiles(API_INTERNAL)) {
    if (file === API_ANALYTICS_GO) continue
    const source = readFileSync(file, 'utf8')
    for (const [constant, event] of byConstant) {
      if (source.includes(`analytics.${constant}`)) used.add(event)
    }
  }
  return used
}

function goFiles(dir: string): string[] {
  const out: string[] = []
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) {
      out.push(...goFiles(path))
      continue
    }
    if (extname(name) !== '.go' || name.endsWith('_test.go')) continue
    out.push(path)
  }
  return out
}

function emittedEvents(): Set<string> {
  return new Set([...webEmitted(), ...apiEmitted()])
}

describe('analytics events', () => {
  it('has a call site for every declared event', () => {
    const emitted = emittedEvents()

    const unemitted = declaredEvents().filter(
      (name) => !emitted.has(name) && !(name in NOT_YET_EMITTED),
    )

    expect(
      unemitted,
      'these events are declared and never sent. An event that never fires looks ' +
        'identical to a feature nobody uses. Wire it, or add it to NOT_YET_EMITTED ' +
        'with the phase that will.',
    ).toEqual([])
  })

  it('emits nothing that is not declared', () => {
    const declared = new Set<string>(declaredEvents())
    const undeclared = [...emittedEvents()].filter((name) => !declared.has(name))

    expect(
      undeclared,
      'these are sent but not in the event map, so they are untyped and invisible to ' +
        'the vocabulary test',
    ).toEqual([])
  })

  it('lists no exemption that is now emitted', () => {
    const emitted = emittedEvents()
    const stale = Object.keys(NOT_YET_EMITTED).filter((name) => emitted.has(name))
    expect(stale, 'these are wired now — remove them from NOT_YET_EMITTED').toEqual([])
  })

  /**
   * Proves the scanner reads real files and recognises real call sites.
   *
   * A regex that stopped matching would report every event unemitted —
   * loud, and therefore safe. A file walk that found nothing would report
   * the same. The dangerous direction is the OTHER one: if `EVENT_NAMES`
   * were ever empty, the first assertion iterates nothing and passes.
   */
  it('is reading real files and a real vocabulary', () => {
    expect(sourceFiles(SRC).length).toBeGreaterThan(20)
    expect(declaredEvents().length, 'the event vocabulary is empty').toBeGreaterThan(10)
    expect(declaredEvents(), 'the EventMap parser lost a known event').toContain(
      'chart_style_switched',
    )

    // Both emitters, separately — so one of them going dark cannot be
    // masked by the other still working.
    expect(webEmitted().size, 'no track() call found in the web app').toBeGreaterThan(5)
    expect(apiEmitted().size, 'no analytics constant found in api-service').toBeGreaterThan(5)

    expect(webEmitted(), 'the web scanner lost a known call site').toContain('kundli_viewed')
    expect(apiEmitted(), 'the Go scanner lost a known emitter').toContain('session_revoked')
  })
})
