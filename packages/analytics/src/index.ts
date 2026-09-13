/**
 * Typed analytics events.
 *
 * Nothing fires yet — Phase 0 has no product surface. The shape exists
 * now so Phase 1 can emit without first designing an event schema under
 * time pressure, and so the one rule that matters is enforced by the
 * type system from the first event onward:
 *
 *   ► PAYLOADS CARRY IDs, ENUMS AND BUCKETS. NEVER PII.
 *
 * Birth date + birth time + birth place is, in combination, close to a
 * unique identifier. An analytics pipeline is a third party, is retained
 * for years, and is queried by people who never read this file. Once a
 * name or an email is in it, it is effectively permanent.
 *
 * `AnalyticsValue` makes that mechanical rather than aspirational: a
 * payload cannot hold an arbitrary object, so the lazy
 * `track('x', user)` is a type error rather than a leak.
 */

/** The only value types an event payload may contain. */
export type AnalyticsValue = string | number | boolean | null

export type EventPayload = Record<string, AnalyticsValue>

// ─── Buckets ─────────────────────────────────────────────────────────
// Continuous values are bucketed before they leave the application.
// A raw amount or an exact message length is a fingerprint; a bucket is
// not, and answers the same product questions.

export type AmountBucket = 'free' | 'under_200' | 'under_500' | 'under_1000' | 'over_1000'
export type LengthBucket = 'short' | 'medium' | 'long'
export type ConfidenceBucket = 'low' | 'medium' | 'high'

export function bucketAmountPaise(paise: number): AmountBucket {
  if (paise <= 0) return 'free'
  if (paise < 20_000) return 'under_200'
  if (paise < 50_000) return 'under_500'
  if (paise < 100_000) return 'under_1000'
  return 'over_1000'
}

export function bucketLength(chars: number): LengthBucket {
  if (chars < 80) return 'short'
  if (chars < 400) return 'medium'
  return 'long'
}

// ─── Event map ───────────────────────────────────────────────────────
// One entry per event, added as each phase ships it. Keeping them in a
// single map means the full surface is reviewable in one place — which
// is where a PII leak would be spotted.

export interface EventMap {
  // Phase 1 — auth
  signup_started: { channel: 'phone' | 'email' | 'google' | 'apple' }
  otp_requested: { channel: 'phone' | 'email' }
  otp_verified: { channel: 'phone' | 'email'; attempts: number }
  signup_completed: { channel: string; user_id: string }
  login_completed: { channel: string; user_id: string }
  account_deleted: { user_id: string }

  // Phase 2 — birth profiles
  birth_profile_started: { user_id: string }
  birth_profile_step_completed: { step: 1 | 2 | 3 }
  birth_time_unknown_selected: { user_id: string }
  // Note: result_count, never the query — a place search is location PII.
  place_search_performed: { result_count: number }
  chart_generated: { user_id: string; chart_type: string; duration_ms: number }

  // Phase 3 — Kundli UI
  kundli_viewed: { user_id: string; chart_type: string }
  chart_style_switched: { from: string; to: string }
  glossary_term_opened: { term_key: string }
  // Measures latent demand for Phase 5 before it is built.
  ai_chat_box_tapped: { enabled: boolean }
}

export type EventName = keyof EventMap

/**
 * Compile-time proof that no event payload can smuggle a nested object.
 *
 * If someone adds `user: User` to an event, this fails to compile rather
 * than silently shipping a person's details to a third party.
 */
type AssertFlat<T> = {
  [K in keyof T]: T[K] extends EventPayload ? true : never
}
export type _EventsAreFlat = AssertFlat<EventMap>

// ─── Emitter ─────────────────────────────────────────────────────────

export interface AnalyticsClient {
  track<E extends EventName>(event: E, payload: EventMap[E]): void
  identify(userId: string): void
  reset(): void
}

/**
 * Does nothing. The default until Phase 3 wires a real provider.
 *
 * A no-op rather than a console logger on purpose: a logger would print
 * payloads during development, which is exactly the habit this module
 * exists to prevent.
 */
export const noopAnalytics: AnalyticsClient = {
  track: () => {},
  identify: () => {},
  reset: () => {},
}
