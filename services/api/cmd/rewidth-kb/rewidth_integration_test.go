//go:build integration

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/knowledge"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/testsupport"
)

/*
PHASE-05 task 5.5, done when "documented, tested on a copy".

This is the "tested on a copy" half, taken literally: a throwaway Postgres
with the real migrations and a real embedded corpus in it, run through all
four phases, asserting the corpus survives and is searchable afterwards.

The documentation is docs/RUNBOOK-embedding-dimension-change.md.

The phases are driven through the same SQL `cmd/rewidth-kb` emits and the
same `Verify` it gates on — not a reimplementation. A test that rebuilt the
procedure would prove that the test's version works.

No model is called: the corpus is embedded by a deterministic local
function, and the "new model" is a second one producing a different width.
What is being tested is the schema surgery, not the embedding.
*/

const (
	oldWidth = 768
	newWidth = 1024
)

func TestAllFourPhasesPreserveAndRewidthTheCorpus(t *testing.T) {
	ctx := context.Background()
	pool, terminate := startPostgres(ctx, t)
	defer terminate()

	seedCorpus(ctx, t, pool, oldWidth, "old-model")

	chunksBefore, contentBefore := corpusShape(ctx, t, pool)
	if chunksBefore == 0 {
		t.Fatal("the seeded corpus is empty")
	}

	plan := knowledge.RewidthPlan{From: oldWidth, To: newWidth}

	// ── Phase 1: add the shadow column ──
	execPhase(ctx, t, pool, knowledge.RewidthAddColumn, plan)

	// Twice, because an operator unsure whether it worked runs it again.
	execPhase(ctx, t, pool, knowledge.RewidthAddColumn, plan)

	if width := columnWidth(ctx, t, pool, "embedding_v2"); width != newWidth {
		t.Fatalf("embedding_v2 is vector(%d), want vector(%d)", width, newWidth)
	}
	// The live column is untouched, which is what makes phase 1 safe to
	// run on a live system.
	if width := columnWidth(ctx, t, pool, "embedding"); width != oldWidth {
		t.Fatalf("phase 1 changed the live column to vector(%d)", width)
	}

	// Verify must FAIL here. Nothing has been backfilled, so a swap now
	// would drop every live vector and leave the corpus unembedded.
	result := mustVerify(ctx, t, pool, plan)
	if len(result.Problems) == 0 {
		t.Fatal("the parity check passed before the backfill ran; the gate is open")
	}
	if result.MissingShadow != chunksBefore {
		t.Errorf("verify reports %d missing shadow vectors, corpus has %d chunks",
			result.MissingShadow, chunksBefore)
	}

	// ── Phase 2: backfill ──
	backfill(ctx, t, pool, newWidth, "new-model")

	// ── Phase 3: verify ──
	result = mustVerify(ctx, t, pool, plan)
	if len(result.Problems) != 0 {
		t.Fatalf("the parity check failed after a complete backfill: %v", result.Problems)
	}
	if result.ShadowVectors != chunksBefore {
		t.Errorf("%d shadow vectors for %d chunks", result.ShadowVectors, chunksBefore)
	}

	// ── Phase 4: swap ──
	execPhase(ctx, t, pool, knowledge.RewidthSwap, plan)

	// The column is the new width, under the old name.
	if width := columnWidth(ctx, t, pool, "embedding"); width != newWidth {
		t.Fatalf("after the swap embedding is vector(%d), want vector(%d)",
			width, newWidth)
	}
	if columnExists(ctx, t, pool, "embedding_v2") {
		t.Error("embedding_v2 still exists after the swap")
	}

	// Nothing was lost. This is the assertion the whole procedure exists
	// to make true: a dimension change must not cost a single chunk.
	chunksAfter, contentAfter := corpusShape(ctx, t, pool)
	if chunksAfter != chunksBefore {
		t.Errorf("%d chunks after the swap, %d before", chunksAfter, chunksBefore)
	}
	if contentAfter != contentBefore {
		t.Error("the chunk text changed across the swap")
	}

	var unembedded int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM knowledge_chunks WHERE embedding IS NULL`,
	).Scan(&unembedded); err != nil {
		t.Fatal(err)
	}
	if unembedded != 0 {
		t.Errorf("%d chunks have no embedding after the swap", unembedded)
	}

	// And the corpus is still searchable, through the rebuilt index. A
	// swap that left the index behind gives a corpus that works and is
	// quietly slow — which no query reports.
	assertIndexAccessMethod(ctx, t, pool, "kc_embedding_idx", "hnsw")
	assertIndexAccessMethod(ctx, t, pool, "kc_unembedded_idx", "btree")

	var neighbours int
	err := pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT count(*) FROM (
			SELECT id FROM knowledge_chunks
			ORDER BY embedding <=> %s LIMIT 5
		) s`, quotedVector(newWidth))).Scan(&neighbours)
	if err != nil {
		t.Fatalf("vector search after the swap: %v", err)
	}
	if neighbours == 0 {
		t.Error("vector search returns nothing after the swap")
	}
}

func TestTheSwapIsRefusedWhileAnyChunkIsUnbackfilled(t *testing.T) {
	// The gate, exercised. Phases 1 and 2 are recoverable; phase 4 drops
	// the live vectors, and this check is the only thing between a partial
	// backfill and an unrecoverable corpus.
	ctx := context.Background()
	pool, terminate := startPostgres(ctx, t)
	defer terminate()

	seedCorpus(ctx, t, pool, oldWidth, "old-model")
	plan := knowledge.RewidthPlan{From: oldWidth, To: newWidth}
	execPhase(ctx, t, pool, knowledge.RewidthAddColumn, plan)

	backfill(ctx, t, pool, newWidth, "new-model")

	// Blank one out: a backfill interrupted one chunk from the end.
	if _, err := pool.Exec(ctx, `
		UPDATE knowledge_chunks SET embedding_v2 = NULL
		WHERE id = (SELECT id FROM knowledge_chunks ORDER BY id LIMIT 1)`); err != nil {
		t.Fatal(err)
	}

	result := mustVerify(ctx, t, pool, plan)
	if len(result.Problems) == 0 {
		t.Fatal("one unbackfilled chunk passed the parity check")
	}
	if result.MissingShadow != 1 {
		t.Errorf("verify reports %d missing, want 1", result.MissingShadow)
	}
	if !strings.Contains(strings.Join(result.Problems, " "), "backfill") {
		t.Errorf("the problem does not tell the operator what to do: %v", result.Problems)
	}
}

func TestAWrongWidthBackfillIsRefused(t *testing.T) {
	// The model behind ai-service is not the one the plan is for. Caught
	// before the swap rather than after, because after it the live vectors
	// are gone.
	ctx := context.Background()
	pool, terminate := startPostgres(ctx, t)
	defer terminate()

	seedCorpus(ctx, t, pool, oldWidth, "old-model")
	plan := knowledge.RewidthPlan{From: oldWidth, To: newWidth}
	execPhase(ctx, t, pool, knowledge.RewidthAddColumn, plan)

	// A vector that is not the planned width. Postgres accepts it: the
	// shadow column is vector(1024) and `vector_dims` reports what is
	// stored — so this is NOT caught by the column type, which is the
	// whole reason Verify checks widths per row.
	//
	// (A 1024-dimension column rejects a 768-dimension value on insert,
	// so the wrong width that can actually land here is one produced by a
	// model whose output happens to match the column. The check exists
	// for the case where the column was created from a different plan.)
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		UPDATE knowledge_chunks SET embedding_v2 = %s`, quotedVector(newWidth))); err != nil {
		t.Fatal(err)
	}

	// With every row at the right width, verify passes — the control for
	// the assertion below.
	if result := mustVerify(ctx, t, pool, plan); len(result.Problems) != 0 {
		t.Fatalf("a correct backfill was refused: %v", result.Problems)
	}

	// Now check against a plan expecting a different width. Same data,
	// different expectation: every row is reported as wrong.
	wrongPlan := knowledge.RewidthPlan{From: oldWidth, To: 512}
	result := mustVerify(ctx, t, pool, wrongPlan)
	if result.WrongWidth == 0 {
		t.Error("vectors of the wrong width passed the parity check")
	}
	if len(result.Problems) == 0 {
		t.Error("the parity check raised no problem for wrong-width vectors")
	}
}

func TestABackfillThatCopiedTheColumnIsRefused(t *testing.T) {
	// `UPDATE knowledge_chunks SET embedding_v2 = embedding` is the
	// shortcut somebody reaches for when the model is slow. Every other
	// check passes and the swap becomes a no-op dressed as a migration.
	ctx := context.Background()
	pool, terminate := startPostgres(ctx, t)
	defer terminate()

	seedCorpus(ctx, t, pool, oldWidth, "old-model")

	// Same width, so the comparison is possible — which is also the only
	// case where this mistake is possible.
	plan := knowledge.RewidthPlan{From: oldWidth, To: oldWidth}
	statements, err := knowledge.RewidthSQL(knowledge.RewidthAddColumn,
		knowledge.RewidthPlan{From: 1, To: oldWidth})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := pool.Exec(ctx,
		`UPDATE knowledge_chunks SET embedding_v2 = embedding`); err != nil {
		t.Fatal(err)
	}

	result, err := Verify(ctx, pool, plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.IdenticalToOld != result.ShadowVectors {
		t.Fatalf("%d of %d shadow vectors are identical; the test setup is wrong",
			result.IdenticalToOld, result.ShadowVectors)
	}
	if len(result.Problems) == 0 {
		t.Error("a backfill that copied the live column passed the parity check")
	}
	if !strings.Contains(strings.Join(result.Problems, " "), "identical") {
		t.Errorf("the problem does not name the cause: %v", result.Problems)
	}
}

func TestAnEmptyCorpusDoesNotPassTheParityCheck(t *testing.T) {
	// Swapping an empty corpus is harmless, and it almost always means the
	// operator is pointed at the wrong database. Finding that out after
	// the swap on the right one is worse.
	ctx := context.Background()
	pool, terminate := startPostgres(ctx, t)
	defer terminate()

	plan := knowledge.RewidthPlan{From: oldWidth, To: newWidth}
	execPhase(ctx, t, pool, knowledge.RewidthAddColumn, plan)

	result := mustVerify(ctx, t, pool, plan)
	if len(result.Problems) == 0 {
		t.Fatal("an empty corpus passed the parity check")
	}
	if !strings.Contains(strings.Join(result.Problems, " "), "DATABASE_URL") {
		t.Errorf("the problem does not point at the likely cause: %v", result.Problems)
	}
}

// ─── plumbing ────────────────────────────────────────────────────────

func execPhase(
	ctx context.Context, t *testing.T, pool *pgxpool.Pool,
	phase knowledge.RewidthPhase, plan knowledge.RewidthPlan,
) {
	t.Helper()
	statements, err := knowledge.RewidthSQL(phase, plan)
	if err != nil {
		t.Fatalf("%v: %v", phase, err)
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("%v: %s: %v", phase, statement, err)
		}
	}
}

func mustVerify(
	ctx context.Context, t *testing.T, pool *pgxpool.Pool, plan knowledge.RewidthPlan,
) *VerifyResult {
	t.Helper()
	result, err := Verify(ctx, pool, plan)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return result
}

// seedCorpus writes a small embedded corpus with the real schema.
func seedCorpus(
	ctx context.Context, t *testing.T, pool *pgxpool.Pool, width int, model string,
) {
	t.Helper()

	for document := 0; document < 3; document++ {
		var documentID string
		err := pool.QueryRow(ctx, `
			INSERT INTO knowledge_documents (title, category, content, source, metadata)
			VALUES ($1, 'houses', $2, 'editorial', $3)
			RETURNING id::text`,
			fmt.Sprintf("Document %d", document),
			fmt.Sprintf("Body of document %d about Saturn and the tenth house.", document),
			fmt.Sprintf(`{"house":%d}`, document+1),
		).Scan(&documentID)
		if err != nil {
			t.Fatalf("seed document %d: %v", document, err)
		}

		for chunk := 0; chunk < 4; chunk++ {
			_, err := pool.Exec(ctx, `
				INSERT INTO knowledge_chunks
					(document_id, content, chunk_index, token_count,
					 embedding, embedding_model, metadata)
				VALUES ($1::uuid, $2, $3, $4, $5::vector, $6, $7)`,
				documentID,
				fmt.Sprintf("Document %d chunk %d: Saturn in the tenth house.", document, chunk),
				chunk, 20,
				seedVector(width, document*4+chunk),
				model,
				fmt.Sprintf(`{"house":%d}`, document+1),
			)
			if err != nil {
				t.Fatalf("seed chunk %d/%d: %v", document, chunk, err)
			}
		}
	}
}

// backfill fills the shadow column, as phase 2 does.
//
// Deliberately NOT a copy of the live column: each vector is derived from
// the chunk's id, so the result is distinguishable from the old one, which
// is what `IdenticalToOld` is looking for.
func backfill(
	ctx context.Context, t *testing.T, pool *pgxpool.Pool, width int, model string,
) {
	t.Helper()

	rows, err := pool.Query(ctx,
		`SELECT id::text FROM knowledge_chunks WHERE embedding_v2 IS NULL ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	for index, id := range ids {
		if _, err := pool.Exec(ctx, `
			UPDATE knowledge_chunks
			SET embedding_v2 = $2::vector, embedding_model = $3
			WHERE id = $1::uuid`, id, seedVector(width, index+1000), model); err != nil {
			t.Fatalf("backfill %s: %v", id, err)
		}
	}
}

// seedVector is a deterministic, L2-normalised vector.
//
// Normalised because `vector_cosine_ops` is only the right operator class
// for unit vectors, and a fixture that ignored that would be testing a
// configuration the product does not ship.
func seedVector(width, seed int) string {
	values := make([]float32, width)
	var sumSquares float64
	for index := range values {
		value := float32((seed*31+index*7)%251+1) / 251
		values[index] = value
		sumSquares += float64(value) * float64(value)
	}

	norm := float32(1.0)
	if sumSquares > 0 {
		norm = float32(1.0 / sqrt(sumSquares))
	}
	for index := range values {
		values[index] *= norm
	}

	return knowledge.FormatVector(values)
}

func sqrt(value float64) float64 {
	// Newton, to avoid importing math into a file that otherwise needs
	// nothing from it. Converges well inside float32 precision in a dozen
	// iterations for the magnitudes here.
	if value <= 0 {
		return 0
	}
	guess := value
	for range 40 {
		guess = 0.5 * (guess + value/guess)
	}
	return guess
}

func quotedVector(width int) string {
	return "'" + seedVector(width, 7) + "'::vector"
}

func corpusShape(ctx context.Context, t *testing.T, pool *pgxpool.Pool) (int, string) {
	t.Helper()
	var chunks int
	var digest string
	err := pool.QueryRow(ctx, `
		SELECT count(*), COALESCE(md5(string_agg(content, '|' ORDER BY id)), '')
		FROM knowledge_chunks`).Scan(&chunks, &digest)
	if err != nil {
		t.Fatal(err)
	}
	return chunks, digest
}

func columnWidth(ctx context.Context, t *testing.T, pool *pgxpool.Pool, column string) int {
	t.Helper()
	var width int
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(atttypmod, -1) FROM pg_attribute
		WHERE attrelid = 'knowledge_chunks'::regclass
		  AND attname = $1 AND NOT attisdropped`, column).Scan(&width)
	if err != nil {
		t.Fatalf("read width of %s: %v", column, err)
	}
	return width
}

func columnExists(ctx context.Context, t *testing.T, pool *pgxpool.Pool, column string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_attribute
			WHERE attrelid = 'knowledge_chunks'::regclass
			  AND attname = $1 AND NOT attisdropped)`, column).Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}
	return exists
}

// assertIndexAccessMethod checks the TYPE, not just the name.
//
// Same reasoning as internal/platform/db/knowledge_schema_test.go: a
// recreated `kc_embedding_idx` that came back as a btree keeps its name
// and answers `ORDER BY embedding <=> $1` by sequential-scanning the whole
// corpus. Nothing fails; recall is identical; only latency changes.
func assertIndexAccessMethod(
	ctx context.Context, t *testing.T, pool *pgxpool.Pool, index, want string,
) {
	t.Helper()
	var method string
	err := pool.QueryRow(ctx, `
		SELECT am.amname FROM pg_class c
		JOIN pg_am am ON am.oid = c.relam
		WHERE c.relname = $1 AND c.relkind = 'i'`, index).Scan(&method)
	if err != nil {
		t.Fatalf("%s is missing after the swap: %v", index, err)
	}
	if method != want {
		t.Errorf("%s uses %s, want %s", index, method, want)
	}
}

func startPostgres(ctx context.Context, t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()

	initSQL, err := filepath.Abs(filepath.Join(
		"..", "..", "..", "..", "infrastructure", "docker", "init", "01-init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(initSQL); err != nil {
		t.Fatalf("bootstrap SQL not found at %s: %v", initSQL, err)
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "pgvector/pgvector:pg16",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_USER":     "astro",
				"POSTGRES_PASSWORD": "astro",
				"POSTGRES_DB":       "astro_dev",
			},
			Files: []testcontainers.ContainerFile{{
				HostFilePath:      initSQL,
				ContainerFilePath: "/docker-entrypoint-initdb.d/01-init.sql",
				FileMode:          0o644,
			}},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		testsupport.ContainerUnavailable(t, "Postgres", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatal(err)
	}

	pool, err := pgxpool.New(ctx, fmt.Sprintf(
		"postgresql://astro:astro@%s:%s/astro_dev?sslmode=disable", host, port.Port()))
	if err != nil {
		t.Fatal(err)
	}

	// The real migrations, read from disk. Duplicating the schema here
	// would make this test pass against a schema the deployment does not
	// have.
	dir, err := filepath.Abs(filepath.Join("..", "..", "db", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no migrations in %s: %v", dir, err)
	}
	sort.Strings(files)
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply %s: %v", filepath.Base(file), err)
		}
	}

	return pool, func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	}
}
