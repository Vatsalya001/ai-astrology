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
  forgetUserId()
  client.reset()
}

/**
 * Emits an event that needs the signed-in user's id.
 *
 * The id is fetched once and remembered. Without this, the natural
 * spelling is `usersApi.me().then((me) => track(...))` at each call
 * site, which costs a request per event — three of them in the birth
 * flow alone — and tempts the next person into passing whatever id
 * happens to be in scope instead.
 *
 * That temptation is not hypothetical: the first version of the birth
 * flow passed the BIRTH PROFILE id as `user_id` on one event and an
 * empty string on another. Both type-check. Neither is a user.
 *
 * An event is dropped rather than sent with a wrong or blank id. An
 * analytics row attributed to nobody is noise; one attributed to the
 * wrong id is worse, because it looks like a fact.
 */
export async function trackAsUser<E extends EventName>(
  event: E,
  payload: Omit<EventMap[E], 'user_id'>,
): Promise<void> {
  const userId = await currentUserId()
  if (!userId) return
  client.track(event, { ...payload, user_id: userId } as EventMap[E])
}

let cachedUserId: string | null = null

async function currentUserId(): Promise<string | null> {
  if (cachedUserId) return cachedUserId
  try {
    const { usersApi } = await import('./users-api')
    const me = await usersApi.me()
    cachedUserId = me.id
    return cachedUserId
  } catch {
    // Not signed in, or the call failed. Either way there is no id, and
    // an event without one is better dropped than guessed.
    return null
  }
}

/** Clears the memoised id. Called from resetAnalytics on sign-out. */
function forgetUserId(): void {
  cachedUserId = null
}
