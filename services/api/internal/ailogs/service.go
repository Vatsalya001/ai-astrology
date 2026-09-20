// Package ailogs persists the telemetry ai-service returns and reads it
// back for the admin views.
//
// ── Why Go writes logs that Python produced ──
//
// ai-service connects as `astro_ro` and cannot write (ADR-001). PHASE-04
// §8 turns that constraint into the design: Python returns telemetry in
// the response envelope, Go persists it. From Phase 5 that INSERT runs
// in the same transaction as the message write, so a request that cost
// money cannot be missing from the bill because a separate logging call
// failed.
//
// ── The rule this package exists to keep ──
//
// No message content, ever. Not the question, not the answer, not an
// excerpt of a blocked response. `.claude/rules/security.md` and
// PHASE-04 §14. The table has no column for it and `Record` has no
// parameter for it, so the rule is enforced by the shape rather than by
// review — which is the only way it survives someone adding a field
// "just for debugging".
package ailogs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients/aiclient"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// ErrTelemetryMissing means ai-service answered without a telemetry
// block.
//
// Treated as an error rather than skipped, because the alternative is a
// model call that happened and was never billed. A hole in the usage
// table is invisible until an invoice is reconciled against it, at which
// point the calls it is missing are months old.
var ErrTelemetryMissing = errors.New("ailogs: response carried no telemetry")

// Querier is the slice of dbgen this package uses.
//
// Declared by the CONSUMER, per .claude/rules/go.md — so a test double
// implements four methods rather than the whole generated interface, and
// so the dependency graph stays acyclic.
type Querier interface {
	InsertAIRequestLog(ctx context.Context, arg dbgen.InsertAIRequestLogParams) (dbgen.AiRequestLog, error)
	ListAIRequestLogsForUser(ctx context.Context, arg dbgen.ListAIRequestLogsForUserParams) ([]dbgen.AiRequestLog, error)
	ListAISafetyIncidents(ctx context.Context, arg dbgen.ListAISafetyIncidentsParams) ([]dbgen.AiRequestLog, error)
	GetAIUsageSummary(ctx context.Context, arg dbgen.GetAIUsageSummaryParams) (dbgen.GetAIUsageSummaryRow, error)
	GetAIUsageByJob(ctx context.Context, arg dbgen.GetAIUsageByJobParams) ([]dbgen.GetAIUsageByJobRow, error)
}

type Service struct {
	q Querier
}

func New(q Querier) *Service { return &Service{q: q} }

// ─── writing ─────────────────────────────────────────────────────────

// Record persists one telemetry block.
//
// Note the parameters: a telemetry struct and two IDs. There is
// deliberately nowhere to pass a message, a response, or a violation
// excerpt.
func (s *Service) Record(
	ctx context.Context,
	t *aiclient.Telemetry,
	userID, conversationID pgtype.UUID,
) (dbgen.AiRequestLog, error) {
	if t == nil {
		return dbgen.AiRequestLog{}, ErrTelemetryMissing
	}

	flags, err := marshalFlags(t.SafetyFlags)
	if err != nil {
		return dbgen.AiRequestLog{}, fmt.Errorf("encode safety flags: %w", err)
	}

	row, err := s.q.InsertAIRequestLog(ctx, dbgen.InsertAIRequestLogParams{
		// Required in the contract, so a plain string rather than a
		// pointer — the one field here the generator did not make
		// optional, because a telemetry row with no trace cannot be
		// tied to anything.
		TraceID:        t.TraceId,
		UserID:         userID,
		ConversationID: conversationID,
		JobType:        t.JobType,
		Intent:         t.Intent,
		ProviderID:     deref(t.ProviderId),
		Model:          deref(t.Model),
		Tier:           deref(t.Tier),
		PromptVersion:  deref(t.PromptVersion),
		ContextVersion: deref(t.ContextVersion),

		// Narrowed from int to int32 to match INTEGER columns. A token
		// count above two billion is not a real request — it is a bug
		// upstream — and clamping rather than wrapping means the row
		// still lands with an implausible number instead of a negative
		// one that would pass the CHECK by accident.
		InputTokens:      clampInt32(derefInt(t.InputTokens)),
		OutputTokens:     clampInt32(derefInt(t.OutputTokens)),
		CachedTokens:     clampInt32(derefInt(t.CachedTokens)),
		CacheWriteTokens: clampInt32(derefInt(t.CacheWriteTokens)),
		LatencyMs:        clampInt32(derefInt(t.LatencyMs)),

		// NOT clamped, and the difference matters: cost is BIGINT here
		// and int64 in Python, so it needs no narrowing. Clamping money
		// would silently discard a charge.
		CostMicros: derefInt64(t.CostMicros),

		FinishReason:     finishReason(t.FinishReason),
		SafetyFlags:      flags,
		ValidationPassed: derefBool(t.ValidationPassed, true),
		Regenerated:      derefBool(t.Regenerated, false),
		ModelCalls:       clampInt32(derefInt(t.ModelCalls)),
	})
	if err != nil {
		return dbgen.AiRequestLog{}, fmt.Errorf("insert ai request log: %w", err)
	}
	return row, nil
}

// marshalFlags encodes the safety flags as JSONB.
//
// An empty list marshals to `[]` rather than `null`, matching the column
// default. A NULL there would make every query filtering on the array
// need a COALESCE, and one of them would forget.
func marshalFlags(flags *[]aiclient.SafetyFlag) ([]byte, error) {
	if flags == nil || len(*flags) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(*flags)
}

// ─── reading ─────────────────────────────────────────────────────────

// Usage is one window's totals, shaped for the admin view.
type Usage struct {
	RequestCount     int64 `json:"request_count"`
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CachedTokens     int64 `json:"cached_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	CostMicros       int64 `json:"cost_micros"`
	ModelCalls       int64 `json:"model_calls"`
	RegeneratedCount int64 `json:"regenerated_count"`
	FailedCount      int64 `json:"failed_count"`

	// Derived, not stored. Reads over reads-plus-fresh — the number that
	// says whether the biggest cost lever in the service is engaged.
	// PHASE-04 §15 lists "prompt caching silently stops working in prod"
	// as a named risk; this is what makes it visible.
	CacheHitRate float64 `json:"cache_hit_rate"`

	// §8: cost per completed TASK, not per request. "A cheap request
	// needing three retries isn't cheap."
	CostPerRequestMicros int64 `json:"cost_per_request_micros"`
}

func (s *Service) UsageSummary(ctx context.Context, from, to time.Time) (Usage, error) {
	row, err := s.q.GetAIUsageSummary(ctx, dbgen.GetAIUsageSummaryParams{
		CreatedAt:   from,
		CreatedAt_2: to,
	})
	if err != nil {
		return Usage{}, fmt.Errorf("ai usage summary: %w", err)
	}

	u := Usage{
		RequestCount:     row.RequestCount,
		InputTokens:      row.InputTokens,
		OutputTokens:     row.OutputTokens,
		CachedTokens:     row.CachedTokens,
		CacheWriteTokens: row.CacheWriteTokens,
		CostMicros:       row.CostMicros,
		ModelCalls:       row.ModelCalls,
		RegeneratedCount: row.RegeneratedCount,
		FailedCount:      row.FailedCount,
	}
	u.CacheHitRate = cacheHitRate(row.CachedTokens, row.InputTokens)

	// Integer division, deliberately. This is a display figure derived
	// from an exact total; computing it as a float and rounding back
	// would introduce the one thing invariant 4 forbids, in the one
	// place nobody would look for it.
	if row.RequestCount > 0 {
		u.CostPerRequestMicros = row.CostMicros / row.RequestCount
	}
	return u, nil
}

// cacheHitRate is cached tokens over total input tokens.
//
// A float, and the only one in this package — it is a ratio for a
// dashboard, never money, and it is recomputed from the integers on
// every read rather than stored.
func cacheHitRate(cached, fresh int64) float64 {
	total := cached + fresh
	if total <= 0 {
		return 0
	}
	return float64(cached) / float64(total)
}

// JobUsage is the same window grouped by job type.
type JobUsage struct {
	JobType      string  `json:"job_type"`
	RequestCount int64   `json:"request_count"`
	CostMicros   int64   `json:"cost_micros"`
	P50LatencyMs int32   `json:"p50_latency_ms"`
	P95LatencyMs int32   `json:"p95_latency_ms"`
	CacheHitRate float64 `json:"cache_hit_rate"`
}

func (s *Service) UsageByJob(ctx context.Context, from, to time.Time) ([]JobUsage, error) {
	rows, err := s.q.GetAIUsageByJob(ctx, dbgen.GetAIUsageByJobParams{
		CreatedAt:   from,
		CreatedAt_2: to,
	})
	if err != nil {
		return nil, fmt.Errorf("ai usage by job: %w", err)
	}

	out := make([]JobUsage, 0, len(rows))
	for _, r := range rows {
		out = append(out, JobUsage{
			JobType:      r.JobType,
			RequestCount: r.RequestCount,
			CostMicros:   r.CostMicros,
			P50LatencyMs: r.P50LatencyMs,
			P95LatencyMs: r.P95LatencyMs,
			CacheHitRate: cacheHitRate(r.CachedTokens, r.InputTokens),
		})
	}
	return out, nil
}

// Incident is one failed validation, for the admin feed.
//
// No excerpt field, because the table has no excerpt column. An admin
// sees WHICH rule fired and on which trace; reading the content means
// reproducing the request, which is the right amount of friction for
// looking at somebody's conversation.
type Incident struct {
	ID          string    `json:"id"`
	TraceID     string    `json:"trace_id"`
	JobType     string    `json:"job_type"`
	Intent      string    `json:"intent"`
	Model       string    `json:"model"`
	SafetyFlags []Flag    `json:"safety_flags"`
	Regenerated bool      `json:"regenerated"`
	CreatedAt   time.Time `json:"created_at"`
}

type Flag struct {
	Type     string `json:"type"`
	Severity string `json:"severity"`
}

func (s *Service) Incidents(ctx context.Context, limit, offset int32) ([]Incident, error) {
	rows, err := s.q.ListAISafetyIncidents(ctx, dbgen.ListAISafetyIncidentsParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("ai incidents: %w", err)
	}

	out := make([]Incident, 0, len(rows))
	for _, r := range rows {
		out = append(out, Incident{
			ID:          uuidString(r.ID),
			TraceID:     r.TraceID,
			JobType:     r.JobType,
			Intent:      deref(r.Intent),
			Model:       r.Model,
			SafetyFlags: decodeFlags(r.SafetyFlags),
			Regenerated: r.Regenerated,
			CreatedAt:   r.CreatedAt,
		})
	}
	return out, nil
}

// decodeFlags tolerates a malformed JSONB value.
//
// An unreadable flags column is a bad row, not a reason to fail the
// whole incident feed — and the feed is what an operator opens when
// something is already wrong.
func decodeFlags(raw []byte) []Flag {
	if len(raw) == 0 {
		return []Flag{}
	}
	var flags []Flag
	if err := json.Unmarshal(raw, &flags); err != nil {
		return []Flag{}
	}
	return flags
}
