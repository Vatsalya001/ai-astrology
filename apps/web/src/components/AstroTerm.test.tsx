import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { GLOSSARY_KEYS, defineTerm } from '@ayana/content'

import { AstroTerm } from './AstroTerm'
import { LocaleProvider } from '@/lib/i18n/context'
import { LOCALE_NAMES, type Locale } from '@/lib/i18n/dictionaries'

function renderTerm(ui: React.ReactElement) {
  return render(<LocaleProvider>{ui}</LocaleProvider>)
}

describe('AstroTerm', () => {
  it('renders a known term as a button', async () => {
    renderTerm(<AstroTerm term="nakshatra" />)

    const button = screen.getByRole('button')
    expect(button).toHaveTextContent('Nakshatra')
  })

  it('opens the definition on click', async () => {
    const user = userEvent.setup()
    renderTerm(<AstroTerm term="nakshatra" />)

    expect(screen.queryByRole('dialog')).toBeNull()
    await user.click(screen.getByRole('button'))

    const dialog = screen.getByRole('dialog')
    const entry = defineTerm('nakshatra', 'en')!
    expect(within(dialog).getByText(entry.long)).toBeInTheDocument()
  })

  // Hover does not exist on a phone, which is the majority case. If this
  // ever becomes a `title` attribute or a hover card, most users lose
  // access to every definition in the product and nothing else fails.
  it('opens on keyboard activation too, not only pointer', async () => {
    const user = userEvent.setup()
    renderTerm(<AstroTerm term="nakshatra" />)

    await user.tab()
    expect(screen.getByRole('button')).toHaveFocus()

    await user.keyboard('{Enter}')
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('names the term in the accessible label, not just "button"', () => {
    renderTerm(<AstroTerm term="nakshatra" />)

    expect(screen.getByRole('button')).toHaveAccessibleName(/nakshatra/i)
  })

  it('renders custom children rather than the canonical name', () => {
    renderTerm(<AstroTerm term="nakshatra">lunar mansions</AstroTerm>)

    expect(screen.getByRole('button')).toHaveTextContent('lunar mansions')
  })

  // ── degradation ────────────────────────────────────────────────────

  // The contract `defineTerm` sets: a missing definition costs a tooltip,
  // never the sentence. Asserting the text SURVIVES is the point — an
  // implementation that returned `null` from the component would pass a
  // "no button is rendered" test while deleting words from the page.
  it('renders an unknown term as plain text, keeping the words', () => {
    renderTerm(<AstroTerm term="not_a_real_term">Gandanta</AstroTerm>)

    expect(screen.queryByRole('button')).toBeNull()
    expect(screen.getByText('Gandanta')).toBeInTheDocument()
  })

  it('does not throw on an unknown term', () => {
    expect(() => renderTerm(<AstroTerm term="__proto__">x</AstroTerm>)).not.toThrow()
    expect(screen.getByText('x')).toBeInTheDocument()
  })
})

/**
 * The guard for the silence.
 *
 * `AstroTerm` degrades to plain text when a term has no entry, without
 * throwing and without logging. That is correct for one missing word and
 * catastrophic for a missing LOCALE: every term on every screen would
 * quietly become unremarkable text, and nothing — not a test, not the
 * console, not the build — would say so.
 *
 * `@ayana/config` already declares a third locale (`hinglish`) that the
 * corpus does not cover. Nothing reaches it today because the picker is
 * driven by `LOCALE_NAMES`, which is keyed off `dictionaries`. The day
 * someone adds a dictionary for it, the picker offers it automatically
 * and this test fails — which is the whole idea.
 */
describe('locale coverage', () => {
  const offered = Object.keys(LOCALE_NAMES) as Locale[]

  it('defines every glossary term in every locale the picker offers', () => {
    const missing: string[] = []

    for (const locale of offered) {
      for (const key of GLOSSARY_KEYS) {
        if (!defineTerm(key, locale)) missing.push(`${key}.${locale}`)
      }
    }

    expect(
      missing,
      `The language picker offers [${offered.join(', ')}], and AstroTerm renders a term ` +
        'with no entry as plain text — silently. A locale missing from the corpus turns ' +
        'every definition in the product into ordinary words with nothing to report it.',
    ).toEqual([])
  })

  // Proves the check above is actually looking, rather than iterating an
  // empty list or a locale set that happens to be covered by accident.
  it('would notice a locale the corpus does not cover', () => {
    const uncovered = 'hinglish' as Locale
    expect(offered).not.toContain(uncovered)
    expect(defineTerm(GLOSSARY_KEYS[0]!, uncovered)).toBeNull()
  })
})

/**
 * The label must CARRY the children, never replace them.
 *
 * ── The bug this pins ──
 *
 * `aria-label` was set unconditionally to "What “{term}” means". That is
 * right where the children ARE the term — `<AstroTerm term="nakshatra"/>`
 * renders the word "Nakshatra" and nothing is lost — and destroys data
 * where the children are a VALUE.
 *
 * `PlanetTable.tsx` passes the value:
 *   <AstroTerm term="nakshatra">{formatNakshatra(…)}</AstroTerm>
 *
 * `aria-label` wins accname over name-from-content, so every one of the
 * nine Nakshatra cells announced "What “Nakshatra” means" instead of
 * "Purva Ashadha 3". Confirmed in a browser against Playwright's accname
 * implementation — the Moon's row name came out as
 *   Moon in Sagittarius, 4th house, 21 degrees Sagittarius 20°44' 4th
 *   What “Nakshatra” means —
 * with the nakshatra absent from the row entirely. Not derivable by ear
 * from the sign and degree that ARE announced, and a voice-control user
 * saying "click Purva Ashadha 3" hit nothing.
 *
 * ── Why the suite did not catch it ──
 *
 * `PlanetTable.test.tsx:47` asserts `toHaveTextContent('Shatabhisha 3')`
 * — DOM text, which was intact the whole time. The test above,
 * "renders custom children rather than the canonical name", asserts
 * `toHaveTextContent` too. Every assertion in reach was about what is on
 * the screen; none about what is announced. `toHaveAccessibleName` is
 * the distinction, and it is the whole bug.
 *
 * axe cannot see it either: it checks a name EXISTS, never that it
 * preserves what it replaced.
 */
describe('the accessible name when the children are data', () => {
  it('announces the value before the affordance', () => {
    renderTerm(<AstroTerm term="nakshatra">Purva Ashadha 3</AstroTerm>)

    const button = screen.getByRole('button')

    expect(
      button,
      'the glossary label replaced the value it wraps, so a reader hears ' +
        '"What “Nakshatra” means" where the data should be',
    ).toHaveAccessibleName(/^Purva Ashadha 3\./)

    // The affordance must survive too — without it the control announces
    // as a value with no hint that pressing it does anything.
    expect(button).toHaveAccessibleName(/nakshatra/i)
  })

  it('keeps the plain label when there are no children at all', () => {
    renderTerm(<AstroTerm term="nakshatra" />)

    expect(screen.getByRole('button').getAttribute('aria-label')).not.toMatch(
      /nakshatra\.\s*what/i,
    )
  })

  it('does not stutter when the children ARE the term', () => {
    /*
      The regression the first fix shipped, and the reason this test
      exists in this exact shape.

      The guard above renders <AstroTerm term="nakshatra" /> with NO
      children, so it exercises the `entry.name` fallback — a branch the
      bug never touched. The real call sites pass the term as children:

        <AstroTerm term="rasi">Rasi</AstroTerm>

      and prefixing unconditionally produced
      `button "Rasi. What “Rasi” means"`, live on the chart page. A guard
      aimed one branch away from the defect is not a guard.
    */
    renderTerm(<AstroTerm term="rasi">Rasi</AstroTerm>)

    expect(
      screen.getByRole('button').getAttribute('aria-label'),
      'the term is announced twice — once as the value, once inside the ' +
        'affordance. Prefix only when the children differ from the name.',
    ).not.toMatch(/rasi\W+what/i)
  })

  it('ignores case when deciding whether it would stutter', () => {
    /*
      Call sites lower-case a term to fit a sentence — "the rasi chart".
      "rasi" and "Rasi" are the same word said twice, and an exact string
      compare would let that through.

      `first_house` was the first spelling of this test and it passed
      vacuously: no such glossary key exists, so `defineTerm` returned
      null, the component rendered plain text with no button at all, and
      `getByRole('button')` was querying something that was never there.
      A real term is required for the assertion to reach the code.
    */
    renderTerm(<AstroTerm term="rasi">rasi</AstroTerm>)

    const name = screen.getByRole('button').getAttribute('aria-label') ?? ''
    expect(name).not.toMatch(/rasi\W+what/i)
  })

  it('falls back rather than announcing an object', () => {
    /*
      `children` is typed ReactNode. A label built by interpolating
      "[object Object]" into speech would be worse than the bug it
      replaces, so a non-text child yields the plain affordance label.
    */
    renderTerm(
      <AstroTerm term="nakshatra">
        <em>styled</em>
      </AstroTerm>,
    )

    const name = screen.getByRole('button').getAttribute('aria-label') ?? ''
    expect(name).not.toMatch(/\[object/i)
    expect(name).toMatch(/nakshatra/i)
  })
})
