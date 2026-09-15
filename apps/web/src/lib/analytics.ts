'use client'

import { noopAnalytics, type AnalyticsClient, type EventMap, type EventName } from '@ayana/analytics'

/**
 * The analytics client the app uses.
 *
 * Still the no-op from `@ayana/analytics` — no provider is wired until
 * Phase 3. Routing every call through here now is the point: when a
 * provider is added it is one line, and until then every call site is
 * already written against the typed event map, which is what stops a
 * payload carrying PII.
 *
 * A no-op rather than a console logger, deliberately. A logger would
 * print payloads during development, which is precisely the habit the
 * typed map exists to prevent.
 */
let client: AnalyticsClient = noopAnalytics

export function setAnalyticsClient(next: AnalyticsClient): void {
  client = next
}

/**
 * Records an event.
 *
 * Typed against EventMap, so a payload carrying a nested object — the
 * way a whole user record gets sent by accident — is a compile error
 * rather than a permanent entry in a third party's database.
 */
export function track<E extends EventName>(event: E, payload: EventMap[E]): void {
  client.track(event, payload)
}

export function identify(userId: string): void {
  client.identify(userId)
}

/** Clears the identity on sign-out, so the next session is not attributed. */
export function resetAnalytics(): void {
  client.reset()
}
