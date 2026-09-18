import type { Meta, StoryObj } from '@storybook/nextjs-vite'

import { PlanetTable } from './PlanetTable'
import type { PlanetPlacement } from './types'

/**
 * The planetary table.
 *
 * ── Why the empty story exists ──
 *
 * A chart computed without a birth time has planets but the table can
 * still receive an empty list — a partially-stored chart, a filter that
 * matched nothing. In the running app that state needs a deliberately
 * broken fixture to reach, so nobody reaches it, and it is consequently
 * the branch most likely to render a bare heading with nothing under it
 * for months.
 *
 * Here it is one click.
 */
const meta = {
  title: 'Chart/PlanetTable',
  component: PlanetTable,
} satisfies Meta<typeof PlanetTable>

export default meta
type Story = StoryObj<typeof meta>

function planet(overrides: Partial<PlanetPlacement> = {}): PlanetPlacement {
  return {
    planet: 'Sun',
    sign: 'Leo',
    signIndex: 4,
    degree: 5.25,
    house: 10,
    nakshatra: 'Magha',
    pada: 2,
    isRetrograde: false,
    isCombust: false,
    dignity: 'own',
    ...overrides,
  }
}

/** The ordinary case: nine grahas, as the engine returns them. */
export const Populated: Story = {
  args: {
    planets: [
      planet(),
      planet({ planet: 'Moon', sign: 'Capricorn', signIndex: 9, degree: 12.5, house: 3, nakshatra: 'Shravana', pada: 2, dignity: 'neutral' }),
      planet({ planet: 'Mars', sign: 'Aries', signIndex: 0, degree: 28.9, house: 6, nakshatra: 'Bharani', pada: 4, dignity: 'own' }),
      planet({ planet: 'Mercury', sign: 'Virgo', signIndex: 5, degree: 2.1, house: 11, nakshatra: 'Uttara Phalguni', pada: 3, dignity: 'exalted' }),
      planet({ planet: 'Jupiter', sign: 'Sagittarius', signIndex: 8, degree: 19.7, house: 2, nakshatra: 'Purva Ashadha', pada: 1, dignity: 'own' }),
      // Retrograde and combust both need a glyph, not only a colour —
      // this row is why.
      planet({ planet: 'Venus', sign: 'Cancer', signIndex: 3, degree: 8.4, house: 9, nakshatra: 'Pushya', pada: 2, isRetrograde: true, dignity: 'neutral' }),
      planet({ planet: 'Saturn', sign: 'Pisces', signIndex: 11, degree: 22.3, house: 5, nakshatra: 'Revati', pada: 1, isCombust: true, dignity: 'debilitated' }),
      planet({ planet: 'Rahu', sign: 'Taurus', signIndex: 1, degree: 14.0, house: 7, nakshatra: 'Rohini', pada: 3, dignity: 'neutral' }),
      planet({ planet: 'Ketu', sign: 'Scorpio', signIndex: 7, degree: 14.0, house: 1, nakshatra: 'Anuradha', pada: 1, dignity: 'neutral' }),
    ],
  },
}

/**
 * Empty.
 *
 * The state the running app makes hard to reach and therefore the one
 * that rots. It must say something, not render a heading over nothing.
 */
export const Empty: Story = {
  args: { planets: [] },
}

/**
 * One planet.
 *
 * Not a trivial variation on Populated: a table with a single row is
 * where a layout that assumes it can fill its container falls over, and
 * where "9 planets" hardcoded in a caption would show.
 */
export const SinglePlanet: Story = {
  args: { planets: [planet()] },
}

/**
 * Every dignity at once.
 *
 * Dignity is the field with the most distinct renderings — exalted,
 * own, neutral, debilitated — and the one where colour is most tempting
 * as the only signal. Seeing all four together is how you notice that
 * two of them look the same.
 */
export const EveryDignity: Story = {
  args: {
    planets: [
      planet({ planet: 'Mercury', dignity: 'exalted', sign: 'Virgo', signIndex: 5 }),
      planet({ planet: 'Sun', dignity: 'own', sign: 'Leo', signIndex: 4 }),
      planet({ planet: 'Moon', dignity: 'neutral', sign: 'Gemini', signIndex: 2 }),
      planet({ planet: 'Saturn', dignity: 'debilitated', sign: 'Aries', signIndex: 0 }),
    ],
  },
}
