import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { PREDICTIVE_PHRASES, YOGA_KEYS, describeYoga } from '@ayana/content'

import { YogaCard } from './YogaCard'
import type { YogaPlacement } from './types'
import { LocaleProvider } from '@/lib/i18n/context'

function yoga(overrides: Partial<YogaPlacement> = {}): YogaPlacement {
  return {
    name: 'Gajakesari Yoga',
    strength: 'strong',
    involvedPlanets: ['Jupiter', 'Moon'],
    involvedHouses: [4],
    ...overrides,
  }
}

function renderCard(y: YogaPlacement) {
  return render(
    <LocaleProvider>
      <YogaCard yoga={y} />
    </LocaleProvider>,
  )
}

describe('YogaCard', () => {
  it('shows the yoga name and its written description', () => {
    renderCard(yoga())

    const card = screen.getByRole('button')
    expect(card).toHaveTextContent('Gajakesari Yoga')
    expect(card).toHaveTextContent(describeYoga('Gajakesari Yoga', 'en')!.short)
  })

  it('names the involved planets and houses', () => {
    renderCard(yoga({ involvedPlanets: ['Jupiter', 'Moon'], involvedHouses: [4] }))

    const card = screen.getByRole('button')
    expect(card).toHaveTextContent('Jupiter, Moon')
    expect(card).toHaveTextContent('4th house')
  })

  it('opens a sheet with the long description', async () => {
    const user = userEvent.setup()
    renderCard(yoga())

    await user.click(screen.getByRole('button'))
    expect(screen.getByRole('dialog')).toHaveTextContent(
      describeYoga('Gajakesari Yoga', 'en')!.long,
    )
  })

  // ── strength stays a word ───────────────────────────────────────────

  /**
   * The single most tempting thing to build on this card, and the one
   * astro-service explicitly forbids.
   *
   * Its schema says: "never a number — a score would imply a precision
   * the tradition does not have and the product could not defend". A
   * five-star rating, a percentage or a 0–100 bar is that number wearing
   * a costume, and it would look like an improvement in review.
   */
  it('prints strength as a word, with no number anywhere', () => {
    for (const strength of ['strong', 'moderate']) {
      const { container, unmount } = renderCard(yoga({ strength, involvedHouses: [] }))
      const text = container.textContent ?? ''

      expect(text, strength).toMatch(/Strong|Moderate/)
      expect(text, `${strength} produced a digit`).not.toMatch(/\d/)
      unmount()
    }
  })

  it('exposes no progressbar or meter for strength', () => {
    renderCard(yoga())
    expect(screen.queryByRole('progressbar')).toBeNull()
    expect(screen.queryByRole('meter')).toBeNull()
  })

  it('distinguishes the two strengths by more than colour', () => {
    const { container: strongEl } = renderCard(yoga({ strength: 'strong' }))
    expect(strongEl.textContent).toContain('Strong')

    const { container: moderateEl } = renderCard(yoga({ strength: 'moderate' }))
    expect(moderateEl.textContent).toContain('Moderate')
  })

  // An unrecognised strength must not read as the stronger one:
  // overstating a combination is the worse direction to be wrong in.
  it('treats an unrecognised strength as moderate, not strong', () => {
    renderCard(yoga({ strength: 'overwhelming' }))
    expect(screen.getByRole('button')).toHaveTextContent('Moderate')
  })

  // ── degradation ─────────────────────────────────────────────────────

  /**
   * A name the corpus has no entry for.
   *
   * `yogas.test.ts` makes this a failing build, so it should never be
   * reachable — but the card is what renders if it ever is, and a
   * heading over a blank reads as a rendering failure. It says so
   * instead, and crucially does NOT compose a description from the
   * planets and houses, which would be the frontend generating
   * interpretation.
   */
  it('says a description is missing rather than inventing one', () => {
    renderCard(yoga({ name: 'Unwritten Yoga' }))

    const card = screen.getByRole('button')
    expect(card).toHaveTextContent('Unwritten Yoga')
    expect(card).toHaveTextContent(/no description written/i)
    expect(card).not.toHaveTextContent(/traditionally|associated with/i)
  })

  it('renders without planets or houses', () => {
    renderCard(yoga({ involvedPlanets: [], involvedHouses: [] }))
    expect(screen.getByRole('button')).toHaveTextContent('Gajakesari Yoga')
  })

  it('names the yoga and its strength in the accessible label', () => {
    renderCard(yoga())
    expect(
      screen.getByRole('button', { name: /Gajakesari Yoga, strong strength, Jupiter and Moon/i }),
    ).toBeInTheDocument()
  })
})

/**
 * The framing rule, checked at the point of RENDER.
 *
 * `yogas.test.ts` checks the corpus. This checks that what reaches the
 * screen is the corpus and nothing composed around it — the card could
 * be perfectly safe and still be wrapped in a sentence like "this brings
 * you…" added later in the component.
 */
describe('what the card actually renders', () => {
  it('uses no predictive framing for any described yoga, in either state', () => {
    const offences: string[] = []

    for (const key of YOGA_KEYS) {
      for (const strength of ['strong', 'moderate']) {
        const { container, unmount } = renderCard(yoga({ name: key, strength }))
        const text = (container.textContent ?? '').toLowerCase()
        for (const phrase of PREDICTIVE_PHRASES) {
          if (text.includes(phrase)) offences.push(`${key}/${strength}: "${phrase}"`)
        }
        unmount()
      }
    }

    expect(offences).toEqual([])
  })

  it('never characterises a yoga as bad for the reader', () => {
    const condemning = /\b(unfortunate|misfortune|suffer|poverty|doomed|cursed|disaster)\b/i
    const offences: string[] = []

    for (const key of YOGA_KEYS) {
      const { container, unmount } = renderCard(yoga({ name: key }))
      if (condemning.test(container.textContent ?? '')) offences.push(key)
      unmount()
    }

    expect(offences).toEqual([])
  })

  // Proves the loop above rendered something, rather than iterating an
  // empty list and reporting clean.
  it('rendered every yoga in the corpus', () => {
    expect(YOGA_KEYS.length).toBeGreaterThanOrEqual(10)

    renderCard(yoga({ name: YOGA_KEYS[0]! }))
    expect(screen.getByRole('button')).toHaveTextContent(
      describeYoga(YOGA_KEYS[0]!, 'en')!.name,
    )
  })
})
