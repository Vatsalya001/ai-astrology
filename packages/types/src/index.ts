/**
 * Types shared between the web app and (from Phase 10) the mobile app.
 *
 * These mirror the JSON api-service returns. Hand-written on purpose: the
 * Go API's public surface is small and stable, and a generator would be more
 * machinery than the problem deserves.
 *
 * CROSS-SERVICE contracts are different — those are generated from OpenAPI
 * into Go clients and must never be hand-written (ADR-005).
 */

/** Health status, shared by every service in the system. */
export type CheckStatus = 'ok' | 'error' | 'degraded'

/**
 * Why a dependency probe failed.
 *
 * A closed vocabulary, never the underlying error text. `/health` is
 * unauthenticated by necessity — a load balancer cannot present a
 * credential — so anything it returns is public, and a raw probe error
 * reads `dial tcp 127.0.0.1:8025: connect: connection refused`, which is
 * the internal topology. The detail goes to the API's log, keyed by
 * trace ID.
 */
export type ProbeReason = 'timeout' | 'unreachable' | 'unavailable'

export interface Check {
  status: CheckStatus
  latency_ms: number
  reason?: ProbeReason
  /**
   * Operator-facing configuration the dependency reports about itself —
   * currently which model backend ai-service is wired to.
   *
   * Distinct from `reason`: this is present when the check *succeeds*,
   * and names a provider and tier rather than describing a failure. It
   * never carries a URL, a host or a key.
   */
  detail?: string
}

/**
 * GET /health on api-service.
 *
 * `checks` is keyed by dependency name. Storage and mail appear only
 * when a probe URL is configured, so consumers must not assume a fixed
 * key set.
 */
export interface HealthResponse {
  status: CheckStatus
  service: string
  version: string
  checks: Record<string, Check>
}

/** Dependencies the API reports on. Critical ones can down the service. */
export const CRITICAL_DEPENDENCIES = ['postgres', 'redis'] as const
/**
 * Feature flags. Every one defaults to false; each switches on as its
 * phase gate passes.
 */
export interface FeatureFlags {
  ai_chat: boolean
  voice: boolean
  astrologers: boolean
  compatibility: boolean
  payments: boolean
  pdf: boolean
}

/** GET /api/v1/meta — non-sensitive runtime facts for the client. */
export interface MetaResponse {
  service: string
  version: string
  env: 'development' | 'staging' | 'production'
  phase: string
  features: FeatureFlags
}
