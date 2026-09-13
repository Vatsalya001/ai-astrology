/**
 * Typed client for api-service, shared by web and (from Phase 10) mobile.
 *
 * The clients talk ONLY to the Go API. They have no knowledge that
 * astro-service or ai-service exist — which is the property that keeps
 * the polyglot backend invisible and lets those services move without
 * touching a client.
 *
 * Hand-written on purpose. The Go API's public surface is small and
 * stable, and a generator for it would be more machinery than the
 * problem deserves. The CROSS-SERVICE contracts are different: those are
 * generated from OpenAPI into Go clients and must never be hand-written
 * (ADR-005).
 */

import { API_URL, TIMEOUTS } from '@ayana/config'
import type { HealthResponse, MetaResponse } from '@ayana/types'

/** Distinguishes "the API said no" from "the API was unreachable". */
export class ApiUnreachableError extends Error {
  constructor(cause: unknown) {
    super('Could not reach api-service', { cause })
    this.name = 'ApiUnreachableError'
  }
}

/** The API returned a structured error envelope. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly traceId?: string,
    readonly retryable = false,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

interface ErrorEnvelope {
  error: { code: string; message: string; trace_id?: string; retryable?: boolean }
}

export interface RequestOptions {
  timeoutMs?: number
  signal?: AbortSignal
  /** Propagates an existing trace across the client→API boundary. */
  traceId?: string
}

async function request<T>(
  path: string,
  { timeoutMs = TIMEOUTS.default, signal, traceId }: RequestOptions = {},
): Promise<T> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)

  // Honour a caller's own cancellation as well as the timeout.
  signal?.addEventListener('abort', () => controller.abort(), { once: true })

  const headers: Record<string, string> = { Accept: 'application/json' }
  if (traceId) headers['X-Trace-Id'] = traceId

  try {
    const res = await fetch(`${API_URL}${path}`, {
      signal: controller.signal,
      cache: 'no-store',
      headers,
    })

    // 503 from /health is a VALID response carrying real information, so
    // it must not be treated as a transport failure.
    if (!res.ok && res.status !== 503) {
      let envelope: ErrorEnvelope | undefined
      try {
        envelope = (await res.json()) as ErrorEnvelope
      } catch {
        // Non-JSON error body; fall through to a generic message.
      }
      throw new ApiError(
        res.status,
        envelope?.error.code ?? 'UNKNOWN',
        envelope?.error.message ?? `Request failed with status ${res.status}`,
        envelope?.error.trace_id,
        envelope?.error.retryable ?? res.status >= 500,
      )
    }

    return (await res.json()) as T
  } catch (err) {
    if (err instanceof ApiError) throw err
    throw new ApiUnreachableError(err)
  } finally {
    clearTimeout(timer)
  }
}

export const api = {
  health: (opts?: RequestOptions) => request<HealthResponse>('/health', opts),
  meta: (opts?: RequestOptions) => request<MetaResponse>('/api/v1/meta', opts),
  url: API_URL,
}
