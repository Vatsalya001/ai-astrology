import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import { hasTerm } from '@ayana/content'

import { DEFAULT_VARGA, VARGAS, varga } from './varga'

/**
 * The wire values in this list are a contract with Go, and nothing in
 * either language checks the other.
 *
 * `?type=` goes straight into the query string; `api-service` compares
 * it against its own constants and falls back to the rasi when it does
 * not recognise the value. So a typo here — `D-9`, `d9`, `D09` — does
 * not 400 and does not log. It renders the birth chart under a heading
 * that says Navamsa, which is precisely the bug Phase 2's gate found
 * already once, when the chart type never reached astro-service and a
 * client asking for D9 got a relabelled D1.
 *
 * Reading the Go source is crude and it is a monorepo, so it works. If
 * the constants move, this fails loudly rather than drifting quietly.
 */
const SERVICE_GO = join(
  process.cwd(),
  '..',
  '..',
  'services',
  'api',
  'internal',
  'charts',
  'service.go',
)

function goChartTypes(): string[] {
  const source = readFileSync(SERVICE_GO, 'utf8')
  return [...source.matchAll(/ChartType\w+\s*=\s*"([^"]+)"/g)].map((m) => m[1]!)
}

describe('varga list', () => {
  it('matches the chart types api-service defines', () => {
    expect(new Set(VARGAS.map((v) => v.type))).toEqual(new Set(goChartTypes()))
  })

  // Proves the reader above found something. An unreadable path or a
  // changed constant name would otherwise make the comparison a test of
  // two empty sets against each other.
  it('actually read the Go constants', () => {
    const types = goChartTypes()
    expect(types.length).toBe(3)
    expect(types).toContain('D1')
  })

  it('has a glossary definition for every chart name', () => {
    const undefinedTerms = VARGAS.filter((v) => !hasTerm(v.term)).map((v) => v.term)
    expect(
      undefinedTerms,
      'the switcher makes each name tappable; a term with no entry renders as plain text',
    ).toEqual([])
  })

  it('explains what each chart is read for', () => {
    // Three unexplained codes is a puzzle, not a control.
    const unexplained = VARGAS.filter((v) => v.purpose.trim().length < 20).map((v) => v.type)
    expect(unexplained).toEqual([])
  })

  // The corpus rule applies to this copy too — Phase 5 grounds a model
  // in whatever the product says, including microcopy.
  it('frames each purpose traditionally, not predictively', () => {
    const offences = VARGAS.filter((v) => /you will|guarantees|is certain/i.test(v.purpose)).map(
      (v) => v.type,
    )
    expect(offences).toEqual([])
  })

  it('defaults to the rasi', () => {
    expect(DEFAULT_VARGA).toBe('D1')
    expect(varga(DEFAULT_VARGA)).toBeDefined()
  })

  it('returns undefined for a chart it does not have', () => {
    expect(varga('D60')).toBeUndefined()
  })
})
