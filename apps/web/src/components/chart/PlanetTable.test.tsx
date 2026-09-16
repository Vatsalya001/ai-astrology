import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { PlanetTable } from './PlanetTable'
import { RETROGRADE_MARK } from './glyphs'
import type { PlanetPlacement } from './types'
import { LocaleProvider } from '@/lib/i18n/context'

function planet(overrides: Partial<PlanetPlacement> = {}): PlanetPlacement {
  return {
    planet: 'Saturn',
    sign: 'Aquarius',
    signIndex: 10,
    degree: 19.78,
    house: 11,
    nakshatra: 'Shatabhisha',
    pada: 3,
    isRetrograde: false,
    isCombust: false,
    dignity: 'own_sign',
    ...overrides,
  } as PlanetPlacement
}

function renderTable(planets: PlanetPlacement[], onSelect?: (p: PlanetPlacement) => void) {
  return render(
    <LocaleProvider>
      <PlanetTable planets={planets} onSelect={onSelect} />
    </LocaleProvider>,
  )
}

describe('PlanetTable', () => {
  it('renders a row per planet', () => {
    renderTable([planet({ planet: 'Sun' }), planet({ planet: 'Moon' }), planet()])
    // Header row plus three.
    expect(screen.getAllByRole('row')).toHaveLength(4)
  })

  it('shows sign, degree, house and nakshatra', () => {
    renderTable([planet({ degree: 19.78, house: 11 })])

    const row = screen.getAllByRole('row')[1]!
    expect(row).toHaveTextContent('Aquarius')
    expect(row).toHaveTextContent("19°46'")
    expect(row).toHaveTextContent('11th')
    expect(row).toHaveTextContent('Shatabhisha 3')
  })

  /**
   * The roles are explicit because the mobile restack changes `display`
   * on rows and cells, which has historically dropped them from the
   * table mapping in the accessibility tree.
   *
   * Measured, it does not happen in the Chromium this project tests
   * against — see the note in PlanetTable.tsx, which asserted otherwise
   * until it was checked. This test therefore guards the attributes, not
   * the browser behaviour: it fails if somebody removes a role, which is
   * all a unit test can see.
   *
   * What the tree ACTUALLY does at 360px is asserted in
   * `tests/e2e/kundli-planets.spec.ts` via `ariaSnapshot`. Axe cannot
   * see it — it evaluates implicit ARIA on the markup and stays green
   * with the roles removed, which was verified by removing one.
   */
  it('states its table roles explicitly so the mobile restack cannot strip them', () => {
    const { container } = renderTable([planet()])

    expect(container.querySelector('table')).toHaveAttribute('role', 'table')
    expect(container.querySelector('tbody tr')).toHaveAttribute('role', 'row')
    expect(container.querySelector('tbody td')).toHaveAttribute('role', 'cell')
    expect(container.querySelector('thead th')).toHaveAttribute('role', 'columnheader')
  })

  // The pseudo-element label at mobile reads from this attribute. Without
  // it a restacked card is a column of bare values with no headings.
  it('labels every cell for the restacked view', () => {
    const { container } = renderTable([planet()])

    const labels = [...container.querySelectorAll('tbody td')].map((td) =>
      td.getAttribute('data-label'),
    )
    expect(labels).toEqual(['Planet', 'Sign', 'Degree', 'House', 'Nakshatra', 'Status'])
  })

  // ── meaning never carried by colour alone ──

  it('marks retrograde with a glyph and a word, not a colour', () => {
    renderTable([planet({ isRetrograde: true })])

    const row = screen.getAllByRole('row')[1]!
    expect(row).toHaveTextContent(RETROGRADE_MARK)
    // And in words, for a reader who does not know ℞.
    expect(within(row).getByText('retrograde')).toBeInTheDocument()
  })

  it('names combustion rather than only ringing the glyph', () => {
    renderTable([planet({ isCombust: true })])
    expect(screen.getAllByRole('row')[1]!).toHaveTextContent('Combust')
  })

  /**
   * "Neutral" is not a term of art, it is the absence of one, and a
   * tooltip reading "the planet is not exalted, debilitated, in its own
   * sign or in moolatrikona" teaches nobody anything.
   *
   * Asserted by name rather than by counting buttons in the row: the row
   * legitimately has two already — the planet and its nakshatra — and a
   * count would pass or fail for reasons that have nothing to do with
   * dignity the moment another cell becomes tappable.
   */
  it('renders a neutral dignity as an em dash with nothing to tap', () => {
    renderTable([planet({ dignity: 'neutral' })])

    const row = screen.getAllByRole('row')[1]!
    expect(row).toHaveTextContent('—')

    // Both the label AND the text. Written `aria-label ?? textContent`
    // first, which was vacuous: AstroTerm's label is "What “Dignity”
    // means", so the `??` never reached the text and making the em dash
    // tappable did not fail the test.
    const buttonText = within(row)
      .getAllByRole('button')
      .map((b) => `${b.getAttribute('aria-label') ?? ''} ${b.textContent ?? ''}`)

    expect(buttonText.filter((name) => name.includes('—'))).toEqual([])
    expect(buttonText.filter((name) => /neutral/i.test(name))).toEqual([])
  })

  it('makes a real dignity tappable', () => {
    renderTable([planet({ dignity: 'exalted' })])

    const row = screen.getAllByRole('row')[1]!
    expect(within(row).getByRole('button', { name: /exalted/i })).toBeInTheDocument()
  })

  // ── the detail sheet ──

  it('opens a sheet with the full position', async () => {
    const user = userEvent.setup()
    renderTable([planet({ isRetrograde: true })])

    await user.click(screen.getByRole('button', { name: /Saturn in Aquarius/i }))

    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveTextContent('Saturn')
    expect(dialog).toHaveTextContent('Shatabhisha 3')
    expect(dialog).toHaveTextContent('Own sign')
    expect(dialog).toHaveTextContent('Retrograde')
  })

  it('reports the selection to the caller as well as opening the sheet', async () => {
    const user = userEvent.setup()
    const onSelect = vi.fn()
    renderTable([planet()], onSelect)

    await user.click(screen.getByRole('button', { name: /Saturn in Aquarius/i }))

    // Phase 5's "why am I seeing this?" needs the callback; the sheet is
    // the default behaviour when nothing is listening. Both, not either.
    expect(onSelect).toHaveBeenCalledOnce()
    expect(onSelect.mock.calls[0]![0].planet).toBe('Saturn')
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('describes the whole placement in the row trigger label', () => {
    renderTable([planet({ isRetrograde: true, isCombust: true, degree: 19.78 })])

    expect(
      screen.getByRole('button', {
        name: 'Saturn in Aquarius, 11th house, 20 degrees, retrograde, combust',
      }),
    ).toBeInTheDocument()
  })

  it('says so when there is nothing to show', () => {
    renderTable([])
    expect(screen.queryByRole('table')).toBeNull()
    expect(screen.getByText(/no planetary positions/i)).toBeInTheDocument()
  })
})
