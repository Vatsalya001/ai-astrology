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
