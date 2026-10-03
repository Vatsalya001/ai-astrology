// Command rewidth-kb changes the embedding dimension of the knowledge
// base.
//
// PHASE-05 task 5.5. §4's warning is the reason it exists:
//
//	pgvector columns are FIXED-dimension. Moving from nomic-embed-text
//	(768) to a 1024-dim model means: add `embedding_v2 vector(1024)`,
//	backfill, switch reads, drop the old column. Write the migration while
//	the corpus is small rather than discovering it at 100k.
//
// Four phases, run separately and in order, with the destructive one gated
// on the check:
//
//	go run ./cmd/rewidth-kb --to 1024 --emit       # write the phase-1 migration
//	task migrate                                   # apply it
//	go run ./cmd/rewidth-kb --to 1024 --backfill   # re-embed every chunk
//	go run ./cmd/rewidth-kb --to 1024 --verify     # parity check
//	go run ./cmd/rewidth-kb --to 1024 --swap       # drop old, rename new
//
// ── Why this is a separate command and not part of ingest-kb ──
//
// Because `ingest-kb --apply` is routine and this is not. A model change
// is a once-a-year event that rebuilds every vector in the product, and
// putting it behind a flag on the command people run weekly is how it gets
// run by accident. The commands share `internal/knowledge`, so the
// chunking and the embedding path are the same code either way.
//
// See docs/RUNBOOK-embedding-dimension-change.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/config"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/knowledge"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
)

func main() {
	to := flag.Int("to", 0, "target embedding dimension (required)")
	emit := flag.Bool("emit", false, "write the phase-1 migration pair to db/migrations/")
	backfill := flag.Bool("backfill", false, "phase 2: re-embed every chunk into the shadow column")
	verify := flag.Bool("verify", false, "phase 3: parity check; read-only")
	swap := flag.Bool("swap", false, "phase 4: drop the old column and rename the new one")
	migrationsDir := flag.String("migrations", "db/migrations", "where --emit writes")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if *to == 0 {
		logger.Error("--to is required",
			slog.String("hint", "the target dimension, e.g. --to 1024"))
		os.Exit(2)
	}

	// Exactly one phase per invocation. Not a convenience restriction:
	// chaining --backfill --swap in one process would run the destructive
	// step on the same breath as the step whose output it depends on, with
	// nobody having looked at the verify in between.
	selected := 0
	for _, chosen := range []bool{*emit, *backfill, *verify, *swap} {
		if chosen {
			selected++
		}
	}
	if selected != 1 {
		logger.Error("choose exactly one phase",
			slog.String("phases", "--emit | --backfill | --verify | --swap"),
			slog.String("why", "the destructive phase must not run in the same "+
				"process as the one it depends on"))
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *to, phaseFlags{
		emit: *emit, backfill: *backfill, verify: *verify, swap: *swap,
		migrationsDir: *migrationsDir,
	}, logger); err != nil {
		logger.Error("rewidth", slog.Any("err", err))
		os.Exit(1)
	}
}

type phaseFlags struct {
	emit, backfill, verify, swap bool
	migrationsDir                string
}

func run(ctx context.Context, to int, flags phaseFlags, logger *slog.Logger) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	// Read, never assume. A plan whose `From` does not match the live
	// column is refused rather than half-applied — and the commonest
	// reason for a mismatch is that somebody already ran part of this.
	from, err := liveDimension(ctx, pool)
	if err != nil {
		return err
	}
	plan := knowledge.RewidthPlan{From: from, To: to}

	fmt.Printf("knowledge_chunks.embedding is vector(%d), target vector(%d)\n\n", from, to)

	switch {
	case flags.emit:
		return emitMigration(plan, flags.migrationsDir)
	case flags.backfill:
		return runBackfill(ctx, pool, plan, logger)
	case flags.verify:
		return runVerify(ctx, pool, plan)
	case flags.swap:
		return runSwap(ctx, pool, plan, logger)
	}

	return errors.New("no phase selected")
}

// liveDimension reads the declared width of the live column.
//
// From `information_schema` via pgvector's own typmod encoding, because
// there is no portable "dimensions of this vector column" view. Reading a
// row's vector instead would report the width of whatever happens to be
// stored, which is the same number right up until the one case that
// matters: a column that was widened while its rows were not.
func liveDimension(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var dimension int
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(atttypmod, -1)
		FROM pg_attribute
		WHERE attrelid = 'knowledge_chunks'::regclass
		  AND attname = 'embedding'
		  AND NOT attisdropped`).Scan(&dimension)
	if err != nil {
		return 0, fmt.Errorf("read the embedding column's width: %w", err)
	}
	if dimension < 1 {
		return 0, fmt.Errorf(
			"knowledge_chunks.embedding reports width %d — the column is not a "+
				"fixed-dimension vector, so this tool cannot reason about it", dimension)
	}
	return dimension, nil
}

func emitMigration(plan knowledge.RewidthPlan, dir string) error {
	up, down, err := knowledge.RewidthMigration(plan)
	if err != nil {
		return err
	}

	next, err := nextMigrationNumber(dir)
	if err != nil {
		return err
	}

	base := fmt.Sprintf("%06d_embedding_%d", next, plan.To)
	upPath := filepath.Join(dir, base+".up.sql")
	downPath := filepath.Join(dir, base+".down.sql")

	for path, content := range map[string]string{upPath: up, downPath: down} {
		// Refused rather than overwritten. A migration that already exists
		// has probably already been applied somewhere, and rewriting it is
		// how two environments end up with the same version number and
		// different schemas.
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; delete it or pick another number", path)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}

	fmt.Printf("wrote\n  %s\n  %s\n\nnext: review them, then `task migrate`.\n",
		upPath, downPath)
	return nil
}

func nextMigrationNumber(dir string) (int, error) {
	existing, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return 0, fmt.Errorf("list migrations: %w", err)
	}
	if len(existing) == 0 {
		return 0, fmt.Errorf("no migrations found in %s — wrong --migrations path?", dir)
	}

	highest := 0
	for _, path := range existing {
		var number int
		if _, err := fmt.Sscanf(filepath.Base(path), "%06d_", &number); err != nil {
			continue
		}
		if number > highest {
			highest = number
		}
	}
	if highest == 0 {
		return 0, fmt.Errorf("could not read a migration number from %s", dir)
	}

	return highest + 1, nil
}

// runBackfill re-embeds every chunk into the shadow column.
//
// Reads chunk content straight from the database rather than re-chunking
// the corpus, and that is deliberate: this phase must change the VECTORS
// and nothing else. Re-chunking here would move boundaries at the same
// time as changing the model, and the two effects on retrieval quality
// would be impossible to separate afterwards. Re-chunking is
// `ingest-kb --apply --force`, which is a different operation.
func runBackfill(
	ctx context.Context, pool *pgxpool.Pool, plan knowledge.RewidthPlan, logger *slog.Logger,
) error {
	cfg, err := config.LoadKnowledgeBase()
	if err != nil {
		return err
	}

	ai, err := buildAI()
	if err != nil {
		return err
	}

	pending, err := countPending(ctx, pool)
	if err != nil {
		return err
	}
	if pending == 0 {
		fmt.Println("nothing to backfill: every chunk already has a shadow vector.")
		return nil
	}

	fmt.Printf("backfilling %d chunks in batches of %d\n", pending, cfg.EmbedBatchSize)

	done := 0
	for {
		ids, texts, err := nextPendingBatch(ctx, pool, cfg.EmbedBatchSize)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			break
		}

		response, err := ai.Embed(ctx, texts)
		if err != nil {
			return fmt.Errorf("embed batch starting at %d: %w", done, err)
		}
		if len(response.Embeddings) != len(ids) {
			// The alignment failure again. Here it would attach every
			// vector in the batch to the wrong chunk, and because the
			// shadow column is written per id, nothing downstream would
			// notice.
			return fmt.Errorf("provider returned %d vectors for %d chunks",
				len(response.Embeddings), len(ids))
		}

		for index, id := range ids {
			vector := response.Embeddings[index]
			if len(vector) != plan.To {
				return fmt.Errorf(
					"chunk %s: provider returned %d dimensions, plan says %d. "+
						"The model behind ai-service is not the one this plan is for",
					id, len(vector), plan.To)
			}

			if _, err := pool.Exec(ctx,
				`UPDATE knowledge_chunks
				 SET embedding_v2 = $2::vector, embedding_model = $3
				 WHERE id = $1`,
				id, knowledge.FormatVector(vector), response.Model); err != nil {
				return fmt.Errorf("write shadow vector for %s: %w", id, err)
			}
		}

		done += len(ids)
		logger.Info("backfill progress",
			slog.Int("done", done), slog.Int("total", pending),
			slog.String("model", response.Model))
	}

	fmt.Printf("\nbackfilled %d chunks. Next: --verify.\n", done)
	return nil
}

func countPending(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var pending int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM knowledge_chunks WHERE embedding_v2 IS NULL`).Scan(&pending)
	if err != nil {
		return 0, fmt.Errorf("count pending chunks (did phase 1 run?): %w", err)
	}
	return pending, nil
}

// nextPendingBatch takes the next slice of unembedded chunks.
//
// Ordered by id so the batches are stable across a restart: an interrupted
// backfill resumes rather than reshuffling, which matters because the
// progress number is the only thing telling an operator whether a
// long-running backfill is advancing.
func nextPendingBatch(
	ctx context.Context, pool *pgxpool.Pool, size int,
) (ids []string, texts []string, err error) {
	rows, err := pool.Query(ctx, `
		SELECT id::text, content FROM knowledge_chunks
		WHERE embedding_v2 IS NULL
		ORDER BY id
		LIMIT $1`, size)
	if err != nil {
		return nil, nil, fmt.Errorf("read pending chunks: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, content string
		if err := rows.Scan(&id, &content); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		texts = append(texts, content)
	}

	return ids, texts, rows.Err()
}

// runVerify is the gate on the destructive phase.
func runVerify(ctx context.Context, pool *pgxpool.Pool, plan knowledge.RewidthPlan) error {
	result, err := Verify(ctx, pool, plan)
	if err != nil {
		return err
	}

	fmt.Printf("parity check\n")
	fmt.Printf("  chunks                  %d\n", result.Chunks)
	fmt.Printf("  shadow vectors          %d\n", result.ShadowVectors)
	fmt.Printf("  missing shadow vectors  %d\n", result.MissingShadow)
	fmt.Printf("  wrong-width shadow      %d\n", result.WrongWidth)
	fmt.Printf("  distinct models         %d\n", result.Models)
	fmt.Printf("  identical to the old    %d\n", result.IdenticalToOld)

	if len(result.Problems) > 0 {
		fmt.Printf("\n✗ not safe to swap:\n")
		for _, problem := range result.Problems {
			fmt.Printf("    %s\n", problem)
		}
		return errors.New("parity check failed")
	}

	fmt.Printf("\n✓ safe to swap. Next: --swap (destructive).\n")
	return nil
}

func runSwap(
	ctx context.Context, pool *pgxpool.Pool, plan knowledge.RewidthPlan, logger *slog.Logger,
) error {
	// Re-run rather than trusting that somebody ran --verify. The gate is
	// only a gate if it cannot be skipped, and "I ran it a minute ago" is
	// not a property of the database.
	result, err := Verify(ctx, pool, plan)
	if err != nil {
		return err
	}
	if len(result.Problems) > 0 {
		fmt.Printf("✗ refusing to swap:\n")
		for _, problem := range result.Problems {
			fmt.Printf("    %s\n", problem)
		}
		return errors.New("the parity check does not pass; swapping would lose the corpus")
	}

	statements, err := knowledge.RewidthSQL(knowledge.RewidthSwap, plan)
	if err != nil {
		return err
	}

	// One transaction. Partway through this sequence the table has no
	// usable `embedding` column at all, and leaving it there — a dropped
	// old column and an un-renamed new one — is a knowledge base that
	// every retrieval query errors against.
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, statement := range statements {
		logger.Info("swap", slog.String("sql", statement))
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("%s: %w", statement, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	fmt.Printf("\n✓ knowledge_chunks.embedding is now vector(%d).\n", plan.To)
	fmt.Printf("  Update EMBEDDING_DIM and the migration 000007 comment, and re-run\n")
	fmt.Printf("  `go run ./cmd/ingest-kb --check` to confirm the corpus still loads.\n")
	return nil
}

func buildAI() (*clients.AI, error) {
	url := os.Getenv("AI_SERVICE_URL")
	if url == "" {
		return nil, errors.New("AI_SERVICE_URL is not set")
	}
	token := os.Getenv("INTERNAL_TOKEN")
	if token == "" {
		return nil, errors.New("INTERNAL_TOKEN is not set")
	}
	return clients.NewAI(url, token, 10*time.Second)
}
