package main

import (
	"strings"
	"testing"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/knowledge"
)

// The SQL generator, unit-tested. The thing that goes wrong in a dimension
// change is not the statements but their ORDER, and order is cheap to
// assert and impossible to notice in review.

func TestTheSwapDropsBeforeItRenames(t *testing.T) {
	statements, err := knowledge.RewidthSQL(
		knowledge.RewidthSwap, knowledge.RewidthPlan{From: 768, To: 1024})
	if err != nil {
		t.Fatal(err)
	}

	position := func(fragment string) int {
		for index, statement := range statements {
			if strings.Contains(statement, fragment) {
				return index
			}
		}
		t.Fatalf("no statement contains %q:\n%s", fragment, strings.Join(statements, "\n"))
		return -1
	}

	dropIndex := position("DROP INDEX IF EXISTS kc_embedding_idx")
	dropColumn := position("DROP COLUMN embedding")
	rename := position("RENAME COLUMN embedding_v2 TO embedding")
	createIndex := position("CREATE INDEX kc_embedding_idx")

	// The old HNSW index is built over the column about to go. Dropping it
	// after the rename leaves an index whose name says `embedding` over
	// data that is no longer there.
	if dropIndex > dropColumn {
		t.Error("the old index is dropped after its column")
	}
	if dropColumn > rename {
		t.Error("the old column is dropped after the rename, so both would " +
			"briefly be called `embedding`")
	}
	// Rebuilt last, over the renamed column. Built earlier it would index
	// `embedding_v2` under a name claiming otherwise.
	if createIndex < rename {
		t.Error("the new index is created before the rename")
	}
}

func TestTheSwapRecreatesBothIndexesThatReferencedTheOldColumn(t *testing.T) {
	// `kc_embedding_idx` (HNSW) and `kc_unembedded_idx` (partial, on
	// `embedding IS NULL`) are both defined over the dropped column and
	// both go with it. A swap that forgets either leaves a corpus that
	// works and is quietly slow — the exact failure
	// internal/platform/db/knowledge_schema_test.go exists to catch, and
	// which no query reports.
	statements, err := knowledge.RewidthSQL(
		knowledge.RewidthSwap, knowledge.RewidthPlan{From: 768, To: 1024})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(statements, "\n")

	for _, want := range []string{
		"USING hnsw (embedding vector_cosine_ops)",
		"kc_unembedded_idx",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the swap does not recreate %q:\n%s", want, joined)
		}
	}
}

func TestTheSwapNeverAltersTheColumnTypeInPlace(t *testing.T) {
	// `ALTER COLUMN embedding TYPE vector(1024)` is the obvious one-liner
	// and it is the trap. pgvector cannot cast between widths, so it
	// errors — the good case. The bad case is somebody "fixing" that with
	// a drop-and-add, which succeeds, discards every vector in the corpus,
	// and leaves a knowledge base that keyword-searches fine and
	// vector-searches not at all.
	for _, phase := range []knowledge.RewidthPhase{
		knowledge.RewidthAddColumn, knowledge.RewidthSwap,
	} {
		statements, err := knowledge.RewidthSQL(phase, knowledge.RewidthPlan{From: 768, To: 1024})
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range statements {
			if strings.Contains(statement, "ALTER COLUMN") {
				t.Errorf("phase %v alters a column type in place: %s", phase, statement)
			}
		}
	}
}

func TestTheBackfillPhaseProducesNoSQL(t *testing.T) {
	// It needs the model. §4's whole point is that this step is not a
	// schema change, and treating it as one is how somebody writes
	// `ALTER COLUMN … TYPE` and loses the corpus.
	statements, err := knowledge.RewidthSQL(
		knowledge.RewidthBackfill, knowledge.RewidthPlan{From: 768, To: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 0 {
		t.Errorf("the backfill phase emitted SQL:\n%s", strings.Join(statements, "\n"))
	}
}

func TestDegeneratePlansAreRefused(t *testing.T) {
	cases := map[string]knowledge.RewidthPlan{
		"same width":       {From: 768, To: 768},
		"zero target":      {From: 768, To: 0},
		"negative target":  {From: 768, To: -1},
		"zero source":      {From: 0, To: 1024},
		"above HNSW limit": {From: 768, To: 4096},
	}

	for name, plan := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := knowledge.RewidthSQL(knowledge.RewidthAddColumn, plan); err == nil {
				t.Error("accepted")
			}
		})
	}
}

func TestTheGeneratedMigrationSaysWhatItDoesNotDo(t *testing.T) {
	// The up file contains phase 1 only, and the generated text has to say
	// so, because the thing somebody will do with it is run `task migrate`
	// and assume the dimension changed. It did not: every vector still has
	// to be re-embedded.
	up, down, err := knowledge.RewidthMigration(knowledge.RewidthPlan{From: 768, To: 1024})
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"phase 1 only", "RE-EMBEDDED", "--backfill", "--verify", "--swap"} {
		if !strings.Contains(up, want) {
			t.Errorf("the up migration does not mention %q", want)
		}
	}

	// The up file must not EXECUTE the swap. A migration that performed it
	// would run the destructive phase the moment somebody types
	// `task migrate`, with no backfill and no parity check.
	for _, line := range strings.Split(up, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		if strings.Contains(trimmed, "DROP COLUMN embedding") ||
			strings.Contains(trimmed, "RENAME COLUMN") {
			t.Errorf("the up migration executes the swap: %s", trimmed)
		}
	}

	// `.claude/rules/database.md`: "Every migration has a real down."
	// Here the honest down is narrow, and the file has to say where its
	// limit is rather than implying it reverses the whole procedure.
	if !strings.Contains(down, "DROP COLUMN IF EXISTS embedding_v2") {
		t.Error("the down does not drop the shadow column")
	}
	for _, want := range []string{"BEFORE the swap", "ingest-kb --apply --force"} {
		if !strings.Contains(down, want) {
			t.Errorf("the down does not explain %q", want)
		}
	}
}

func TestTheAddColumnPhaseIsRerunnable(t *testing.T) {
	// Phase 1 gets run twice by anybody who is unsure whether it worked.
	// `IF NOT EXISTS` on both statements makes the second run a no-op
	// rather than an error that reads as "something is broken".
	statements, err := knowledge.RewidthSQL(
		knowledge.RewidthAddColumn, knowledge.RewidthPlan{From: 768, To: 1024})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range statements {
		if !strings.Contains(statement, "IF NOT EXISTS") {
			t.Errorf("not re-runnable: %s", statement)
		}
	}
}

func TestPhaseNamesAreStable(t *testing.T) {
	// They appear in operator output and in the runbook. A renumbering
	// that changed them would make the runbook wrong without failing
	// anything.
	want := map[knowledge.RewidthPhase]string{
		knowledge.RewidthAddColumn: "add-column",
		knowledge.RewidthBackfill:  "backfill",
		knowledge.RewidthVerify:    "verify",
		knowledge.RewidthSwap:      "swap",
	}
	for phase, name := range want {
		if phase.String() != name {
			t.Errorf("phase %d is %q, want %q", int(phase), phase.String(), name)
		}
	}
}
