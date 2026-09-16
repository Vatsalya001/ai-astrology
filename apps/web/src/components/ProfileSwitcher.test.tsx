import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import { ProfileSwitcher } from './ProfileSwitcher'
import type { BirthProfile } from '@/lib/astrology-api'
import { ProfileProvider, resolveProfile, useSelectedProfile } from '@/lib/profile-context'

/**
 * The complete shape, not a partial cast.
 *
 * Written as a partial first, which made the "no birth details in the
 * options" test below almost meaningless: `birth_place` is a field on
 * BirthProfile and the fixture did not have it, so nothing could have
 * leaked it. The whole point is that every PII field IS present and
 * still does not reach the DOM.
 */
function profile(id: string, label = ''): BirthProfile {
  return {
    id,
    label,
    birth_date: '1994-08-17',
    birth_time: '14:35',
    time_accuracy: 'exact',
    birth_place: 'Jaipur, Rajasthan, India',
    latitude: 26.9124,
    longitude: 75.7873,
    timezone: 'Asia/Kolkata',
    utc_offset_min: 330,
    utc_instant: '1994-08-17T09:05:00Z',
    version: 1,
    is_active: true,
    superseded_by: null,
    created_at: '2026-09-16T00:00:00Z',
  }
}

function renderSwitcher(profiles: BirthProfile[]) {
  return render(
    <ProfileProvider>
      <ProfileSwitcher profiles={profiles} />
    </ProfileProvider>,
  )
}

beforeEach(() => {
  window.localStorage.clear()
})

describe('ProfileSwitcher', () => {
  it('lists every profile by its label', () => {
    renderSwitcher([profile('a', 'Me'), profile('b', 'Partner'), profile('c', 'Child')])

    const select = screen.getByRole('combobox', { name: /chart/i })
    expect(select).toBeInTheDocument()
    for (const label of ['Me', 'Partner', 'Child']) {
      expect(screen.getByRole('option', { name: label })).toBeInTheDocument()
    }
  })

  it('selects a different profile', async () => {
    const user = userEvent.setup()
    renderSwitcher([profile('a', 'Me'), profile('b', 'Partner')])

    await user.selectOptions(screen.getByRole('combobox'), 'b')
    expect(window.localStorage.getItem('ayana.birthProfileId')).toBe('b')
  })

  /**
   * A switcher offering one choice is furniture.
   *
   * Asserted rather than left to taste: the first version rendered it
   * always, and a single-option select in the header of every Kundli
   * screen is a control that looks broken to anyone who taps it.
   */
  it('renders nothing when there is only one profile', () => {
    renderSwitcher([profile('a', 'Me')])
    expect(screen.queryByRole('combobox')).toBeNull()
  })

  it('renders nothing when there are none', () => {
    renderSwitcher([])
    expect(screen.queryByRole('combobox')).toBeNull()
  })

  it('falls back to the first profile when the stored id matches nothing', () => {
    window.localStorage.setItem('ayana.birthProfileId', 'deleted-profile')
    renderSwitcher([profile('a', 'Me'), profile('b', 'Partner')])

    // Not blank, and not the stale id: the control shows what the screen
    // is actually rendering.
    expect(screen.getByRole('combobox')).toHaveValue('a')
  })

  it('names an unlabelled profile rather than showing an empty option', () => {
    renderSwitcher([profile('a'), profile('b', '')])

    expect(screen.getByRole('option', { name: 'Chart 1' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Chart 2' })).toBeInTheDocument()
  })

  /**
   * Birth details never reach the option text.
   *
   * Date plus time plus place is close to a unique identifier, and this
   * control renders on every Kundli screen and into the PDF — exactly
   * the kind of place that quietly becomes a screenshot in a support
   * ticket. A label is a name the user chose; the rest is PII.
   */
  it('puts no birth details in the options', () => {
    const { container } = renderSwitcher([profile('a', 'Me'), profile('b', 'Partner')])
    const text = container.textContent ?? ''

    expect(text, 'a birth date reached the DOM').not.toMatch(/1994|08-17/)
    expect(text, 'a birth time reached the DOM').not.toMatch(/14:35/)
    expect(text, 'a birth place reached the DOM').not.toMatch(/Jaipur|Rajasthan/)
    expect(text, 'coordinates reached the DOM').not.toMatch(/26\.91|75\.78/)
    expect(text).not.toMatch(/\bexact\b/i)
  })
})

describe('resolveProfile', () => {
  it('returns the selected profile when it exists', () => {
    const list = [profile('a'), profile('b')]
    expect(resolveProfile(list, 'b')?.id).toBe('b')
  })

  /**
   * The stored id outlives the profile.
   *
   * Deleting one, or editing one — which creates a NEW version with a
   * new id rather than mutating the old, per Phase 2 — leaves
   * localStorage pointing at a record that is gone. Requesting it gives
   * a 404, and the ownership middleware returns 404 for another user's
   * profile too, so the screen cannot tell "deleted" from "not yours"
   * and would show an error state forever with the fix invisible.
   */
  it('falls back to the first when the selection no longer exists', () => {
    const list = [profile('a'), profile('b')]
    expect(resolveProfile(list, 'deleted')?.id).toBe('a')
  })

  it('falls back to the first when nothing is selected', () => {
    expect(resolveProfile([profile('a')], null)?.id).toBe('a')
  })

  it('returns null when there are no profiles at all', () => {
    expect(resolveProfile([], 'a')).toBeNull()
    expect(resolveProfile([], null)).toBeNull()
  })
})

/**
 * The selection has to reach OTHER components, not just the control.
 *
 * "Switching re-renders everything correctly" is task 3.14's acceptance,
 * and a switcher that updates only itself satisfies every test above.
 */
describe('the selection reaches consumers', () => {
  function Consumer() {
    const { selectedId } = useSelectedProfile()
    return <p data-testid="consumer">{selectedId ?? 'none'}</p>
  }

  it('re-renders a sibling when the selection changes', async () => {
    const user = userEvent.setup()
    render(
      <ProfileProvider>
        <ProfileSwitcher profiles={[profile('a', 'Me'), profile('b', 'Partner')]} />
        <Consumer />
      </ProfileProvider>,
    )

    expect(screen.getByTestId('consumer')).toHaveTextContent('none')

    await user.selectOptions(screen.getByRole('combobox'), 'b')
    expect(screen.getByTestId('consumer')).toHaveTextContent('b')
  })

  it('throws a useful error outside the provider', () => {
    expect(() => render(<Consumer />)).toThrow(/ProfileProvider/)
  })
})
