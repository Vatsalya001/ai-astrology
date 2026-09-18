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
    started_at: '2023-01-17T00:00:00Z',
    ends_at: '2030-06-03T00:00:00Z',
    ...overrides,
  }
}

/**
 * A fixed "now", between the fixture's start and end.
 *
 * The server's instant rather than the machine's: this component
 * measures how long is left from what the API sent, so a test that let
 * the real clock in would report a different number every year and fail
 * on its own in 2030.
 */
const SERVER_NOW = '2026-09-18T00:00:00Z'

function renderIndicator(s: SadeSatiStatus, at: string = SERVER_NOW) {
  return render(
    <LocaleProvider>
      <SadeSatiIndicator status={s} at={at} />
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

  /*
    The window, which is the question people came to ask.

    This test replaces one that asserted the OPPOSITE — that no dates
    appear, "because the engine reports none". That was true and correct
    when written: the engine had no window function exposed, so any date
    on screen would have been derived in TypeScript, which is the
    frontend computing astrology.

    The engine supplies them now. The rule has not changed; what it
    permits has, and the test below is the same rule from the other side
    — the dates shown must be the ones the API SENT.
  */
  it('shows the window the engine supplied', () => {
    renderIndicator(
      status({
        started_at: '2023-01-17T00:00:00Z',
        ends_at: '2030-06-03T00:00:00Z',
      }),
    )

    expect(screen.getByText(/January 2023.*June 2030/)).toBeInTheDocument()
  })

  /**
   * The dates are rendered, not derived.
   *
   * Given a window the phase alone could never imply, the screen must
   * show THAT window. A component quietly computing "2.5 years per sign
   * from the current phase" would produce something plausible and wrong,
   * and would pass any test that only checked a date was present.
   */
  it('renders the API\'s dates rather than deriving its own', () => {
    renderIndicator(
      status({
        phase: 'rising',
        // Deliberately not 7.5 years, and not adjacent to today.
        started_at: '1994-03-02T00:00:00Z',
        ends_at: '1996-11-19T00:00:00Z',
      }),
    )

    const text = screen.getByText(/1994/).textContent ?? ''
    expect(text).toContain('March 1994')
    expect(text).toContain('November 1996')
  })

  /**
   * No window is "we do not know", never silence.
   *
   * The server sends nulls when the stretch is not running and also when
   * it IS running but no window has been computed yet. Rendering nothing
   * in that second case reads as "this has no end", which is both untrue
   * and the more frightening of the two readings.
   */
  it('says the dates are unknown rather than showing nothing', () => {
    renderIndicator(status({ started_at: null, ends_at: null }))

    expect(screen.getByText(/still being worked out/i)).toBeInTheDocument()
  })

  /**
   * How long is left is measured from the SERVER's instant.
   *
   * A browser clock can be wrong by years — a phone with the date reset
   * is common — and "about 4 years left" derived from one is a
   * confident wrong answer to the exact number a reader came for.
   * `DashaTimeline` takes the server's instant for the same reason.
   */
  it('counts the remaining time from the instant the API sent', () => {
    renderIndicator(
      status({ started_at: '2023-01-17T00:00:00Z', ends_at: '2030-06-03T00:00:00Z' }),
      // Four years before the end, whatever this machine's clock says.
      '2026-06-03T00:00:00Z',
    )

    expect(screen.getByText(/About 4 years left/i)).toBeInTheDocument()
  })

  it('says less than a year when the end is close', () => {
    renderIndicator(
      status({ started_at: '2023-01-17T00:00:00Z', ends_at: '2030-06-03T00:00:00Z' }),
      '2030-01-03T00:00:00Z',
    )

    expect(screen.getByText(/less than a year/i)).toBeInTheDocument()
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
