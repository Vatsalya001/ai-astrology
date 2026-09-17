import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { defineTerm } from '@ayana/content'

import { STYLE_TERMS, StyleSwitcher } from './StyleSwitcher'
import { CHART_STYLES, DEFAULT_CHART_STYLE, toChartStyle } from './style'
import { LocaleProvider } from '@/lib/i18n/context'

function renderSwitcher(value: 'north' | 'south' = 'north', onChange = vi.fn()) {
  render(
    <LocaleProvider>
      <StyleSwitcher value={value} onChange={onChange} />
    </LocaleProvider>,
  )
  return onChange
}

describe('StyleSwitcher', () => {
  it('offers exactly the styles the geometry can draw', () => {
    renderSwitcher()
    expect(screen.getAllByRole('radio')).toHaveLength(CHART_STYLES.length)
  })

  it('exposes the selection as a checked radio', () => {
    renderSwitcher('south')

    expect(screen.getByRole('radio', { name: /south/i })).toBeChecked()
    expect(screen.getByRole('radio', { name: /north/i })).not.toBeChecked()
  })

  it('reports a change', async () => {
    const user = userEvent.setup()
    const onChange = renderSwitcher('north')

    await user.click(screen.getByRole('radio', { name: /south/i }))
    expect(onChange).toHaveBeenCalledWith('south')
  })

  it('is operable from the keyboard', async () => {
    const user = userEvent.setup()
    const onChange = renderSwitcher('north')

    await user.tab()
    expect(screen.getByRole('radio', { name: /north/i })).toHaveFocus()

    await user.keyboard('{ArrowRight}')
    expect(onChange).toHaveBeenCalledWith('south')
  })

  // The two layouts look nothing like each other, so a reader whose
  // diagram changes shape needs to be told what it changed to.
  it('explains the selected style', () => {
    renderSwitcher('south')
    expect(screen.getByText(/grid layout/i)).toBeInTheDocument()
  })

  it('marks the selection with something other than colour', () => {
    const { container } = render(
      <LocaleProvider>
        <StyleSwitcher value="south" onChange={vi.fn()} />
      </LocaleProvider>,
    )

    const dots = [...container.querySelectorAll('span[aria-hidden="true"]')].filter((el) =>
      el.className.includes('rounded-full'),
    )
    expect(dots).toHaveLength(CHART_STYLES.length)
    expect(dots.filter((el) => el.className.includes('bg-gold'))).toHaveLength(1)
  })

  it('can be disabled while a chart is loading', () => {
    render(
      <LocaleProvider>
        <StyleSwitcher value="north" onChange={vi.fn()} disabled />
      </LocaleProvider>,
    )
    for (const radio of screen.getAllByRole('radio')) expect(radio).toBeDisabled()
  })
})

/**
 * The promise `astro-term-usage.test.ts` allows this file's computed
 * `term={…}` on: typed as GlossaryKey, and the mapping checked — because
 * a well-typed key can still point at the wrong entry.
 */
describe('style glossary terms', () => {
  it('defines a term for every style', () => {
    const missing = CHART_STYLES.filter((s) => !defineTerm(STYLE_TERMS[s], 'en'))
    expect(missing).toEqual([])
  })

  it('points each style at the entry for that style', () => {
    const mismatched: string[] = []
    for (const style of CHART_STYLES) {
      const entry = defineTerm(STYLE_TERMS[style], 'en')!
      if (!entry.name.toLowerCase().includes(style)) {
        mismatched.push(`${style} -> "${entry.name}"`)
      }
    }
    expect(mismatched).toEqual([])
  })
})

/**
 * The gap `style.ts` exists to close.
 *
 * `user_preferences.chart_style` is a free string, the settings screen
 * offered `['north', 'south', 'east']`, and the geometry package has no
 * East Indian polygons at all — so a reader could pick East, have it
 * saved, and be handed a layout nothing could render. It was invisible
 * only because no screen mounted ChartSVG until now.
 */
describe('toChartStyle', () => {
  it('passes through a style the geometry can draw', () => {
    expect(toChartStyle('north')).toBe('north')
    expect(toChartStyle('south')).toBe('south')
  })

  it('falls back for East, which the geometry cannot draw', () => {
    // Accounts created before the picker dropped it still hold this.
    expect(toChartStyle('east')).toBe(DEFAULT_CHART_STYLE)
  })

  it('falls back for anything unusable rather than throwing', () => {
    for (const bad of [null, undefined, '', 'NORTH', 'diamond', 'north ']) {
      expect(toChartStyle(bad), JSON.stringify(bad)).toBe(DEFAULT_CHART_STYLE)
    }
  })

  // If the geometry ever gains East, this list is what has to change —
  // and this assertion is what makes that obvious rather than silent.
  it('offers only what the geometry package declares', () => {
    expect([...CHART_STYLES].sort()).toEqual(['north', 'south'])
  })
})
