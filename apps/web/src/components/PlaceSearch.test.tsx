import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { PlaceSearch } from './PlaceSearch'
import { astrologyApi, type Place } from '@/lib/astrology-api'

/**
 * The place search is the last step before a chart, and the highest
 * drop-off point in the funnel. The behaviours tested here are the ones
 * that make it feel broken while every request succeeds: a dropdown that
 * flickers back to stale results, and a search that fires per keystroke.
 */

const JAIPUR_RAJASTHAN: Place = {
  id: 1269515,
  name: 'Jaipur',
  admin1: 'Rajasthan',
  country_code: 'IN',
  latitude: 26.9124,
  longitude: 75.7873,
  timezone: 'Asia/Kolkata',
  population: 2711758,
}

const JAIPUR_ODISHA: Place = {
  ...JAIPUR_RAJASTHAN,
  id: 1269516,
  admin1: 'Odisha',
  population: 612,
}

function mockSearch(byQuery: Record<string, Place[]>) {
  return vi
    .spyOn(astrologyApi, 'searchPlaces')
    .mockImplementation(async (query: string) =>
      Promise.resolve({ places: byQuery[query] ?? [], query }),
    )
}

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true })
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('PlaceSearch', () => {
  it('does not search until the query is long enough', async () => {
    const search = mockSearch({})
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })

    render(<PlaceSearch selected={null} onSelect={vi.fn()} />)
    await user.type(screen.getByRole('combobox'), 'j')
    await vi.advanceTimersByTimeAsync(1000)

    expect(search).not.toHaveBeenCalled()
    expect(screen.getByText(/keep typing/i)).toBeInTheDocument()
  })

  // Without the debounce, a five-character town is five requests and
  // five chances for the responses to arrive out of order.
  it('makes one request for a burst of typing, not one per keystroke', async () => {
    const search = mockSearch({ jaipur: [JAIPUR_RAJASTHAN] })
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })

    render(<PlaceSearch selected={null} onSelect={vi.fn()} />)
    await user.type(screen.getByRole('combobox'), 'jaipur')
    await vi.advanceTimersByTimeAsync(400)

    expect(search).toHaveBeenCalledTimes(1)
    expect(search).toHaveBeenCalledWith('jaipur', expect.anything())
  })

  it('shows results ranked as the server returned them', async () => {
    mockSearch({ jaip: [JAIPUR_RAJASTHAN, JAIPUR_ODISHA] })
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })

    render(<PlaceSearch selected={null} onSelect={vi.fn()} />)
    await user.type(screen.getByRole('combobox'), 'jaip')
    await vi.advanceTimersByTimeAsync(400)

    const options = await screen.findAllByRole('option')
    expect(options).toHaveLength(2)
    // The larger Jaipur first. The client must not re-sort: population
    // ranking is the server's decision and the reason "jaip" is useful.
    expect(options[0]).toHaveTextContent('Rajasthan')
    expect(options[1]).toHaveTextContent('Odisha')
  })

  // The bug this guards: a slow response for "jai" landing after a fast
  // one for "jaipur" replaces the right list with the wrong one. Every
  // request succeeded; the dropdown is still wrong.
  it('ignores a response for a query the user has typed past', async () => {
    vi.spyOn(astrologyApi, 'searchPlaces').mockImplementation(async (query: string) => {
      if (query === 'jai') {
        // Arrives late, after the user has typed more.
        await new Promise((resolve) => setTimeout(resolve, 500))
        return { places: [JAIPUR_ODISHA], query }
      }
      return { places: [JAIPUR_RAJASTHAN], query }
    })

    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    render(<PlaceSearch selected={null} onSelect={vi.fn()} />)

    const input = screen.getByRole('combobox')
    await user.type(input, 'jai')
    await vi.advanceTimersByTimeAsync(300)
    await user.type(input, 'pur')
    await vi.advanceTimersByTimeAsync(1000)

    const options = await screen.findAllByRole('option')
    expect(options).toHaveLength(1)
    expect(options[0]).toHaveTextContent('Rajasthan')
  })

  // Shortening the query must not leave the old list on screen. This is
  // derived from the query rather than cleared by an effect, which is
  // why it holds without a cascading render.
  it('hides results once the query drops below the threshold', async () => {
    mockSearch({ jaip: [JAIPUR_RAJASTHAN] })
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })

    render(<PlaceSearch selected={null} onSelect={vi.fn()} />)
    const input = screen.getByRole('combobox')

    await user.type(input, 'jaip')
    await vi.advanceTimersByTimeAsync(400)
    expect(await screen.findAllByRole('option')).toHaveLength(1)

    await user.clear(input)
    await user.type(input, 'j')
    await vi.advanceTimersByTimeAsync(400)

    expect(screen.queryAllByRole('option')).toHaveLength(0)
  })

  it('reports the whole place when one is chosen', async () => {
    mockSearch({ jaip: [JAIPUR_RAJASTHAN, JAIPUR_ODISHA] })
    const onSelect = vi.fn()
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })

    render(<PlaceSearch selected={null} onSelect={onSelect} />)
    await user.type(screen.getByRole('combobox'), 'jaip')
    await vi.advanceTimersByTimeAsync(400)

    const options = await screen.findAllByRole('option')
    await user.click(options[1]!)

    // The whole Place, so the caller sends its ID. Coordinates never
    // originate in the browser.
    expect(onSelect).toHaveBeenCalledWith(JAIPUR_ODISHA)
  })

  it('is navigable with the arrow keys and Enter', async () => {
    mockSearch({ jaip: [JAIPUR_RAJASTHAN, JAIPUR_ODISHA] })
    const onSelect = vi.fn()
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })

    render(<PlaceSearch selected={null} onSelect={onSelect} />)
    const input = screen.getByRole('combobox')

    await user.type(input, 'jaip')
    await vi.advanceTimersByTimeAsync(400)
    await screen.findAllByRole('option')

    await user.keyboard('{ArrowDown}{ArrowDown}{Enter}')
    expect(onSelect).toHaveBeenCalledWith(JAIPUR_ODISHA)
  })

  // The input keeps focus and points at the active option, rather than
  // focus moving into the list. Without this a screen reader announces
  // nothing as the arrow keys move.
  it('points aria-activedescendant at the highlighted option', async () => {
    mockSearch({ jaip: [JAIPUR_RAJASTHAN, JAIPUR_ODISHA] })
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })

    render(<PlaceSearch selected={null} onSelect={vi.fn()} />)
    const input = screen.getByRole('combobox')

    await user.type(input, 'jaip')
    await vi.advanceTimersByTimeAsync(400)
    await screen.findAllByRole('option')

    expect(input).not.toHaveAttribute('aria-activedescendant')
    await user.keyboard('{ArrowDown}')
    expect(input).toHaveAttribute('aria-activedescendant', 'place-option-0')
    expect(document.activeElement).toBe(input)
  })

  it('says so when nothing matched, rather than showing an empty box', async () => {
    mockSearch({ zzzz: [] })
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })

    render(<PlaceSearch selected={null} onSelect={vi.fn()} />)
    await user.type(screen.getByRole('combobox'), 'zzzz')
    await vi.advanceTimersByTimeAsync(400)

    expect(await screen.findByText(/no places found/i)).toBeInTheDocument()
  })

  it('reports a failed search without clearing what the user typed', async () => {
    vi.spyOn(astrologyApi, 'searchPlaces').mockRejectedValue(new Error('offline'))
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })

    render(<PlaceSearch selected={null} onSelect={vi.fn()} />)
    const input = screen.getByRole('combobox')
    await user.type(input, 'jaip')
    await vi.advanceTimersByTimeAsync(400)

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/couldn’t search places/i)
    })
    expect(input).toHaveValue('jaip')
  })
})
