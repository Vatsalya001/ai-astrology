import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { PREDICTIVE_PHRASES, defineTerm } from '@ayana/content'

import { PHASES, SadeSatiIndicator } from './SadeSatiIndicator'
import type { SadeSatiStatus } from '@/lib/astrology-api'
import { LocaleProvider } from '@/lib/i18n/context'

function status(overrides: Partial<SadeSatiStatus> = {}): SadeSatiStatus {
  return {
    is_active: true,
    phase: 'rising',
    saturn_sign: 'Capricorn',
    houses_from_moon: 12,
    ...overrides,
  }
}

function renderIndicator(s: SadeSatiStatus) {
  return render(
    <LocaleProvider>
      <SadeSatiIndicator status={s} />
    </LocaleProvider>,
  )
}

describe('SadeSatiIndicator', () => {
  // Targeted at the step marker rather than by text: "peak" also appears
  // in the glossary entry's name below the dots, so a bare text query
  // matches two elements and would keep matching if the marker vanished.
  it('names the current phase', () => {
    renderIndicator(status({ phase: 'peak' }))

    const current = screen.getByText('Peak').closest('[aria-current="step"]')
    expect(current).not.toBeNull()
    expect(screen.getByText(/— current phase/i)).toBeInTheDocument()
  })

  it('shows all three phases, not only the current one', () => {
    renderIndicator(status({ phase: 'rising' }))

    for (const phase of ['Rising', 'Peak', 'Setting']) {
      expect(screen.getByText(phase), phase).toBeInTheDocument()
    }
  })

  // The dots are gold; the phase is also named in text and marked with
  // aria-current. A reader who cannot separate the golds still knows.
  it('marks the phase with something other than colour', () => {
    renderIndicator(status({ phase: 'setting' }))

    const current = screen.getByText('Setting').closest('[aria-current]')
    expect(current).toHaveAttribute('aria-current', 'step')
  })

  it('says so plainly when it is not running', () => {
    renderIndicator(status({ is_active: false, phase: null }))

    expect(screen.getByText(/not currently running/i)).toBeInTheDocument()
    expect(screen.queryByText(/current phase/i)).toBeNull()
  })

  it('reports where Saturn is in both cases', () => {
    renderIndicator(status({ saturn_sign: 'Aquarius', houses_from_moon: 1 }))
    expect(screen.getByText(/Aquarius/)).toBeInTheDocument()
    expect(screen.getByText(/1st sign from your Moon/)).toBeInTheDocument()
  })

  /**
   * An active Sade Sati whose phase the engine did not name.
   *
   * Defaulting to "rising" would be inventing the answer to the most
   * consequential question on the screen — and it would look completely
   * normal, because "rising" is a plausible thing to display.
   */
  it('does not guess a phase the engine did not report', () => {
    renderIndicator(status({ is_active: true, phase: null }))

    expect(screen.getByText(/phase was not reported/i)).toBeInTheDocument()
    expect(screen.queryByText(/current phase/i)).toBeNull()
  })

  it('does not guess a phase it does not recognise', () => {
    renderIndicator(status({ is_active: true, phase: 'waxing' }))
    expect(screen.getByText(/phase was not reported/i)).toBeInTheDocument()
  })

  /**
   * The framing rule, on the screen where it matters most.
   *
   * Sade Sati is the most dreaded thing in Indian astrology. Telling
   * somebody they are in for seven bad years is the exact failure the AI
   * rules exist to prevent, and it does not become acceptable for being
   * static copy rather than model output. Checked against the same
   * phrase list the corpus is checked against.
   */
  it('never uses predictive framing, in any state', () => {
    const states: SadeSatiStatus[] = [
      status({ phase: 'rising' }),
      status({ phase: 'peak' }),
      status({ phase: 'setting' }),
      status({ is_active: false, phase: null }),
      status({ is_active: true, phase: null }),
    ]

    const offences: string[] = []
    for (const s of states) {
      const { container, unmount } = renderIndicator(s)
      const text = (container.textContent ?? '').toLowerCase()
      for (const phrase of PREDICTIVE_PHRASES) {
        if (text.includes(phrase)) offences.push(`${s.phase ?? 'none'}: "${phrase}"`)
      }
      unmount()
    }

    expect(offences).toEqual([])
  })

  // "Difficult", "bad", "suffering" are not in the predictive list and
  // are exactly the register this component must not use.
  it('does not characterise the period as bad', () => {
    const { container } = renderIndicator(status({ phase: 'peak' }))
    const text = (container.textContent ?? '').toLowerCase()

    for (const word of ['bad', 'suffer', 'misfortune', 'disaster', 'danger', 'beware']) {
      expect(text, word).not.toContain(word)
    }
  })

  /**
   * The engine reports no span, so none is shown.
   *
   * Deriving "ends April 2033" from the phase in TypeScript would be the
   * frontend computing astrology — the same rule that keeps the model
   * out of it. This asserts the absence so that adding a computed date
   * is a failing test rather than a plausible-looking improvement.
   */
  it('shows no start or end date, because the engine reports none', () => {
    const { container } = renderIndicator(status({ phase: 'rising' }))
    const text = container.textContent ?? ''

    expect(text).not.toMatch(/\b(19|20)\d{2}\b/)
    expect(text).not.toMatch(/\b(started|ends|until)\b/i)
  })
})

/**
 * The promise `astro-term-usage.test.ts` allows this file's computed
 * `term={…}` on: typed as `GlossaryKey`, and the mapping checked.
 */
describe('Sade Sati phase terms', () => {
  it('defines a term for all three phases', () => {
    const missing = PHASES.filter((p) => !defineTerm(p.term, 'en')).map((p) => p.key)
    expect(missing).toEqual([])
  })

  it('points each phase at the entry for that phase', () => {
    const mismatched: string[] = []
    for (const phase of PHASES) {
      const entry = defineTerm(phase.term, 'en')!
      if (!entry.name.toLowerCase().includes(phase.key)) {
        mismatched.push(`${phase.key} -> "${entry.name}"`)
      }
    }
    expect(mismatched).toEqual([])
  })

  // The keys are compared against what the API sends in `phase`. A
  // mismatch here silently renders "the phase was not reported" for a
  // reading where the engine reported it perfectly well.
  it('uses the phase keys api-service actually sends', () => {
    expect(PHASES.map((p) => p.key)).toEqual(['rising', 'peak', 'setting'])
  })
})
