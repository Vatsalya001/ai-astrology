/**
 * Typed client for api-service.
 *
 * The web app talks ONLY to the Go API. It has no knowledge that
 * astro-service or ai-service exist — that is the property which keeps
 * the polyglot backend invisible to clients and lets the services move
 * without touching the frontend.
 */

import type { HealthResponse, MetaResponse } from '@ayana/types'

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:4000'

// Response shapes live in @ayana/types so the mobile app (Phase 10) uses
// the identical definitions rather than a drifting copy.
export type { Check, CheckStatus, HealthResponse, MetaResponse } from '@ayana/types'

/**
 * Distinguishes "the API said no" from "the API was unreachable".
 *
 * The underlying failure is passed through the standard ES2022
 * `cause` option rather than a own property, which keeps it visible to
 * anything that already understands error chaining.
 */
export class ApiUnreachableError extends Error {
  constructor(cause: unknown) {
    super('Could not reach api-service', { cause })
    this.name = 'ApiUnreachableError'
  }
}

async function get<T>(path: string, timeoutMs = 3000): Promise<T> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)

  try {
    const res = await fetch(`${API_URL}${path}`, {
      signal: controller.signal,
      // This data changes constantly; caching it would make the status
      // page confidently wrong.
      cache: 'no-store',
      headers: { Accept: 'application/json' },
    })

    // A 503 from /health is a *valid* response carrying real
    // information, so it must not be treated as a transport failure.
    if (!res.ok && res.status !== 503) {
      throw new Error(`${path} returned ${res.status}`)
    }

    return (await res.json()) as T
  } catch (err) {
    if (err instanceof Error && err.name === 'AbortError') {
      throw new ApiUnreachableError(new Error(`timed out after ${timeoutMs}ms`))
    }
    throw new ApiUnreachableError(err)
  } finally {
    clearTimeout(timer)
  }
}

export const api = {
  health: () => get<HealthResponse>('/health'),
  meta: () => get<MetaResponse>('/api/v1/meta'),
  url: API_URL,
}
