package knowledge

import (
	"fmt"
	"strings"
)

// ── Changing the embedding dimension ──
//
// PHASE-05 task 5.5, and §4's warning in full:
//
//	pgvector columns are FIXED-dimension. Moving from nomic-embed-text
//	(768) to a 1024-dim model means: add `embedding_v2 vector(1024)`,
//	backfill, switch reads, drop the old column. Write the migration while
//	the corpus is small (a few thousand chunks, minutes to re-embed)
//	rather than discovering it at 100k.
//
// The SQL is generated here rather than hand-written into a migration for
// one reason: it is the same four statements every time, and the thing that
// goes wrong is not the SQL but the ORDER. A generator can be unit-tested
// against the phase order; a hand-written migration is reviewed once by
// somebody who is trying to ship a model change.
//
// ── What this cannot do ──
//
// It cannot convert the existing vectors. There is no transformation from
// a 768-dimension `nomic-embed-text` vector to a 1024-dimension one; the
// models have different training and different geometry, and padding or
// truncating produces vectors that are the right shape and semantically
// meaningless. **Every chunk has to be re-embedded.** That is why phase 2
// exists and why §4 says to do this while the corpus is small.
//
// See docs/RUNBOOK-embedding-dimension-change.md for the operator's view.

// RewidthPhase is one step of the shadow migration.
//
// Four phases, in this order, and the order is the whole design. A direct
// `ALTER TABLE … ALTER COLUMN embedding TYPE vector(1024)` is the obvious
// alternative and it fails: pgvector cannot cast between widths, so it
// errors — which is the good case. The bad case is doing it as
// drop-and-add, which succeeds, discards every vector in the corpus, and
// leaves a knowledge base that keyword-searches fine and vector-searches
// not at all.
type RewidthPhase int

const (
	// RewidthAddColumn adds `embedding_v2` alongside the live column.
	// Non-blocking: adding a nullable column takes no table rewrite.
	RewidthAddColumn RewidthPhase = iota + 1

	// RewidthBackfill re-embeds every chunk into the new column. Not SQL —
	// it needs the model. See Rewidth in cmd/rewidth-kb.
	RewidthBackfill

	// RewidthVerify is the parity check. Read-only, and the gate: phase 4
	// must not run until this passes, because phase 4 is the destructive
	// one.
	RewidthVerify

	// RewidthSwap drops the old column and renames the new one into its
	// place, then rebuilds the vector index.
	RewidthSwap
)

func (p RewidthPhase) String() string {
	switch p {
	case RewidthAddColumn:
		return "add-column"
	case RewidthBackfill:
		return "backfill"
	case RewidthVerify:
		return "verify"
	case RewidthSwap:
		return "swap"
	}
	return fmt.Sprintf("unknown(%d)", int(p))
}

// RewidthPlan describes one dimension change.
type RewidthPlan struct {
	// From is the current width, asserted before anything runs. Stated
	// rather than discovered so a plan that does not match the database
	// is refused instead of half-applied.
	From int

	// To is the target width.
	To int
}

// shadowColumn is the name the new vector lives under until the swap.
const shadowColumn = "embedding_v2"

func (p RewidthPlan) validate() error {
	// pgvector's own limit for an indexable vector. Above 2000 dimensions
	// HNSW refuses to build, and the column would be created successfully
	// and then be unindexable — which is a corpus that works and is slow
	// in a way no migration will fix.
	const maxIndexable = 2000

	switch {
	case p.From < 1 || p.To < 1:
		return fmt.Errorf("dimensions must be positive, got %d → %d", p.From, p.To)
	case p.From == p.To:
		return fmt.Errorf("from and to are both %d; there is nothing to do", p.From)
	case p.To > maxIndexable:
		return fmt.Errorf(
			"%d dimensions exceeds pgvector's %d-dimension HNSW limit: the column "+
				"would be created and then be unindexable", p.To, maxIndexable)
	}
	return nil
}

// RewidthSQL returns the statements for one phase.
//
// Phases that are not SQL (RewidthBackfill) return nil, and
// RewidthVerify returns nothing because its queries live with the code
// that interprets them — a verify step whose SQL is generated here and
// whose pass/fail rule is decided elsewhere is a check that can drift
// into always passing.
func RewidthSQL(phase RewidthPhase, plan RewidthPlan) ([]string, error) {
	if err := plan.validate(); err != nil {
		return nil, err
	}

	switch phase {
	case RewidthAddColumn:
		return []string{
			fmt.Sprintf(
				"ALTER TABLE knowledge_chunks ADD COLUMN IF NOT EXISTS %s vector(%d)",
				shadowColumn, plan.To),

			// The progress query for the backfill, and afterwards the
			// assertion that it finished. Partial, so in a completed
			// backfill it is empty and costs nothing — the same shape as
			// `kc_unembedded_idx` in migration 000007 and for the same
			// reason.
			fmt.Sprintf(
				"CREATE INDEX IF NOT EXISTS kc_%s_pending_idx ON knowledge_chunks (id) "+
					"WHERE %s IS NULL", shadowColumn, shadowColumn),
		}, nil

	case RewidthBackfill:
		// Deliberately nil. The backfill needs a model, and the whole
		// point of §4's note is that this step is not a schema change —
		// treating it as one is how somebody ends up writing
		// `ALTER COLUMN … TYPE` and losing the corpus.
		return nil, nil

	case RewidthVerify:
		return nil, nil

	case RewidthSwap:
		return []string{
			// Dropped first. The old HNSW index is built over the column
			// about to go, and leaving the drop until after the rename
			// would leave an index whose name says `embedding` over data
			// that is no longer there.
			"DROP INDEX IF EXISTS kc_embedding_idx",

			"ALTER TABLE knowledge_chunks DROP COLUMN embedding",

			fmt.Sprintf("ALTER TABLE knowledge_chunks RENAME COLUMN %s TO embedding",
				shadowColumn),

			fmt.Sprintf("DROP INDEX IF EXISTS kc_%s_pending_idx", shadowColumn),

			// Rebuilt last, over the renamed column.
			//
			// `vector_cosine_ops` again, and that is a real decision rather
			// than copying: it is correct only while the embeddings are
			// L2-normalised. `nomic-embed-text` returns unit vectors
			// (verified: norm 1.000000), and a model that does not would
			// need `vector_ip_ops` or `vector_l2_ops` instead. Keeping
			// cosine with unnormalised vectors is not an error — it
			// silently ranks by angle while the caller believes it is
			// ranking by similarity.
			"CREATE INDEX kc_embedding_idx ON knowledge_chunks " +
				"USING hnsw (embedding vector_cosine_ops)",

			// The partial index from 000007 referenced the dropped column
			// and went with it.
			"CREATE INDEX IF NOT EXISTS kc_unembedded_idx ON knowledge_chunks " +
				"(document_id) WHERE embedding IS NULL",
		}, nil
	}

	return nil, fmt.Errorf("unknown phase %v", phase)
}

// RewidthMigration renders the add-column and swap phases as a
// golang-migrate pair, for committing under db/migrations/.
//
// Generated rather than hand-written because `.claude/rules/database.md`
// requires every migration to have a real down, and the down for a
// dimension change is the part people get wrong: it cannot restore the old
// vectors, because they were dropped and cannot be recomputed from the new
// ones. The generated down says so in the file, where somebody reaching
// for it at 2am will read it.
func RewidthMigration(plan RewidthPlan) (up, down string, err error) {
	if err := plan.validate(); err != nil {
		return "", "", err
	}

	addColumn, err := RewidthSQL(RewidthAddColumn, plan)
	if err != nil {
		return "", "", err
	}
	swap, err := RewidthSQL(RewidthSwap, plan)
	if err != nil {
		return "", "", err
	}

	var upFile strings.Builder
	fmt.Fprintf(&upFile, `-- Embedding dimension: %d → %d.
--
-- GENERATED by cmd/rewidth-kb --emit. Do not hand-edit; regenerate.
--
-- ⚠ THIS MIGRATION IS NOT THE WHOLE PROCEDURE. It contains phase 1 only.
--
-- Phases 2 to 4 need the embedding model, because there is no
-- transformation from a %d-dimension vector to a %d-dimension one: the
-- models have different geometry, and padding or truncating produces
-- vectors that are the right shape and semantically meaningless. Every
-- chunk has to be RE-EMBEDDED.
--
-- Run, in this order:
--
--   task migrate                      # phase 1, this file
--   go run ./cmd/rewidth-kb --to %d --backfill
--   go run ./cmd/rewidth-kb --to %d --verify
--   go run ./cmd/rewidth-kb --to %d --swap
--
-- The swap is destructive and refuses to run until --verify passes. See
-- docs/RUNBOOK-embedding-dimension-change.md.

`, plan.From, plan.To, plan.From, plan.To, plan.To, plan.To, plan.To)

	for _, statement := range addColumn {
		fmt.Fprintf(&upFile, "%s;\n", statement)
	}

	var downFile strings.Builder
	fmt.Fprintf(&downFile, `-- Reverses the phase-1 column addition for %d → %d.
--
-- GENERATED by cmd/rewidth-kb --emit.
--
-- ⚠ This down is only meaningful BEFORE the swap. It drops the shadow
-- column, which costs the backfill — minutes of re-embedding — and no
-- data, because the live %d-dimension column is untouched.
--
-- AFTER the swap there is nothing here that can help you. The old column
-- was dropped and its vectors cannot be recomputed from the new ones.
-- Recovery is: restore the database, or re-run ingestion with
--
--   go run ./cmd/ingest-kb --apply --force
--
-- against a model producing %d-dimension vectors, which rebuilds the
-- whole corpus from packages/content/knowledge/ — the source of truth, and
-- in git. That is the real rollback plan and it is why `+
		"`ingest-kb`"+` is
-- idempotent.

DROP INDEX IF EXISTS kc_%s_pending_idx;
ALTER TABLE knowledge_chunks DROP COLUMN IF EXISTS %s;
`, plan.From, plan.To, plan.From, plan.From, shadowColumn, shadowColumn)

	// Rendered into the up file as a comment rather than executed: the
	// swap is a separate, gated step and a migration that performed it
	// would run it the moment somebody types `task migrate`.
	fmt.Fprintf(&upFile, "\n-- Phase 4, for reference. Run via --swap, NOT here:\n")
	for _, statement := range swap {
		fmt.Fprintf(&upFile, "--   %s;\n", statement)
	}

	return upFile.String(), downFile.String(), nil
}
