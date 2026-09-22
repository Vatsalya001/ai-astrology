import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { LocaleProvider } from '@/lib/i18n/context'

import SharesPage from './page'

/**
 * The share-management screen.
 *
 * What is worth asserting here is not the layout. It is the handful of
 * properties that make this screen safe to trust when you are trying to
 * work out who can still see your chart:
 *
 *   • a partial load is an ERROR, never a short list
 *   • "revoke failed" says the link is still live
 *   • expired and revoked links cannot be revoked again, and say which
 *     they are in words rather than in colour
 *
 * The first is the one that motivated the test. The comfortable
 * implementation — `Promise.allSettled`, render what loaded — produces a
 * screen that answers "is anything shared?" with a reassuring NO when
 * one request failed, and nothing about it looks broken.
 */

const listProfiles = vi.fn()
const listShares = vi.fn()
const revokeShare = vi.fn()

vi.mock('@/lib/astrology-api', () => ({
  astrologyApi: {
    listProfiles: (...a: unknown[]) => listProfiles(...a),
    listShares: (...a: unknown[]) => listShares(...a),
    revokeShare: (...a: unknown[]) => revokeShare(...a),
  },
}))

// Returns false for "not an auth error", matching the real hook's
// contract: it redirects and returns true only on a genuine 401.
vi.mock('@/lib/use-require-auth', () => ({
  useRequireAuth: () => () => false,
}))

const PROFILE_A = 'b1a7c0de-0000-4000-8000-00000000000a'
const PROFILE_B = 'b1a7c0de-0000-4000-8000-00000000000b'

function share(over: Partial<Record<string, unknown>> = {}) {
  return {
    id: 'share-1',
    birth_profile_id: PROFILE_A,
    scope: 'chart',
    // Far future, so "live" does not depend on when the suite runs.
    expires_at: '2099-01-01T00:00:00Z',
    revoked_at: null,
    view_count: 0,
    created_at: '2026-01-01T00:00:00Z',
    ...over,
  }
}

function renderPage() {
  return render(
    // No locale prop: the provider reads localStorage and defaults to
    // English, which is what these assertions are written against.
    <LocaleProvider>
      <SharesPage />
    </LocaleProvider>,
  )
}

beforeEach(() => {
  listProfiles.mockReset()
  listShares.mockReset()
  revokeShare.mockReset()
})

/*
  No `vi.restoreAllMocks()` here, deliberately.

  These are bare `vi.fn()`s, not spies on a real object, and restoring
  them fought the `mockReset()` above: any test that set
  `mockResolvedValue` immediately after a test that set
  `mockImplementation` got a mock returning `undefined`, so the page's
  own `.then()` threw and four unrelated tests failed with
  "Cannot read properties of undefined". The reset in `beforeEach` is
  the whole cleanup these need.
*/

describe('the four states', () => {
  it('shows a loading state before anything arrives', () => {
    listProfiles.mockReturnValue(new Promise(() => {}))
    const { container } = renderPage()

    // `aria-busy` + `aria-live` on a plain div, matching the sessions
    // page. Asserted on the attribute and the announced text rather than
    // on role="status", which this markup does not have — the skeletons
    // themselves are aria-hidden, so the sr-only line is the only thing
    // a screen reader gets.
    expect(container.querySelector('[aria-busy="true"]')).toBeInTheDocument()
    expect(screen.getByText(/loading/i)).toBeInTheDocument()
  })

  it('shows an empty state when nothing has been shared', async () => {
    listProfiles.mockResolvedValue({ birth_profiles: [{ id: PROFILE_A, label: 'Amma' }] })
    listShares.mockResolvedValue({ shares: [] })

    renderPage()

    expect(await screen.findByText(/have not shared any/i)).toBeInTheDocument()
  })

  it('lists a live link with the profile it belongs to', async () => {
    listProfiles.mockResolvedValue({ birth_profiles: [{ id: PROFILE_A, label: 'Amma' }] })
    listShares.mockResolvedValue({ shares: [share({ view_count: 4 })] })

    renderPage()

    expect(await screen.findByText('Amma')).toBeInTheDocument()
    expect(screen.getByText('Active')).toBeInTheDocument()
    // The view count is how an owner notices a link is being opened more
    // than the one person they sent it to.
    expect(screen.getByText(/Opened: 4/)).toBeInTheDocument()
  })

  it('offers a retry when the load fails', async () => {
    listProfiles.mockRejectedValue(new Error('network'))
    renderPage()
    expect(await screen.findByRole('button', { name: /try again|retry/i })).toBeInTheDocument()
  })
})

describe('a partial load is an error, not a short list', () => {
  it('does not render the profiles that loaded when one fails', async () => {
    listProfiles.mockResolvedValue({
      birth_profiles: [
        { id: PROFILE_A, label: 'Amma' },
        { id: PROFILE_B, label: 'Appa' },
      ],
    })
    listShares.mockImplementation((id: string) =>
      id === PROFILE_A
        ? Promise.resolve({ shares: [share()] })
        : Promise.reject(new Error('500')),
    )

    renderPage()

    expect(await screen.findByRole('button', { name: /try again|retry/i })).toBeInTheDocument()

    /*
      The assertion the implementation exists for.

      With `Promise.allSettled` this screen would render Amma's link,
      look completely normal, and silently omit every link on Appa's
      profile — so a user checking "who can see my charts" gets a
      confident answer that is missing rows. The empty text must not
      appear either: "nothing is shared" is the most dangerous possible
      wrong answer here.
    */
    expect(screen.queryByText('Amma')).not.toBeInTheDocument()
    expect(screen.queryByText(/have not shared any/i)).not.toBeInTheDocument()
  })
})

describe('revoking', () => {
  it('revokes against the link’s own profile, then reloads', async () => {
    /*
      A tiny stand-in for the server rather than a chain of
      `mockResolvedValueOnce`. The chained version made the test depend
      on exactly how many times the page refetches, which is an
      implementation detail — and when that count was off by one the
      mock returned `undefined` and the page fell into its error state,
      which looked like a bug in the page rather than in the test.
    */
    let stored = [share()]
    listProfiles.mockResolvedValue({ birth_profiles: [{ id: PROFILE_A, label: 'Amma' }] })
    listShares.mockImplementation(() => Promise.resolve({ shares: stored }))
    revokeShare.mockImplementation(() => {
      stored = [share({ revoked_at: '2026-02-01T00:00:00Z' })]
      return Promise.resolve(stored[0])
    })

    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: /^Revoke/ }))

    // Both ids, in order. A screen that passed the CURRENTLY SELECTED
    // profile instead of the link's own would work perfectly until a
    // user had two profiles, then revoke against the wrong one and 404.
    await waitFor(() => expect(revokeShare).toHaveBeenCalledWith(PROFILE_A, 'share-1'))

    // Re-fetched rather than patched in memory: the server decides what
    // revoked means.
    await waitFor(() => expect(screen.getByText('Revoked')).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: /^Revoke/ })).not.toBeInTheDocument()
  })

  it('says the link is STILL ACTIVE when revoking fails', async () => {
    listProfiles.mockResolvedValue({ birth_profiles: [{ id: PROFILE_A, label: 'Amma' }] })
    listShares.mockResolvedValue({ shares: [share()] })
    revokeShare.mockRejectedValue(new Error('500'))

    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: /^Revoke/ }))

    // "Something went wrong" after pressing revoke reads as "probably
    // fine". The consequence has to be in the message, and it has to be
    // an alert so a screen reader hears it without hunting.
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/still active/i)
  })

  it('offers no revoke button for a link that is already dead', async () => {
    listProfiles.mockResolvedValue({ birth_profiles: [{ id: PROFILE_A, label: 'Amma' }] })
    listShares.mockResolvedValue({
      shares: [
        share({ id: 'dead-1', revoked_at: '2026-02-01T00:00:00Z' }),
        share({ id: 'dead-2', expires_at: '2020-01-01T00:00:00Z' }),
      ],
    })

    renderPage()

    expect(await screen.findByText('Revoked')).toBeInTheDocument()
    expect(screen.getByText('Expired')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Revoke/ })).not.toBeInTheDocument()
  })
})

describe('accessibility and honesty', () => {
  it('names which link each revoke button belongs to', async () => {
    listProfiles.mockResolvedValue({
      birth_profiles: [
        { id: PROFILE_A, label: 'Amma' },
        { id: PROFILE_B, label: 'Appa' },
      ],
    })
    listShares.mockImplementation((id: string) =>
      Promise.resolve({
        shares: [
          share({
            id: `s-${id}`,
            birth_profile_id: id,
            created_at: id === PROFILE_A ? '2026-01-01T00:00:00Z' : '2026-03-03T00:00:00Z',
          }),
        ],
      }),
    )

    renderPage()

    // Without the aria-label a screen reader hears a column of identical
    // "Revoke" buttons and has no way to tell which chart each one is
    // about — on a screen whose whole purpose is choosing between them.
    expect(await screen.findByRole('button', { name: /Revoke — Amma, 2026-01-01/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Revoke — Appa, 2026-03-03/ })).toBeInTheDocument()
  })

  it('states that a link cannot be shown again', async () => {
    listProfiles.mockResolvedValue({ birth_profiles: [{ id: PROFILE_A, label: 'Amma' }] })
    listShares.mockResolvedValue({ shares: [share()] })

    renderPage()

    // The token is hashed server-side and the listing omits it, so there
    // can be no copy button. Saying so is the difference between a
    // missing feature and a user hunting for one that cannot exist.
    expect(await screen.findByText(/shown only once/i)).toBeInTheDocument()
  })

  it('marks status with a word, not only a colour', async () => {
    listProfiles.mockResolvedValue({ birth_profiles: [{ id: PROFILE_A, label: 'Amma' }] })
    listShares.mockResolvedValue({ shares: [share()] })

    renderPage()

    // `.claude/rules/frontend.md`: colour never carries meaning alone.
    // "Is this link still live" is precisely the question that must not
    // be answerable only by hue.
    const row = (await screen.findByText('Amma')).closest('li')
    expect(row).not.toBeNull()
    expect(within(row!).getByText('Active')).toBeInTheDocument()
  })
})
