import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { defineTerm } from '@ayana/content'

import { HOUSE_TERMS, HouseList } from './HouseList'
import { ordinal } from './glyphs'
import type { HousePlacement, PlanetPlacement } from './types'
import { LocaleProvider } from '@/lib/i18n/context'

const SIGNS = [
  'Aries',
  'Taurus',
  'Gemini',
  'Cancer',
  'Leo',
  'Virgo',
  'Libra',
  'Scorpio',
  'Sagittarius',
  'Capricorn',
  'Aquarius',
  'Pisces',
]

const LORDS = [
  'Mars',
  'Venus',
  'Mercury',
  'Moon',
  'Sun',
  'Mercury',
  'Venus',
  'Mars',
  'Jupiter',
  'Saturn',
  'Saturn',
  'Jupiter',
]

function houses(): HousePlacement[] {
  return Array.from({ length: 12 }, (_, i) => ({
    house: (i + 1) as HousePlacement['house'],
    sign: SIGNS[i]!,
    signIndex: i as HousePlacement['signIndex'],
    lord: LORDS[i]!,
  }))
}

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

function renderList(ui: React.ReactElement) {
  return render(<LocaleProvider>{ui}</LocaleProvider>)
}

describe('HouseList', () => {
  it('renders all twelve houses', () => {
    renderList(<HouseList houses={houses()} planets={[]} />)
    expect(screen.getAllByRole('listitem')).toHaveLength(12)
  })

  it('shows each house sign and lord', () => {
    renderList(<HouseList houses={houses()} planets={[]} />)

    const seventh = screen.getAllByRole('listitem')[6]!
    expect(seventh).toHaveTextContent('7th')
    expect(seventh).toHaveTextContent('Libra')
    expect(seventh).toHaveTextContent('Venus')
  })

  it('places each planet in the house it says it is in', () => {
    renderList(
      <HouseList
        houses={houses()}
        planets={[planet({ planet: 'Saturn', house: 11 }), planet({ planet: 'Mars', house: 3 })]}
      />,
    )

    const items = screen.getAllByRole('listitem')
    expect(items[10]).toHaveTextContent('Saturn')
    expect(items[2]).toHaveTextContent('Mars')
    // And not anywhere else.
    expect(items[2]).not.toHaveTextContent('Saturn')
  })

  // A blank row is indistinguishable from a row that failed to render.
  it('says an empty house is empty rather than showing nothing', () => {
    renderList(<HouseList houses={houses()} planets={[planet({ house: 11 })]} />)
    expect(screen.getAllByRole('listitem')[0]).toHaveTextContent('empty')
  })

  // The row reads visually as "7th · Libra · lord Venus · 2" — four
  // fragments a screen reader runs together into something that is not a
  // sentence. Spelled out on the button, so the row is comprehensible
  // without seeing it.
  it('gives each row an accessible name, not four fragments', () => {
    renderList(<HouseList houses={houses()} planets={[planet({ house: 7 })]} />)

    expect(
      screen.getByRole('button', { name: /7th house, Libra, ruled by Venus, Saturn/i }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /1st house, Aries, ruled by Mars, empty/i }),
    ).toBeInTheDocument()
  })

  it('opens a detail sheet naming the house', async () => {
    const user = userEvent.setup()
    renderList(<HouseList houses={houses()} planets={[planet({ house: 7 })]} />)

    await user.click(screen.getByRole('button', { name: /7th house/i }))

    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveTextContent('7th house')
    expect(dialog).toHaveTextContent('Libra')
    expect(dialog).toHaveTextContent('Saturn')
  })

  /**
   * Without a birth time the ascendant is a guess, so the houses are not
   * missing — they are undefined. `null` must not render as twelve empty
   * rows, and it must not render as a generic failure either: the reader
   * can fix this, and only if they are told what to fix.
   */
  it('explains the absence when there is no birth time, rather than showing empty houses', () => {
    renderList(<HouseList houses={null} planets={[planet()]} />)

    expect(screen.queryByRole('listitem')).toBeNull()
    expect(screen.getByText(/birth time/i)).toBeInTheDocument()
  })
})

/**
 * The off-by-one guard.
 *
 * This fired for real while the component was being written. The corpus
 * had eleven `*_bhava` terms and no third house, so `HOUSE_TERMS[3]`
 * pointed at `sukha_bhava` — the FOURTH house — and the 3rd house
 * rendered "Home, mother, comfort" under the heading "3rd". Nothing
 * about that looks wrong on screen, and no other test in this file would
 * have caught it: the names, the lords and the occupants were all fine.
 */
describe('house glossary terms', () => {
  it('has a term for all twelve houses', () => {
    const missing: number[] = []
    for (let house = 1; house <= 12; house++) {
      if (!HOUSE_TERMS[house]) missing.push(house)
    }
    expect(missing).toEqual([])
  })

  it('gives no two houses the same term', () => {
    const terms = HOUSE_TERMS.slice(1)
    expect(new Set(terms).size, `duplicated: ${terms.join(', ')}`).toBe(12)
  })

  /**
   * The assertion that would have caught it on its own.
   *
   * Each glossary entry's English name is the ordinal of its house —
   * "Third house", "Fourth house". Comparing that against the index the
   * term sits at is a direct check that the mapping is aligned, rather
   * than a check that it is merely populated and unique.
   */
  it('points each house at the entry named for that house', () => {
    // The corpus spells these out — "Third house", not "3rd house" —
    // so this cannot reuse `ordinal`.
    const WORDS = [
      '',
      'first',
      'second',
      'third',
      'fourth',
      'fifth',
      'sixth',
      'seventh',
      'eighth',
      'ninth',
      'tenth',
      'eleventh',
      'twelfth',
    ]
    const mismatched: string[] = []

    for (let house = 1; house <= 12; house++) {
      const term = HOUSE_TERMS[house]!
      const entry = defineTerm(term, 'en')
      if (!entry) {
        mismatched.push(`house ${house}: "${term}" has no entry`)
        continue
      }
      const expected = `${WORDS[house]} house`
      if (entry.name.toLowerCase() !== expected) {
        mismatched.push(`house ${house} (${ordinal(house)}) -> "${term}" is "${entry.name}"`)
      }
    }

    expect(
      mismatched,
      'a house pointing at its neighbour’s definition renders a completely plausible ' +
        'screen: right ordinal, wrong meaning.',
    ).toEqual([])
  })
})
