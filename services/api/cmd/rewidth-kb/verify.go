package main

// The parity check, and the gate on the destructive phase.
//
// Its own file because it is the only part of this command that must not
// be wrong. Phases 1 and 2 are recoverable — drop the shadow column, run
// the backfill again. Phase 4 drops the live vectors, and the only thing
// standing between a bad backfill and an unrecoverable corpus is what this
// file decides.
//
// Every check here answers a question of the form "what would be true
// after a swap, and silently wrong?"

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/knowledge"
)

// VerifyResult is what the parity check found.
type VerifyResult struct {
	Chunks        int
	ShadowVectors int
	MissingShadow int
	WrongWidth    int
	Models        int

	// IdenticalToOld counts shadow vectors byte-identical to the live
	// ones. See the check below: this is how a backfill that did nothing
	// looks.
	IdenticalToOld int

	// Problems is empty when it is safe to swap. Each entry is a sentence
	// an operator can act on.
	Problems []string
}

// Verify decides whether the swap is safe.
func Verify(
	ctx context.Context, pool *pgxpool.Pool, plan knowledge.RewidthPlan,
) (*VerifyResult, error) {
	result := &VerifyResult{}

	err := pool.QueryRow(ctx, `
		SELECT
			count(*),
			count(embedding_v2),
			count(*) FILTER (WHERE embedding_v2 IS NULL),
			count(*) FILTER (WHERE embedding_v2 IS NOT NULL
			                   AND vector_dims(embedding_v2) <> $1),
			count(DISTINCT embedding_model)
		FROM knowledge_chunks`, plan.To).Scan(
		&result.Chunks, &result.ShadowVectors, &result.MissingShadow,
		&result.WrongWidth, &result.Models)
	if err != nil {
		return nil, fmt.Errorf("parity check (did phase 1 run?): %w", err)
	}

	// An empty corpus passes every other check trivially, and swapping it
	// is harmless — but it almost always means the operator is pointed at
	// the wrong database, and finding that out AFTER the swap on the right
	// one is worse.
	if result.Chunks == 0 {
		result.Problems = append(result.Problems,
			"the corpus is empty — check DATABASE_URL points where you think it does")
		return result, nil
	}

	if result.MissingShadow > 0 {
		result.Problems = append(result.Problems, fmt.Sprintf(
			"%d of %d chunks have no shadow vector. Swapping now would drop their "+
				"live vector and leave them unembedded: invisible to vector search, "+
				"still matching keyword search. Run --backfill again",
			result.MissingShadow, result.Chunks))
	}

	if result.WrongWidth > 0 {
		result.Problems = append(result.Problems, fmt.Sprintf(
			"%d shadow vectors are not %d-dimensional — the model behind ai-service "+
				"is not the one this plan is for",
			result.WrongWidth, plan.To))
	}

	if result.Models > 1 {
		// Not pedantry. Cosine similarity between vectors from two
		// different models is a number with no meaning, so the corpus
		// would rank arbitrarily and nothing about the result would look
		// wrong.
		result.Problems = append(result.Problems, fmt.Sprintf(
			"the corpus holds vectors from %d models. Ranking across models is "+
				"meaningless; re-run --backfill against one model", result.Models))
	}

	// The check that catches a backfill which ran and did nothing.
	//
	// Reachable: a backfill against the SAME model and the same dimension
	// writes the identical vector into the shadow column, every check
	// above passes, and the swap is a no-op dressed as a migration. It is
	// also what a UPDATE … SET embedding_v2 = embedding would look like,
	// which is the shortcut somebody reaches for when the model is slow.
	//
	// Only meaningful when the widths match; across widths the comparison
	// cannot be made and does not need to be.
	if plan.From == plan.To {
		err = pool.QueryRow(ctx, `
			SELECT count(*) FROM knowledge_chunks
			WHERE embedding IS NOT NULL AND embedding_v2 IS NOT NULL
			  AND embedding::text = embedding_v2::text`).Scan(&result.IdenticalToOld)
		if err != nil {
			return nil, fmt.Errorf("compare shadow to live vectors: %w", err)
		}

		if result.IdenticalToOld == result.ShadowVectors && result.ShadowVectors > 0 {
			result.Problems = append(result.Problems,
				"every shadow vector is byte-identical to the live one. Either the "+
					"backfill copied the column instead of re-embedding, or "+
					"ai-service is still serving the old model")
		}
	}

	return result, nil
}
