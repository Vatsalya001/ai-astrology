import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { TransitPanel } from './TransitPanel'
import { RETROGRADE_MARK } from './glyphs'
import type { NatalTransits, TransitPosition } from '@/lib/astrology-api'
import { LocaleProvider } from '@/lib/i18n/context'

function position(overrides: Partial<TransitPosition> = {}): TransitPosition {
  return {
    planet: 'Saturn',
    sign: 'Pisces',
    sign_index: 11,
    degree: 12.5,
    longitude: 342.5,
    is_retrograde: false,
    timestamp: '2026-09-16T12:00:00Z',
    house_from_moon: 12,
    ...overrides,
  }
}

function data(overrides: Partial<NatalTransits> = {}): NatalTransits {
  return {
    at: '2026-09-16T12:00:00Z',
    natal_moon_sign: 'Aries',
    transits: [position()],
    sade_sati: {
      is_active: true,
      phase: 'rising',
      saturn_sign: 'Pisces',
      houses_from_moon: 12,
    },
    ...overrides,
  }
}

function renderPanel(d: NatalTransits) {
  return render(
    <LocaleProvider>
      <TransitPanel data={d} />
    </LocaleProvider>,
  )
}

/**
 * Scoped to the transit list by name.
 *
 * The Sade Sati indicator below is also a list, so a bare
 * `getByRole('listitem')` matches its three phases as well — which is
 * how this was written first, and it failed with "found multiple
 * elements" rather than silently passing. Naming the list fixed both the
 * test and the landmark a screen reader lands on.
 */
function transitItems() {
  return within(screen.getByRole('list', { name: /transiting planets/i })).queryAllByRole(
    'listitem',
  )
}

describe('TransitPanel', () => {
  it('lists each transiting planet with its sign and degree', () => {
    renderPanel(data({ transits: [position({ planet: 'Jupiter', sign: 'Gemini', degree: 8.7 })] }))

    const item = transitItems()[0]!
    expect(item).toHaveTextContent('Jupiter')
    expect(item).toHaveTextContent('Gemini')
    expect(item).toHaveTextContent("8°42'")
  })

  /**
   * Which frame the houses are counted from, said out loud.
   *
   * Gochara counts from the Moon's sign, not the ascendant. "Saturn in
   * your 12th" means two different things depending on where you count
   * from, and a reader comparing this screen with a printed chart has no
   * way to tell which unless it says so.
   */
  it('states that houses are counted from the natal Moon, and which sign that is', () => {
    renderPanel(data({ natal_moon_sign: 'Taurus' }))

    expect(screen.getByText(/counted from your Moon in Taurus/i)).toBeInTheDocument()
  })

  it('gives each row one accessible name rather than four fragments', () => {
    renderPanel(data({ transits: [position({ planet: 'Saturn', house_from_moon: 12 })] }))

    expect(
      screen.getByLabelText('Saturn, in Pisces, 12th house from your Moon'),
    ).toBeInTheDocument()
  })

  it('includes retrograde in the accessible name, not only as a glyph', () => {
    renderPanel(data({ transits: [position({ is_retrograde: true })] }))

    expect(screen.getByLabelText(/retrograde/)).toBeInTheDocument()
    expect(transitItems()[0]!).toHaveTextContent(RETROGRADE_MARK)
  })

  /**
   * Transits refresh every six hours by a worker.
   *
   * A panel headed "right now in the sky" that is silently four hours
   * stale is a small lie told confidently. The instant is one line, and
   * it is the difference between a reader trusting the screen and a
   * reader finding out later that they should not have.
   */
  it('shows when the positions were computed', () => {
    renderPanel(data({ at: '2026-09-16T12:00:00Z' }))
    expect(screen.getByText(/computed/i)).toHaveTextContent(/2026/)
  })

  it('says nothing has been computed rather than showing an empty list', () => {
    renderPanel(data({ transits: [] }))

    expect(screen.queryByRole('list', { name: /transiting planets/i })).toBeNull()
    expect(screen.getByText(/no transit positions have been computed/i)).toBeInTheDocument()
  })

  it('includes the Sade Sati indicator', () => {
    renderPanel(data())
    expect(screen.getByText(/sade sati/i)).toBeInTheDocument()
    expect(screen.getByText(/— current phase/i)).toBeInTheDocument()
  })
})
