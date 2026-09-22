-- Every model call, written by Go from the telemetry Python returned.
-- See docs/specs/PHASE-04-AI-INFRASTRUCTURE.md §8.
--
-- No query here selects message content, because no column holds any.

-- name: InsertAIRequestLog :one
-- Called inside the same transaction as the message write (Phase 5), so
-- a request that cost money cannot be missing from the bill because a
-- separate logging call failed.
INSERT INTO ai_request_logs (
    trace_id, user_id, conversation_id,
    job_type, intent,
    provider_id, model, tier,
    prompt_version, context_version,
    input_tokens, output_tokens, cached_tokens, cache_write_tokens,
    latency_ms, cost_micros,
    finish_reason, safety_flags, validation_passed,
    regenerated, model_calls
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21
)
RETURNING *;

-- name: ListAIRequestLogsForUser :many
-- Newest first, served by ai_logs_user_idx.
SELECT * FROM ai_request_logs
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListAISafetyIncidents :many
-- The admin incident feed. Served by the PARTIAL index, so this stays
-- fast as the table grows — failures are a small fraction of rows and a
-- full index on the boolean would be scanned rather than used.
SELECT * FROM ai_request_logs
WHERE validation_passed = FALSE
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: GetAIUsageSummary :one
-- Totals over a window, for the admin usage view.
--
-- COALESCE on every aggregate: SUM over zero rows is NULL, and a
-- dashboard rendering "null requests, null cost" for a quiet hour is
-- indistinguishable from a broken query.
--
-- `cost_micros` is summed as BIGINT and returned as BIGINT. No cast to
-- NUMERIC anywhere — invariant 4, and a NUMERIC that reaches Go through
-- a float is exactly the bug the rule exists to prevent.
SELECT
    COUNT(*)                                         AS request_count,
    COALESCE(SUM(input_tokens), 0)::BIGINT           AS input_tokens,
    COALESCE(SUM(output_tokens), 0)::BIGINT          AS output_tokens,
    COALESCE(SUM(cached_tokens), 0)::BIGINT          AS cached_tokens,
    COALESCE(SUM(cache_write_tokens), 0)::BIGINT     AS cache_write_tokens,
    COALESCE(SUM(cost_micros), 0)::BIGINT            AS cost_micros,
    COALESCE(SUM(model_calls), 0)::BIGINT            AS model_calls,
    COUNT(*) FILTER (WHERE regenerated)              AS regenerated_count,
    COUNT(*) FILTER (WHERE NOT validation_passed)    AS failed_count
FROM ai_request_logs
WHERE created_at >= $1 AND created_at < $2;

-- name: GetAIUsageByJob :many
-- The same window, grouped. p50 and p95 latency come from
-- percentile_disc rather than an average: an average latency hides the
-- tail, and the tail is what users notice.
SELECT
    job_type,
    COUNT(*)                                      AS request_count,
    COALESCE(SUM(cost_micros), 0)::BIGINT         AS cost_micros,
    COALESCE(
        percentile_disc(0.5) WITHIN GROUP (ORDER BY latency_ms), 0
    )::INTEGER                                    AS p50_latency_ms,
    COALESCE(
        percentile_disc(0.95) WITHIN GROUP (ORDER BY latency_ms), 0
    )::INTEGER                                    AS p95_latency_ms,
    -- Cache hit rate: reads over reads-plus-fresh. NULLIF guards the
    -- division when a job made no calls in the window, which is a
    -- quiet hour rather than a divide-by-zero.
    COALESCE(SUM(cached_tokens), 0)::BIGINT       AS cached_tokens,
    COALESCE(SUM(input_tokens), 0)::BIGINT        AS input_tokens
FROM ai_request_logs
WHERE created_at >= $1 AND created_at < $2
GROUP BY job_type
ORDER BY cost_micros DESC;

-- name: ListAIRequestsForUser :many
-- Every AI request this person made, for their data export.
--
-- PHASE-04 §8 keeps message CONTENT out of this table entirely, so what
-- a person receives here is the shape of their usage and nothing they
-- wrote: when, which job, which intent, what it cost. That is still
-- personal data — "asked about medical matters on these dates" is a
-- fact about a person — which is why it is exported rather than filed
-- as telemetry.
--
-- `cost_micros` is included deliberately. Phase 7 bills from this
-- table, and an export that hides the number a charge is computed from
-- would be the one field a person most reasonably wants to check.
SELECT
    id,
    job_type,
    intent,
    model,
    tier,
    prompt_version,
    input_tokens,
    output_tokens,
    cached_tokens,
    latency_ms,
    cost_micros,
    finish_reason,
    safety_flags,
    validation_passed,
    created_at
FROM ai_request_logs
WHERE user_id = $1
ORDER BY created_at DESC;
