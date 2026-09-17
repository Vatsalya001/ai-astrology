import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { MOON_SIGNS, PREDICTIVE_PHRASES, moonDay } from '@ayana/content'

import { AskBox } from './AskBox'
import { CurrentPeriodCard } from './CurrentPeriodCard'
import { TodayCard } from './TodayCard'
import { greeting } from '@/app/home/page'
import { en, interpolate } from '@/lib/i18n/dictionaries'
import type { CurrentDashas, DashaPeriod } from '@/lib/astrology-api'
import { LocaleProvider } from '@/lib/i18n/context'

function wrap(ui: React.ReactElement) {
  return render(<LocaleProvider>{ui}</LocaleProvider>)
}

describe('TodayCard', () => {
  it('names the transiting Moon sign and its written line', () => {
    wrap(<TodayCard moonSign="Taurus" />)

    expect(screen.getByText(/Moon in Taurus|in Taurus/)).toBeInTheDocument()
    expect(screen.getByText(moonDay('Taurus', 'en')!.short)).toBeInTheDocument()
  })

  it('has a line for every sign the engine can report', () => {
    for (const sign of MOON_SIGNS) {
      const { unmount } = wrap(<TodayCard moonSign={sign} />)
      expect(screen.queryByText(/no note written/i), sign).toBeNull()
      unmount()
    }
  })

  /**
   * The nakshatra and the tithi are absent on purpose.
   *
   * The spec's mock shows both. The transits endpoint returns neither,
   * and deriving them from the longitude here would be the frontend
   * computing astrology. This asserts the absence so that adding a
   * derived nakshatra is a failing test rather than a plausible-looking
   * improvement — the same guard as the Sade Sati dates.
   */
  it('shows no nakshatra and no tithi, because the engine reports neither', () => {
    const { container } = wrap(<TodayCard moonSign="Taurus" />)
    const text = container.textContent ?? ''

    expect(text).not.toMatch(/nakshatra|rohini|ashwini|tithi/i)
  })

  it('says the sky is not ready rather than showing a blank card', () => {
    wrap(<TodayCard moonSign={null} />)
    expect(screen.getByText(/still being prepared/i)).toBeInTheDocument()
  })

  it('does not invent a line for a sign it does not have', () => {
    wrap(<TodayCard moonSign="Ophiuchus" />)

    expect(screen.getByText(/no note written/i)).toBeInTheDocument()
    expect(screen.queryByText(/traditionally read as/i)).toBeNull()
  })

  it('uses no predictive framing for any sign', () => {
    const offences: string[] = []
    for (const sign of MOON_SIGNS) {
      const { container, unmount } = wrap(<TodayCard moonSign={sign} />)
      const text = (container.textContent ?? '').toLowerCase()
      for (const phrase of PREDICTIVE_PHRASES) {
        if (text.includes(phrase)) offences.push(`${sign}: "${phrase}"`)
      }
      unmount()
    }
    expect(offences).toEqual([])
  })
})

describe('AskBox', () => {
  /**
   * Really disabled, not styled to look inert.
   *
   * The usual way this goes wrong is a dimmed box that still takes
   * focus and swallows what somebody types. A keyboard user who reaches
   * it has been lied to more completely than one who never could.
   */
  it('disables the input and every topic when the flag is off', () => {
    wrap(<AskBox enabled={false} />)

    expect(screen.getByRole('textbox', { name: /ask your ai astrologer/i })).toBeDisabled()
    for (const button of screen.getAllByRole('button')) expect(button).toBeDisabled()
  })

  it('explains why rather than just dimming', () => {
    wrap(<AskBox enabled={false} />)

    const input = screen.getByRole('textbox')
    expect(input).toHaveAccessibleDescription(/not available yet/i)
  })

  it('enables everything when the flag is on', () => {
    wrap(<AskBox enabled />)

    expect(screen.getByRole('textbox')).toBeEnabled()
    for (const button of screen.getAllByRole('button')) expect(button).toBeEnabled()
    expect(screen.queryByText(/not available yet/i)).toBeNull()
  })

  it('offers the four topics the spec names', () => {
    wrap(<AskBox enabled />)
    for (const topic of ['Career', 'Love', 'Money', 'Marriage']) {
      expect(screen.getByRole('button', { name: topic })).toBeInTheDocument()
    }
  })
})

function period(overrides: Partial<DashaPeriod> = {}): DashaPeriod {
  return {
    id: 'a',
    planet: 'Jupiter',
    start: '2019-03-01T00:00:00Z',
    end: '2035-03-01T00:00:00Z',
    level: 1,
    ...overrides,
  }
}

function current(overrides: Partial<CurrentDashas> = {}): CurrentDashas {
  return {
    mahadasha: period(),
    antardasha: period({
      id: 'b',
      planet: 'Saturn',
      start: '2021-05-01T00:00:00Z',
      end: '2026-11-01T00:00:00Z',
      level: 2,
    }),
    pratyantardasha: null,
    at: '2027-03-01T00:00:00Z',
    ...overrides,
  }
}

describe('CurrentPeriodCard', () => {
  it('names the mahadasha and its span', () => {
    wrap(<CurrentPeriodCard current={current()} />)

    expect(screen.getByText(/Jupiter/)).toBeInTheDocument()
    expect(screen.getByText(/Mar 2019/)).toBeInTheDocument()
    expect(screen.getByText(/Mar 2035/)).toBeInTheDocument()
  })

  it('shows progress as a real progressbar with the percentage written out', () => {
    wrap(<CurrentPeriodCard current={current()} />)

    const bar = screen.getByRole('progressbar')
    const value = Number(bar.getAttribute('aria-valuenow'))

    // 2027 of 2019–2035 is half, give or take.
    expect(value).toBeGreaterThan(40)
    expect(value).toBeLessThan(60)
    expect(screen.getByText(new RegExp(`${value}% elapsed`))).toBeInTheDocument()
  })

  it('names the antardasha and when it ends', () => {
    wrap(<CurrentPeriodCard current={current()} />)
    expect(screen.getByText(/Saturn/)).toBeInTheDocument()
    expect(screen.getByText(/Nov 2026/)).toBeInTheDocument()
  })

  /**
   * No mahadasha means no dasha tree, which means no birth time.
   *
   * An empty card would read as a rendering failure; this names
   * something the reader can act on.
   */
  it('explains the absence when there is no mahadasha', () => {
    wrap(<CurrentPeriodCard current={current({ mahadasha: null, antardasha: null })} />)

    expect(screen.getByText(/need a birth time/i)).toBeInTheDocument()
    expect(screen.queryByRole('progressbar')).toBeNull()
  })

  // `toSpan` returns null for unparseable dates rather than NaN. The card
  // must then drop the bar, not render `width: NaN%`.
  it('drops the progress bar rather than rendering NaN for unusable dates', () => {
    wrap(<CurrentPeriodCard current={current({ mahadasha: period({ end: 'not-a-date' }) })} />)

    expect(screen.getByText(/Jupiter/)).toBeInTheDocument()
    expect(screen.queryByRole('progressbar')).toBeNull()
  })

  it('links to the full timeline', async () => {
    const user = userEvent.setup()
    wrap(<CurrentPeriodCard current={current()} />)

    const link = screen.getByRole('link', { name: /see all periods/i })
    expect(link).toHaveAttribute('href', '/kundli/dashas')
    await user.tab()
  })
})

/**
 * Four combinations, none of which may render a stray comma.
 *
 * The time of day is null on the server and the name is null for anyone
 * who skipped it during onboarding, so both absences are reachable in
 * production rather than theoretical.
 */
describe('greeting', () => {
  // The real English dictionary and the real interpolator, so these
  // assert what a reader sees rather than what a fixture says.
  const g = (time: Parameters<typeof greeting>[2], name?: string | null) =>
    greeting(en, interpolate, time, name)

  it('uses the time of day and the name when both are known', () => {
    expect(g('morning', 'Priya')).toBe('Good morning, Priya')
    expect(g('afternoon', 'Priya')).toBe('Good afternoon, Priya')
    expect(g('evening', 'Priya')).toBe('Good evening, Priya')
  })

  it('drops the time of day rather than guessing one', () => {
    // Null is what the SERVER renders. "Good morning" shown to somebody
    // at midnight is worse than "Hello".
    expect(g(null, 'Priya')).toBe('Hello, Priya')
  })

  it('renders no trailing comma when the name is missing', () => {
    expect(g('morning', null)).toBe('Good morning')
    expect(g('morning', undefined)).toBe('Good morning')
    expect(g('morning', '   ')).toBe('Good morning')
    expect(g(null, null)).toBe('Hello')
  })

  it('trims a name with stray whitespace', () => {
    expect(g('morning', '  Priya  ')).toBe('Good morning, Priya')
  })
})
