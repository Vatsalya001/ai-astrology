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

export interface Check {
  status: CheckStatus
  latency_ms: number
  error?: string
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
