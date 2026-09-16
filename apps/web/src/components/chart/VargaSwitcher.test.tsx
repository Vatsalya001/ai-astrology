import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { VargaSwitcher } from './VargaSwitcher'
import { VARGAS } from '@/lib/varga'
import { LocaleProvider } from '@/lib/i18n/context'

function renderSwitcher(value = 'D1', onChange = vi.fn()) {
  render(
    <LocaleProvider>
      <VargaSwitcher value={value} onChange={onChange} />
    </LocaleProvider>,
  )
  return onChange
}

describe('VargaSwitcher', () => {
  it('offers every chart the product renders', () => {
    renderSwitcher()

    const radios = screen.getAllByRole('radio')
    expect(radios).toHaveLength(VARGAS.length)
    for (const v of VARGAS) {
      expect(screen.getByRole('radio', { name: new RegExp(v.name, 'i') })).toBeInTheDocument()
    }
  })

  /**
   * Real radios, not buttons with `aria-pressed`.
   *
   * Three mutually exclusive options is what a radio group is, and using
   * inputs means arrow-key navigation, the roving tab stop and the
   * checked state come from the browser rather than from hand-written
   * key handlers. This asserts the checked state is genuinely on the
   * input, because that is what assistive technology reads — a styled
   * label that merely looks selected announces nothing.
   */
  it('exposes the selection as a checked radio', () => {
    renderSwitcher('D9')

    expect(screen.getByRole('radio', { name: /navamsa/i })).toBeChecked()
    expect(screen.getByRole('radio', { name: /rasi/i })).not.toBeChecked()
  })

  it('reports a change', async () => {
    const user = userEvent.setup()
    const onChange = renderSwitcher('D1')

    await user.click(screen.getByRole('radio', { name: /dasamsa/i }))
    expect(onChange).toHaveBeenCalledWith('D10')
  })

  it('is reachable and operable from the keyboard', async () => {
    const user = userEvent.setup()
    const onChange = renderSwitcher('D1')

    await user.tab()
    expect(screen.getByRole('radio', { name: /rasi/i })).toHaveFocus()

    // Arrow keys within a radio group are the browser's, not ours —
    // which is the reason for using inputs in the first place.
    await user.keyboard('{ArrowRight}')
    expect(onChange).toHaveBeenCalledWith('D9')
  })

  /**
   * "D9" means nothing on first sight. A reader who presses it, sees a
   * completely different diagram and is told nothing concludes the app
   * is broken — so the purpose line is part of the control, not a nicety.
   */
  it('says what the selected chart is read for', () => {
    renderSwitcher('D10')
    expect(screen.getByText(/career, profession and public standing/i)).toBeInTheDocument()
  })

  it('updates the explanation when the selection changes', () => {
    const { rerender } = render(
      <LocaleProvider>
        <VargaSwitcher value="D1" onChange={vi.fn()} />
      </LocaleProvider>,
    )
    expect(screen.getByText(/everything else is derived from it/i)).toBeInTheDocument()

    rerender(
      <LocaleProvider>
        <VargaSwitcher value="D9" onChange={vi.fn()} />
      </LocaleProvider>,
    )
    expect(screen.getByText(/marriage, partnership and inner strength/i)).toBeInTheDocument()
  })

  // Selection is signalled by a gold border AND a filled dot. Around 8%
  // of men cannot separate the gold from the grey, and a segmented
  // control whose state only they cannot read is one that silently stops
  // working for them.
  it('marks the selection with something other than colour', () => {
    const { container } = render(
      <LocaleProvider>
        <VargaSwitcher value="D9" onChange={vi.fn()} />
      </LocaleProvider>,
    )

    const dots = [...container.querySelectorAll('span[aria-hidden="true"]')].filter((el) =>
      el.className.includes('rounded-full'),
    )
    expect(dots).toHaveLength(VARGAS.length)

    const filled = dots.filter((el) => el.className.includes('bg-gold'))
    expect(filled).toHaveLength(1)
  })

  /**
   * `value` is typed, but in practice it arrives from a URL parameter or
   * a stored preference. A chart the list has never heard of should show
   * the birth chart, not a blank panel and not a crash.
   */
  it('falls back to the rasi for a chart it does not know', () => {
    renderSwitcher('D60')
    expect(screen.getByText(/everything else is derived from it/i)).toBeInTheDocument()
  })

  it('can be disabled while a chart is loading', () => {
    render(
      <LocaleProvider>
        <VargaSwitcher value="D1" onChange={vi.fn()} disabled />
      </LocaleProvider>,
    )
    for (const radio of screen.getAllByRole('radio')) expect(radio).toBeDisabled()
  })
})
