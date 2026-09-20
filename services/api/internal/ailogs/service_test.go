package ailogs

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients/aiclient"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// fakeQuerier captures what would have been written.
//
// The interface is declared by this package rather than by dbgen, so the
// double implements five methods instead of the whole generated surface
// — which is the point of a consumer-declared interface.
type fakeQuerier struct {
	inserted dbgen.InsertAIRequestLogParams
	summary  dbgen.GetAIUsageSummaryRow
	byJob    []dbgen.GetAIUsageByJobRow
	rows     []dbgen.AiRequestLog
	err      error
}

func (f *fakeQuerier) InsertAIRequestLog(_ context.Context, arg dbgen.InsertAIRequestLogParams) (dbgen.AiRequestLog, error) {
	f.inserted = arg
	if f.err != nil {
		return dbgen.AiRequestLog{}, f.err
	}
	return dbgen.AiRequestLog{TraceID: arg.TraceID, CostMicros: arg.CostMicros}, nil
}

func (f *fakeQuerier) ListAIRequestLogsForUser(context.Context, dbgen.ListAIRequestLogsForUserParams) ([]dbgen.AiRequestLog, error) {
	return f.rows, f.err
}

func (f *fakeQuerier) ListAISafetyIncidents(context.Context, dbgen.ListAISafetyIncidentsParams) ([]dbgen.AiRequestLog, error) {
	return f.rows, f.err
}

func (f *fakeQuerier) GetAIUsageSummary(context.Context, dbgen.GetAIUsageSummaryParams) (dbgen.GetAIUsageSummaryRow, error) {
	return f.summary, f.err
}

func (f *fakeQuerier) GetAIUsageByJob(context.Context, dbgen.GetAIUsageByJobParams) ([]dbgen.GetAIUsageByJobRow, error) {
	return f.byJob, f.err
}

func ptr[T any](v T) *T { return &v }

func telemetry() *aiclient.Telemetry {
	return &aiclient.Telemetry{
		TraceId:          "trace-1",
		JobType:          "chat_response",
		Intent:           ptr("kundli"),
		ProviderId:       ptr("anthropic"),
		Model:            ptr("claude-sonnet-5"),
		Tier:             ptr("chat"),
		PromptVersion:    ptr("v1"),
		ContextVersion:   ptr("chart.v1"),
		InputTokens:      ptr(120),
		OutputTokens:     ptr(400),
		CachedTokens:     ptr(8000),
		CacheWriteTokens: ptr(0),
		LatencyMs:        ptr(2400),
		CostMicros:       ptr(int64(9_360)),
		ModelCalls:       ptr(2),
	}
}

// ─── the rule this package exists to keep ────────────────────────────

// TestRecordHasNowhereToPutContent is a compile-time argument, written
// as a test so it is read.
//
// `Record` takes a telemetry struct and two IDs. There is deliberately
// no parameter for the message, the answer, or a violation excerpt, and
// the table has no column for any of them. PHASE-04 §14 and
// .claude/rules/security.md.
//
// A test asserting "the written row contains no content" would be weaker:
// it would pass today and keep passing while someone added a `Note`
// field, because the test would not know to look at it.
func TestRecordHasNowhereToPutContent(t *testing.T) {
	q := &fakeQuerier{}
	svc := New(q)

	if _, err := svc.Record(context.Background(), telemetry(), pgtype.UUID{}, pgtype.UUID{}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Every field that reached the INSERT, serialised. Nothing here may
	// look like prose — if a content-carrying field is ever added to the
	// telemetry contract, this is where it shows up.
	encoded, err := json.Marshal(q.inserted)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, forbidden := range []string{"message", "answer", "excerpt", "content", "text"} {
		if strings.Contains(strings.ToLower(string(encoded)), `"`+forbidden) {
			t.Errorf("the insert carries a %q field; ai_request_logs must hold no message content", forbidden)
		}
	}
}

func TestRecordRejectsMissingTelemetry(t *testing.T) {
	// A model call that happened and was never billed is a hole in the
	// usage table, invisible until an invoice is reconciled against it.
	svc := New(&fakeQuerier{})

	_, err := svc.Record(context.Background(), nil, pgtype.UUID{}, pgtype.UUID{})

	if !errors.Is(err, ErrTelemetryMissing) {
		t.Fatalf("want ErrTelemetryMissing, got %v", err)
	}
}

// ─── money ───────────────────────────────────────────────────────────

func TestCostIsCarriedExactly(t *testing.T) {
	q := &fakeQuerier{}
	tel := telemetry()
	tel.CostMicros = ptr(int64(9_223_372_036_854_775_807)) // max int64

	if _, err := New(q).Record(context.Background(), tel, pgtype.UUID{}, pgtype.UUID{}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	if q.inserted.CostMicros != math.MaxInt64 {
		t.Errorf("cost was altered in transit: got %d", q.inserted.CostMicros)
	}
}

// TestTokenCountsClampRatherThanWrap pins the one narrowing conversion
// in this package.
//
// The columns are INTEGER and the contract carries `int`. A plain
// int32(x) on a value above 2^31 WRAPS, frequently to a negative number,
// which then fails the column's `>= 0` CHECK and rejects the whole row —
// losing a real cost record over an implausible token count.
func TestTokenCountsClampRatherThanWrap(t *testing.T) {
	q := &fakeQuerier{}
	tel := telemetry()
	tel.InputTokens = ptr(math.MaxInt32 + 1000)

	if _, err := New(q).Record(context.Background(), tel, pgtype.UUID{}, pgtype.UUID{}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	if q.inserted.InputTokens != math.MaxInt32 {
		t.Errorf("want clamp to MaxInt32, got %d", q.inserted.InputTokens)
	}
	if q.inserted.InputTokens < 0 {
		t.Error("the narrowing wrapped to a negative count, which the column CHECK would reject")
	}
}

func TestCostIsNotClamped(t *testing.T) {
	// The negative case for the test above: cost is BIGINT and needs no
	// narrowing, and clamping money would silently discard a charge.
	q := &fakeQuerier{}
	tel := telemetry()
	tel.CostMicros = ptr(int64(math.MaxInt32) + 1000)

	if _, err := New(q).Record(context.Background(), tel, pgtype.UUID{}, pgtype.UUID{}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	if q.inserted.CostMicros != int64(math.MaxInt32)+1000 {
		t.Errorf("cost was clamped: got %d", q.inserted.CostMicros)
	}
}

// ─── defaults ────────────────────────────────────────────────────────

// TestAbsentBooleansTakeOppositeDefaults pins a distinction that is easy
// to collapse.
//
// `validation_passed` defaults TRUE — an absent field means nothing
// reported a failure. `regenerated` defaults FALSE — an absent field
// means no retry was spent. Defaulting both the same way would either
// mark every row as a validation failure or hide every retry.
func TestAbsentBooleansTakeOppositeDefaults(t *testing.T) {
	q := &fakeQuerier{}
	tel := telemetry()
	tel.ValidationPassed = nil
	tel.Regenerated = nil

	if _, err := New(q).Record(context.Background(), tel, pgtype.UUID{}, pgtype.UUID{}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	if !q.inserted.ValidationPassed {
		t.Error("absent validation_passed defaulted to false; every row would read as a failure")
	}
	if q.inserted.Regenerated {
		t.Error("absent regenerated defaulted to true; every row would read as a retry")
	}
}

func TestAnAbsentFinishReasonIsNotEmpty(t *testing.T) {
	// The column is NOT NULL and an empty finish reason is not something
	// a dashboard can group by.
	q := &fakeQuerier{}
	tel := telemetry()
	tel.FinishReason = nil

	if _, err := New(q).Record(context.Background(), tel, pgtype.UUID{}, pgtype.UUID{}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	if q.inserted.FinishReason != "stop" {
		t.Errorf("want %q, got %q", "stop", q.inserted.FinishReason)
	}
}

func TestEmptySafetyFlagsMarshalToArrayNotNull(t *testing.T) {
	// A NULL there would make every query filtering on the array need a
	// COALESCE, and one of them would forget.
	q := &fakeQuerier{}

	if _, err := New(q).Record(context.Background(), telemetry(), pgtype.UUID{}, pgtype.UUID{}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	if string(q.inserted.SafetyFlags) != "[]" {
		t.Errorf("want %q, got %q", "[]", string(q.inserted.SafetyFlags))
	}
}

func TestSafetyFlagsAreCarried(t *testing.T) {
	// The negative case: always writing "[]" would pass the test above
	// and lose every incident.
	q := &fakeQuerier{}
	tel := telemetry()
	tel.SafetyFlags = &[]aiclient.SafetyFlag{{Type: "fabricated_chart_fact", Severity: "block"}}

	if _, err := New(q).Record(context.Background(), tel, pgtype.UUID{}, pgtype.UUID{}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	if !strings.Contains(string(q.inserted.SafetyFlags), "fabricated_chart_fact") {
		t.Errorf("safety flags were dropped: %q", string(q.inserted.SafetyFlags))
	}
}

// ─── derived figures ─────────────────────────────────────────────────

func TestCacheHitRate(t *testing.T) {
	// PHASE-04 §15 names "prompt caching silently stops working in prod"
	// as a risk. This ratio is what makes it visible, so its edges are
	// worth pinning — particularly the zero case, which is a quiet hour
	// rather than a divide-by-zero.
	cases := []struct {
		name          string
		cached, fresh int64
		want          float64
	}{
		{"no traffic", 0, 0, 0},
		{"nothing cached", 0, 1000, 0},
		{"everything cached", 1000, 0, 1},
		{"nine in ten", 900, 100, 0.9},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cacheHitRate(tc.cached, tc.fresh); got != tc.want {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestCostPerRequestIsIntegerDivision(t *testing.T) {
	// Derived from an exact total. Computing it as a float and rounding
	// back would introduce the one thing invariant 4 forbids, in the one
	// place nobody would look for it.
	q := &fakeQuerier{summary: dbgen.GetAIUsageSummaryRow{
		RequestCount: 3,
		CostMicros:   10,
	}}

	usage, err := New(q).UsageSummary(context.Background(), time.Now(), time.Now())
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}

	if usage.CostPerRequestMicros != 3 {
		t.Errorf("want 3 (10/3 truncated), got %d", usage.CostPerRequestMicros)
	}
}

func TestCostPerRequestSurvivesAQuietWindow(t *testing.T) {
	// Zero requests is a quiet hour, not a panic.
	q := &fakeQuerier{summary: dbgen.GetAIUsageSummaryRow{}}

	usage, err := New(q).UsageSummary(context.Background(), time.Now(), time.Now())
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}

	if usage.CostPerRequestMicros != 0 {
		t.Errorf("want 0, got %d", usage.CostPerRequestMicros)
	}
}

// ─── incidents ───────────────────────────────────────────────────────

func TestIncidentsCarryNoExcerpt(t *testing.T) {
	// The table has no excerpt column and the struct has no excerpt
	// field. An admin sees WHICH rule fired and on which trace;
	// reading content means reproducing the request, which is the right
	// amount of friction for looking at somebody's conversation.
	q := &fakeQuerier{rows: []dbgen.AiRequestLog{{
		TraceID:     "trace-9",
		JobType:     "chat_response",
		Model:       "claude-sonnet-5",
		SafetyFlags: []byte(`[{"type":"fabricated_chart_fact","severity":"block"}]`),
	}}}

	incidents, err := New(q).Incidents(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("Incidents: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("want 1 incident, got %d", len(incidents))
	}

	encoded, err := json.Marshal(incidents[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), "excerpt") {
		t.Errorf("an incident carried an excerpt: %s", encoded)
	}
	if incidents[0].SafetyFlags[0].Type != "fabricated_chart_fact" {
		t.Errorf("the flag type was lost: %+v", incidents[0].SafetyFlags)
	}
}

func TestMalformedFlagsDoNotBreakTheFeed(t *testing.T) {
	// A bad row is not a reason to fail the whole incident feed — and
	// the feed is what an operator opens when something is already
	// wrong.
	q := &fakeQuerier{rows: []dbgen.AiRequestLog{{SafetyFlags: []byte(`not json`)}}}

	incidents, err := New(q).Incidents(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("Incidents: %v", err)
	}
	if len(incidents) != 1 || len(incidents[0].SafetyFlags) != 0 {
		t.Errorf("want one incident with no flags, got %+v", incidents)
	}
}
