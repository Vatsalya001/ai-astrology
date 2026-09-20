-- Phase 4 — every model call, written by Go because Python cannot.
--
-- ai-service holds a READ-ONLY role (ADR-001), so it cannot write its
-- own logs. PHASE-04 §8 turns that constraint into the design: Python
-- returns telemetry in the response envelope and Go persists it,
-- alongside the message, in one transaction. A request that cost money
-- therefore cannot be missing from the bill because a separate logging
-- call failed.
--
-- ── What this table must never contain ──
--
-- Message content. Not the question, not the answer, not an excerpt of
-- a blocked response. `.claude/rules/security.md` and PHASE-04 §14 are
-- both explicit, and the reason is the retention: this table is backed
-- up, replicated, and read by a usage-billing job in Phase 7. A
-- sentence from someone's reading about their marriage does not belong
-- in any of those places.
--
-- `safety_flags` therefore carries types and severities only. The cost
-- is real — an admin reviewing a block sees `fabricated_chart_fact`
-- rather than the sentence — and it is the right trade.

CREATE TABLE ai_request_logs (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Not a foreign key, and not nullable. The trace is minted by
    -- middleware on the way in and is the only thing that ties a row
    -- here to the Go request, the Python call and the log lines of
    -- both.
    trace_id         TEXT NOT NULL,

    -- Nullable, and ON DELETE SET NULL rather than CASCADE.
    --
    -- A deleted user must not take the cost history with them: Phase 7
    -- reconciles an invoice that was already issued, and a row that
    -- vanished cannot be reconciled against anything. Nulling the ID
    -- removes the link to the person while leaving the money. That is
    -- what "delete my account" should mean for a billing record, and
    -- the row carries no content, so nothing personal survives it.
    user_id          UUID REFERENCES users(id) ON DELETE SET NULL,

    -- No FK: conversations arrive in Phase 5. Declaring one now would
    -- either block this migration on a table that does not exist or
    -- require a second migration to add it later, and the column is
    -- written by the same service that will own that table.
    conversation_id  UUID,

    job_type         TEXT NOT NULL,
    intent           TEXT,

    -- Which model actually answered, not which one was configured.
    -- They differ the moment fallback exists, and a row recording the
    -- configured model cannot explain the answer that came back.
    provider_id      TEXT NOT NULL,
    model            TEXT NOT NULL,
    tier             TEXT NOT NULL,

    -- A response is unexplainable three weeks later without both: the
    -- prompt version says what was asked, the context version says what
    -- was shown.
    prompt_version   TEXT NOT NULL,
    context_version  TEXT NOT NULL,

    -- The three input classes stay disjoint, exactly as the provider
    -- adapters report them. Their prices differ by roughly twelve-fold,
    -- so one merged column cannot be priced and cannot show a cache hit
    -- rate.
    input_tokens        INTEGER NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens       INTEGER NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cached_tokens       INTEGER NOT NULL DEFAULT 0 CHECK (cached_tokens >= 0),
    cache_write_tokens  INTEGER NOT NULL DEFAULT 0 CHECK (cache_write_tokens >= 0),

    latency_ms       INTEGER NOT NULL DEFAULT 0 CHECK (latency_ms >= 0),

    -- BIGINT micro-USD. Never NUMERIC, never a float.
    -- `.claude/CLAUDE.md` invariant 4. This column is what Phase 7's
    -- usage billing reads; float addition does not associate, so the
    -- same charges summed in a different order would give a different
    -- total, which is indefensible on an invoice.
    --
    -- Zero for local free models, which is what makes the dev/prod cost
    -- delta visible in one table rather than needing a second code path.
    cost_micros      BIGINT NOT NULL DEFAULT 0 CHECK (cost_micros >= 0),

    finish_reason    TEXT NOT NULL,

    -- [{"type": "...", "severity": "warn"|"block"}]. Types and
    -- severities. No excerpt, ever — see the header.
    safety_flags     JSONB NOT NULL DEFAULT '[]',
    validation_passed BOOLEAN NOT NULL DEFAULT TRUE,

    -- Whether the single corrective retry was spent. Two full
    -- generations for one answer, so this is the number that says
    -- whether output validation is costing real money.
    regenerated      BOOLEAN NOT NULL DEFAULT FALSE,

    -- Every model call this request made, including classification and
    -- screening. §8 wants cost per completed TASK rather than per
    -- request — "a cheap request needing three retries isn't cheap" —
    -- and this is the denominator that makes that computable.
    model_calls      INTEGER NOT NULL DEFAULT 0 CHECK (model_calls >= 0),

    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Per-user history, newest first. The admin usage view and Phase 7's
-- billing both read exactly this shape.
CREATE INDEX ai_logs_user_idx ON ai_request_logs (user_id, created_at DESC);

-- Per-job aggregates: p50/p95 latency and token volume by job type.
CREATE INDEX ai_logs_job_idx ON ai_request_logs (job_type, created_at DESC);

-- Partial, because it is the incident feed and failures are rare.
-- A full index on a boolean would be scanned rather than used; this one
-- is small enough to stay in cache and is the only access path the
-- admin incidents endpoint needs.
CREATE INDEX ai_logs_failed_idx ON ai_request_logs (created_at DESC)
    WHERE validation_passed = FALSE;

-- Trace lookup: "this user reported a bad answer, here is their trace
-- ID". Not unique — one Go request can make several model calls that
-- are logged separately.
CREATE INDEX ai_logs_trace_idx ON ai_request_logs (trace_id);
