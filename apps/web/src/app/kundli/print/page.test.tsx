import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { LocaleProvider } from '@/lib/i18n/context'

import PrintPage from './page'

/**
 * The print page, which is the only screen in this product whose
 * audience is a machine.
 *
 * The assertions that matter here are not "does it look right" — they
 * are the two contracts the PDF worker depends on and cannot renegotiate
 * at runtime:
 *
 *   1. `data-print-ready` on <body>, eventually, in EVERY terminal
 *      state. chromedp blocks on that selector; a state that never sets
 *      it is a render that hangs for the full ninety seconds and then
 *      fails, while the user watches a spinner.
 *   2. The locale comes from the URL. The worker's browser has no
 *      localStorage, so a Hindi user's PDF would silently come out in
 *      English with nothing failing anywhere.
 */

const SEARCH = { value: '' }

vi.mock('next/navigation', () => ({
  useSearchParams: () => new URLSearchParams(SEARCH.value),
}))

const fetchPrintBundle = vi.fn()
const PrintTokenRejectedError = class extends Error {
  constructor() {
    super('rejected')
    this.name = 'PrintTokenRejectedError'
  }
}

vi.mock('@/lib/print-api', () => ({
  fetchPrintBundle: (...args: unknown[]) => fetchPrintBundle(...args),
  get PrintTokenRejectedError() {
    return PrintTokenRejectedError
  },
}))

/**
 * A minimal but complete bundle.
 *
 * The chart data is a real shape rather than `{}`, because
 * `parseChartData` returns null for anything it cannot read and a null
 * chart renders nothing — which would make every assertion below pass
 * against an empty document.
 */
function bundle(overrides: Record<string, unknown> = {}) {
  const chartData = {
    ascendant: { longitude: 215.5, sign: 'Scorpio', sign_index: 7, degree: 5.5 },
    planets: [
      {
        planet: 'Sun',
        longitude: 125,
        sign: 'Leo',
        sign_index: 4,
        degree: 5,
        house: 10,
        nakshatra: 'Magha',
        nakshatra_index: 9,
        pada: 2,
        is_retrograde: false,
        is_combust: false,
        dignity: 'own',
        speed: 0.98,
      },
    ],
    houses: null,
  }

  return {
    birth_profile: {
      id: 'profile-1',
      label: 'Priya',
      birth_date: '1994-08-17',
      birth_time: '14:35',
      time_accuracy: 'exact',
      birth_place: 'Jaipur',
      timezone: 'Asia/Kolkata',
    },
    charts: {
      D1: {
        id: 'chart-1',
        birth_profile_id: 'profile-1',
        chart_type: 'D1',
        ayanamsa: 'lahiri',
        house_system: 'whole_sign',
        engine_version: 'skyfield-1.55+de421+schema1',
        chart_data: chartData,
        computed_at: '2026-01-01T00:00:00Z',
      },
    },
    mahadashas: [
      { id: 'maha-1', planet: 'Ketu', start: '1994-01-01T00:00:00Z', end: '2001-01-01T00:00:00Z', level: 1 },
      { id: 'maha-2', planet: 'Venus', start: '2001-01-01T00:00:00Z', end: '2021-01-01T00:00:00Z', level: 1 },
    ],
    current_dasha: {
      mahadasha: { id: 'maha-2', planet: 'Venus', start: '2001-01-01T00:00:00Z', end: '2021-01-01T00:00:00Z', level: 1 },
      antardasha: null,
      pratyantardasha: null,
      at: '2026-09-18T00:00:00Z',
    },
    generated_at: '2026-09-18T00:00:00Z',
    ...overrides,
  }
}

function renderPage() {
  return render(
    <LocaleProvider>
      <PrintPage />
    </LocaleProvider>,
  )
}

/** The flag the worker blocks on. */
function printReady(): boolean {
  return document.body.getAttribute('data-print-ready') === 'true'
}

beforeEach(() => {
  SEARCH.value = 'token=abc'
  fetchPrintBundle.mockReset()
  fetchPrintBundle.mockResolvedValue(bundle())
  window.localStorage.clear()
})

afterEach(() => {
  document.body.removeAttribute('data-print-ready')
})

describe('the printed document', () => {
  it('renders the chart, the positions and the dasha sequence', async () => {
    renderPage()

    expect(await screen.findByText('Kundli')).toBeInTheDocument()
    expect(screen.getByText('Prepared for Priya')).toBeInTheDocument()

    // The chart, not merely a heading claiming there is one.
    expect(screen.getByRole('heading', { name: /rasi chart/i })).toBeInTheDocument()

    // The positions table carries a real position.
    expect(screen.getByText('Magha')).toBeInTheDocument()

    // Both mahadashas, and the current one labelled in WORDS. Weight
    // alone means nothing to a colour-blind reader and nothing at all
    // on a monochrome printer.
    expect(screen.getByText('Ketu')).toBeInTheDocument()
    expect(screen.getByText('Venus')).toBeInTheDocument()
    expect(screen.getByText('Running now')).toBeInTheDocument()
  })

  it('signals print-ready once the document is on the page', async () => {
    renderPage()
    await screen.findByText('Kundli')
    await waitFor(() => expect(printReady()).toBe(true))
  })

  /*
    Every terminal state must signal ready, including the failures.

    Otherwise chromedp blocks on a page that has already given up and the
    render burns its whole ninety-second budget before failing. A
    one-page PDF saying the link expired is a worse document and a much
    better experience than a spinner that resolves into a timeout.
  */
  it.each([
    ['a rejected token', () => fetchPrintBundle.mockRejectedValue(new PrintTokenRejectedError())],
    ['any other failure', () => fetchPrintBundle.mockRejectedValue(new Error('network'))],
    ['no token in the URL', () => (SEARCH.value = '')],
  ])('signals print-ready after %s', async (_name, arrange) => {
    arrange()
    renderPage()

    await waitFor(() => expect(printReady()).toBe(true))

    // And says something, rather than printing a blank sheet.
    expect(screen.getByText(/no longer valid|could not be prepared/i)).toBeInTheDocument()
  })

  it('does not request anything when the URL carries no token', async () => {
    SEARCH.value = ''
    renderPage()

    await waitFor(() => expect(printReady()).toBe(true))
    expect(fetchPrintBundle).not.toHaveBeenCalled()
  })

  it('sends only the token, never a profile id', async () => {
    SEARCH.value = 'token=abc&id=someone-elses-profile'
    renderPage()
    await screen.findByText('Kundli')

    expect(fetchPrintBundle).toHaveBeenCalledTimes(1)
    const [token] = fetchPrintBundle.mock.calls[0] as [string]
    expect(token).toBe('abc')
  })

  /*
    The locale comes from the URL.

    This is the half that has no fallback: the worker's browser starts
    with an empty profile, so `ayana.locale` is unset and the dictionary
    would answer in English for every Hindi user's PDF — with no error
    anywhere, on either side.
  */
  it('renders in the locale the URL asks for', async () => {
    SEARCH.value = 'token=abc&locale=hi'
    renderPage()

    expect(await screen.findByText('कुंडली')).toBeInTheDocument()
    expect(screen.queryByText('Kundli')).not.toBeInTheDocument()
  })

  it('defaults to English when the URL names no locale', async () => {
    SEARCH.value = 'token=abc'
    renderPage()

    expect(await screen.findByText('Kundli')).toBeInTheDocument()
  })

  // A profile with no birth time has no dasha tree at all, and the
  // document has to say so rather than printing an empty section.
  it('explains a missing timeline instead of leaving a gap', async () => {
    fetchPrintBundle.mockResolvedValue(bundle({ mahadashas: null, current_dasha: null }))
    renderPage()

    expect(await screen.findByText(/dasha periods need a birth time/i)).toBeInTheDocument()
  })

  // The document carries its provenance, so a printed chart three years
  // from now can still be explained.
  it('records which engine and ayanamsa produced it', async () => {
    renderPage()
    await screen.findByText('Kundli')

    expect(
      screen.getByText(/skyfield-1\.55\+de421\+schema1.*lahiri/i),
    ).toBeInTheDocument()
  })
})
