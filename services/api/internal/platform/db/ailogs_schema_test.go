//go:build integration

package db_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// The Phase 4 cost table, asserted against real Postgres.
//
// Two of these are about money and one is about what "delete my account"
// means for a billing record. None of them can be checked by a unit test
// — a CHECK constraint and an ON DELETE rule live in the database, and a
// mock would simply agree with whatever the code did.

func anAILog(traceID string) dbgen.InsertAIRequestLogParams {
	return dbgen.InsertAIRequestLogParams{
		TraceID:        traceID,
		JobType:        "chat_response",
		ProviderID:     "anthropic",
		Model:          "claude-sonnet-5",
		Tier:           "chat",
		PromptVersion:  "v1",
		ContextVersion: "chart.v1",
		FinishReason:   "stop",
		SafetyFlags:    []byte("[]"),
	}
}

// TestCostMicrosIsBigintNotNumeric pins invariant 4 at the column.
//
// `.claude/CLAUDE.md`: money is an integer. A NUMERIC column would read
// back through a float somewhere in the stack and the error would appear
// months later as an invoice that does not reconcile. Asserted by
// storing a value no float64 can hold exactly: 2^53 + 1 round-trips
// through BIGINT and does not through a double.
func TestCostMicrosSurvivesAValueFloatCannotHold(t *testing.T) {
	pool, cleanup := schemaPool(t)
	defer cleanup()

	ctx := context.Background()
	q := dbgen.New(pool)

	const beyondFloat64 = int64(1)<<53 + 1

	params := anAILog("trace-bigint")
	params.CostMicros = beyondFloat64

	row, err := q.InsertAIRequestLog(ctx, params)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// The round trip IS the whole test. A DOUBLE PRECISION or
	// float-routed NUMERIC column stores 2^53+1 as 2^53, so the value
	// read back would be one less than the value written.
	//
	// The first version of this test added a second assertion —
	// `float64(row.CostMicros) == float64(beyondFloat64-1)` — meant to
	// detect the collapse. It fires on a CORRECT int64 too, because the
	// conversion happens in the test rather than in the column: casting
	// any int64 of 2^53+1 to float64 rounds it down. It failed against a
	// working BIGINT, which is the right way to find out an assertion is
	// measuring the test instead of the subject.
	if row.CostMicros != beyondFloat64 {
		t.Errorf("cost changed in the round trip: wrote %d, read %d. "+
			"A float-backed column loses exactly this value.", beyondFloat64, row.CostMicros)
	}
}

func TestNegativeCountsAreRefused(t *testing.T) {
	pool, cleanup := schemaPool(t)
	defer cleanup()

	ctx := context.Background()
	q := dbgen.New(pool)

	cases := []struct {
		name  string
		apply func(*dbgen.InsertAIRequestLogParams)
	}{
		{"cost", func(p *dbgen.InsertAIRequestLogParams) { p.CostMicros = -1 }},
		{"input tokens", func(p *dbgen.InsertAIRequestLogParams) { p.InputTokens = -1 }},
		{"output tokens", func(p *dbgen.InsertAIRequestLogParams) { p.OutputTokens = -1 }},
		{"latency", func(p *dbgen.InsertAIRequestLogParams) { p.LatencyMs = -1 }},
		{"model calls", func(p *dbgen.InsertAIRequestLogParams) { p.ModelCalls = -1 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := anAILog("trace-negative-" + tc.name)
			tc.apply(&params)

			if _, err := q.InsertAIRequestLog(ctx, params); err == nil {
				t.Fatalf("a negative %s was accepted", tc.name)
			} else if !strings.Contains(err.Error(), "violates check constraint") {
				t.Fatalf("refused for the wrong reason: %v", err)
			}
		})
	}
}

// TestDeletingAUserKeepsTheCostHistory pins ON DELETE SET NULL.
//
// CASCADE is the reflex and it is wrong here. Phase 7 reconciles an
// invoice that was already issued, and a row that vanished cannot be
// reconciled against anything. Nulling the ID removes the link to the
// person and leaves the money — which is what "delete my account" should
// mean for a billing record, and is safe because the row carries no
// content.
func TestDeletingAUserKeepsTheCostHistory(t *testing.T) {
	pool, cleanup := schemaPool(t)
	defer cleanup()

	ctx := context.Background()
	q := dbgen.New(pool)

	var userID pgtype.UUID
	err := pool.QueryRow(ctx,
		`INSERT INTO users (email, role) VALUES ($1, 'user') RETURNING id`,
		"delete-me@example.test",
	).Scan(&userID)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	params := anAILog("trace-orphan")
	params.UserID = userID
	params.CostMicros = 4_200

	if _, err := q.InsertAIRequestLog(ctx, params); err != nil {
		t.Fatalf("insert log: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var cost int64
	var remaining pgtype.UUID
	err = pool.QueryRow(ctx,
		`SELECT cost_micros, user_id FROM ai_request_logs WHERE trace_id = 'trace-orphan'`,
	).Scan(&cost, &remaining)
	if err != nil {
		t.Fatalf("the cost row was deleted with the user: %v", err)
	}

	if cost != 4_200 {
		t.Errorf("cost changed: want 4200, got %d", cost)
	}
	if remaining.Valid {
		t.Error("user_id survived the delete; the row is still linked to the person")
	}
}

// TestSafetyFlagsDefaultToAnEmptyArray guards every query that reads the
// column.
//
// A NULL there would make each one need a COALESCE, and one of them
// would forget — producing a nil dereference on the incident feed, which
// is the screen an operator opens when something is already wrong.
func TestSafetyFlagsDefaultToAnEmptyArray(t *testing.T) {
	pool, cleanup := schemaPool(t)
	defer cleanup()

	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		INSERT INTO ai_request_logs
			(trace_id, job_type, provider_id, model, tier,
			 prompt_version, context_version, finish_reason)
		VALUES ('trace-default', 'chat_response', 'mock', 'm', 'fast', 'v1', 'none', 'stop')`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var flags []byte
	err = pool.QueryRow(ctx,
		`SELECT safety_flags FROM ai_request_logs WHERE trace_id = 'trace-default'`,
	).Scan(&flags)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if string(flags) != "[]" {
		t.Errorf("want %q, got %q", "[]", string(flags))
	}
}

// TestTheUsageSummaryIsZeroNotNullOverAnEmptyWindow.
//
// SUM over zero rows is NULL, and a dashboard rendering "null requests,
// null cost" for a quiet hour is indistinguishable from a broken query.
// The COALESCEs in the query are what prevent that, and this is what
// says they are still there.
func TestTheUsageSummaryIsZeroNotNullOverAnEmptyWindow(t *testing.T) {
	pool, cleanup := schemaPool(t)
	defer cleanup()

	ctx := context.Background()
	q := dbgen.New(pool)

	// A window in the past with nothing in it.
	from := time.Date(2001, time.January, 1, 0, 0, 0, 0, time.UTC)

	row, err := q.GetAIUsageSummary(ctx, dbgen.GetAIUsageSummaryParams{
		CreatedAt:   from,
		CreatedAt_2: from.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("summary: %v", err)
	}

	if row.RequestCount != 0 || row.CostMicros != 0 || row.InputTokens != 0 {
		t.Errorf("want zeroes over an empty window, got %+v", row)
	}
}
