import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createRef } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { LocaleProvider } from '@/lib/i18n/context'

import { ShareSheet } from './ShareSheet'

/**
 * The share sheet.
 *
 * The assertions that matter are about what the link IS, not about how
 * the dialog looks: that the URL is built from this page's own origin,
 * that the token is the only thing in it, and that the two failure modes
 * a user can actually hit say different things.
 */

const createShare = vi.fn()
const track = vi.fn()

vi.mock('@/lib/astrology-api', () => ({
  astrologyApi: {
    createShare: (...args: unknown[]) => createShare(...args),
  },
}))

vi.mock('@/lib/analytics', () => ({
  track: (...args: unknown[]) => track(...args),
  trackAsUser: vi.fn(),
}))

const PROFILE = 'b1a7c0de-0000-4000-8000-000000000001'
// Self-describing rather than random-looking: the secret scanner reads
// a 22-character opaque string as a generic API key, and its advice is
// to make the fixture obviously fake rather than to allowlist the file.
const TOKEN = 'example-not-a-real-tok'

function renderSheet() {
  const chartRef = createRef<SVGSVGElement>()
  render(
    <LocaleProvider>
      <ShareSheet profileId={PROFILE} chartRef={chartRef} />
    </LocaleProvider>,
  )
  return chartRef
}

async function openSheet() {
  await userEvent.click(screen.getByRole('button', { name: /^share$/i }))
}

beforeEach(() => {
  createShare.mockReset()
  track.mockReset()
  createShare.mockResolvedValue({
    id: 'share-1',
    birth_profile_id: PROFILE,
    scope: 'chart',
    expires_at: '2026-10-18T00:00:00Z',
    view_count: 0,
    token: TOKEN,
  })
  window.localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('the share sheet', () => {
  it('offers an image and a link', async () => {
    renderSheet()
    await openSheet()

    expect(screen.getByRole('button', { name: /save as image/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /create a link/i })).toBeInTheDocument()
  })

  /*
    The warning says both halves, and both are load-bearing.

    "Anyone with this link can see the chart" is the risk. "It does not
    show your birth date, time or place" is what makes sending it
    reasonable. A reader deciding whether to press the button needs both,
    and a warning carrying only the first would stop people using a
    feature that is in fact safe to use.
  */
  it('tells the reader what the link does and does not expose', async () => {
    renderSheet()
    await openSheet()

    const warning = screen.getByText(/anyone with this link/i)
    expect(warning).toHaveTextContent(/can see the chart/i)
    expect(warning).toHaveTextContent(/does not show your birth date, time or place/i)
  })

  /*
    The link is built from THIS page's origin.

    Not from anything the API returned. A response that could name a host
    would be a response that could point a user's share — which they are
    about to send to their family — at somebody else's site.
  */
  it('builds the link from the page origin and the token alone', async () => {
    renderSheet()
    await openSheet()
    await userEvent.click(screen.getByRole('button', { name: /create a link/i }))

    const field = await screen.findByRole('textbox')
    const value = (field as HTMLInputElement).value

    expect(value).toBe(`${window.location.origin}/shared/${TOKEN}`)
    expect(value).not.toContain(PROFILE)
  })

  it('reports the too-many-links case differently from a failure', async () => {
    renderSheet()
    await openSheet()

    createShare.mockRejectedValueOnce(Object.assign(new Error('conflict'), { status: 409 }))
    await userEvent.click(screen.getByRole('button', { name: /create a link/i }))

    expect(await screen.findByText(/too many active links/i)).toBeInTheDocument()
    expect(screen.queryByText(/that did not work/i)).not.toBeInTheDocument()
  })

  it('reports an ordinary failure', async () => {
    renderSheet()
    await openSheet()

    createShare.mockRejectedValueOnce(new Error('network'))
    await userEvent.click(screen.getByRole('button', { name: /create a link/i }))

    expect(await screen.findByText(/that did not work/i)).toBeInTheDocument()
  })

  /*
    A response with no token is a failure, not a link.

    The API returns the plaintext exactly once. A client that rendered
    `/shared/undefined` because the field was missing would hand the user
    a broken link and tell them it worked.
  */
  it('treats a tokenless response as a failure rather than a link', async () => {
    renderSheet()
    await openSheet()

    createShare.mockResolvedValueOnce({
      id: 'share-1',
      birth_profile_id: PROFILE,
      scope: 'chart',
      expires_at: '2026-10-18T00:00:00Z',
      view_count: 0,
      // no token
    })
    await userEvent.click(screen.getByRole('button', { name: /create a link/i }))

    expect(await screen.findByText(/that did not work/i)).toBeInTheDocument()
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  })

  // The analytics event records the method and nothing else. A share id
  // is a handle to somebody's birth chart, and analytics is a third
  // party.
  it('records the method and no identifiers', async () => {
    renderSheet()
    await openSheet()
    await userEvent.click(screen.getByRole('button', { name: /create a link/i }))

    await waitFor(() => expect(track).toHaveBeenCalledWith('kundli_shared', { method: 'link' }))

    const payload = JSON.stringify(track.mock.calls)
    for (const identifier of [PROFILE, TOKEN, 'share-1']) {
      expect(payload).not.toContain(identifier)
    }
  })

  // The dialog forgets the link when it closes, so reopening does not
  // show a credential the user may have finished with.
  it('forgets the link when the sheet is closed', async () => {
    renderSheet()
    await openSheet()
    await userEvent.click(screen.getByRole('button', { name: /create a link/i }))
    expect(await screen.findByRole('textbox')).toBeInTheDocument()

    await userEvent.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('textbox')).not.toBeInTheDocument())

    await openSheet()
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /create a link/i })).toBeInTheDocument()
  })
})
