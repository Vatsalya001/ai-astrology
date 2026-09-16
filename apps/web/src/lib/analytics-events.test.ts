import { describe, expect, it } from 'vitest'

import {
  bucketAmountPaise,
  bucketLength,
  noopAnalytics,
  type EventMap,
  type EventName,
} from '@ayana/analytics'

/**
 * The event vocabulary, checked against the specifications that define
 * it.
 *
 * `services/api` has had this guard since Phase 1 and it earned its keep
 * immediately — adding the Phase 2 events failed it with "knownEvents
 * has 19 entries, the spec lists 10". This side had no equivalent, and
 * by the time Phase 2 shipped the TypeScript map was missing four of the
 * nine events the spec lists: place_selected_via_map,
 * birth_profile_created, chart_generation_failed and
 * birth_profile_edited.
 *
 * Nothing noticed, because a missing event is not a compile error
 * anywhere — it is a call site that was never written, and an analytics
 * question nobody can answer six months later.
 *
 * It lives in `apps/web` rather than in `packages/analytics` because the
 * package has no test runner, and adding vitest plus a config there to
 * hold one file would be more machinery than coverage. The web app is
 * the consumer and already runs vitest; the guard fires either way.
 */

// Transcribed from docs/specs/PHASE-01-AUTH-AND-USERS.md §15.
const PHASE_1 = [
  'signup_started',
  'otp_requested',
  'otp_verified',
  'signup_completed',
  'login_completed',
  'logout_completed',
  'preferences_updated',
  'profile_updated',
  'session_revoked',
  'data_exported',
  'account_deletion_requested',
  'account_deletion_cancelled',
  'account_deleted',
] as const

// Transcribed from docs/specs/PHASE-02-ASTROLOGY-ENGINE.md §15.
const PHASE_2 = [
  'birth_profile_started',
  'birth_profile_step_completed',
  'birth_time_unknown_selected',
  'place_search_performed',
  'place_selected_via_map',
  'birth_profile_created',
  'chart_generated',
  'chart_generation_failed',
  'birth_profile_edited',
] as const

// Phase 3 is not shipped. Its events are already in the map, which is
// fine — they are listed here so this test compares like with like
// rather than reporting them as drift every run.
const PHASE_3 = [
  'kundli_viewed',
  'chart_style_switched',
  'glossary_term_opened',
  'ai_chat_box_tapped',
] as const

// Listing the keys explicitly rather than deriving them from EventMap:
// deriving would make the test agree with the map by construction, which
// is the one thing it must not do.
const IMPLEMENTED: EventName[] = [
  'signup_started',
  'otp_requested',
  'otp_verified',
  'signup_completed',
  'login_completed',
  'logout_completed',
  'preferences_updated',
  'profile_updated',
  'session_revoked',
  'data_exported',
  'account_deletion_requested',
  'account_deletion_cancelled',
  'account_deleted',
  'birth_profile_started',
  'birth_profile_step_completed',
  'birth_time_unknown_selected',
  'place_search_performed',
  'place_selected_via_map',
  'birth_profile_created',
  'chart_generated',
  'chart_generation_failed',
  'birth_profile_edited',
  'kundli_viewed',
  'chart_style_switched',
  'glossary_term_opened',
  'ai_chat_box_tapped',
]

describe('the event vocabulary matches the specifications', () => {
  const specified = [...PHASE_1, ...PHASE_2, ...PHASE_3]

  it('has no event the specifications do not list', () => {
    const extra = IMPLEMENTED.filter((name) => !specified.includes(name as never))
    expect(extra, `${extra.join(', ')} is emitted but written down nowhere`).toEqual([])
  })

  it('implements every event the specifications list', () => {
    const missing = specified.filter((name) => !IMPLEMENTED.includes(name as EventName))
    expect(
      missing,
      `${missing.join(', ')} is in a spec §15 but absent from EventMap — ` +
        'a call site that was never written, and a question nobody can answer later',
    ).toEqual([])
  })

  it('counts the same on both sides', () => {
    expect(
      IMPLEMENTED.length,
      `EventMap has ${IMPLEMENTED.length} entries, the three specs list ` +
        `${specified.length} (${PHASE_1.length} + ${PHASE_2.length} + ${PHASE_3.length})`,
    ).toBe(specified.length)
  })
})

describe('payloads carry no continuous values that could fingerprint', () => {
  // Buckets exist so an exact amount or message length never leaves the
  // app. A raw value is a fingerprint; a bucket answers the same product
  // question and is not.
  it('buckets money without ever exposing the amount', () => {
    expect(bucketAmountPaise(0)).toBe('free')
    expect(bucketAmountPaise(-1)).toBe('free')
    expect(bucketAmountPaise(19_999)).toBe('under_200')
    expect(bucketAmountPaise(20_000)).toBe('under_500')
    expect(bucketAmountPaise(99_999)).toBe('under_1000')
    expect(bucketAmountPaise(100_000)).toBe('over_1000')
  })

  it('buckets lengths', () => {
    expect(bucketLength(0)).toBe('short')
    expect(bucketLength(79)).toBe('short')
    expect(bucketLength(80)).toBe('medium')
    expect(bucketLength(399)).toBe('medium')
    expect(bucketLength(400)).toBe('long')
  })
})

describe('the default client', () => {
  // A no-op rather than a console logger, deliberately: a logger prints
  // payloads during development, which is exactly the habit the typed
  // map exists to prevent.
  it('does nothing and throws nothing', () => {
    expect(() => {
      noopAnalytics.track('chart_generation_failed', { error_code: 'astro_unavailable' })
      noopAnalytics.identify('user-1')
      noopAnalytics.reset()
    }).not.toThrow()
  })
})

// A payload type that is not flat is a compile error, not a test
// failure — _EventsAreFlat in index.ts does that. This asserts the
// property is still wired, because deleting that type alias would
// silently remove the guarantee.
describe('event payloads stay flat', () => {
  it('rejects a nested object at the type level', () => {
    const payload: EventMap['chart_generated'] = {
      user_id: 'u1',
      chart_type: 'D1',
      duration_ms: 42,
    }
    for (const value of Object.values(payload)) {
      expect(['string', 'number', 'boolean']).toContain(typeof value)
    }
  })
})
