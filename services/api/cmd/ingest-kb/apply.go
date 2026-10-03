package main

// The --apply half: embed through ai-service, write to Postgres.
//
// Kept apart from main.go so the chunking path and the writing path read
// as two separate things, because they have different prerequisites and
// different failure modes. `--check` needs nothing running; `--apply`
// needs Postgres, ai-service and an embedding model.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/config"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/knowledge"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
)

// generalServiceTimeout matches the service's `SERVICE_TIMEOUT` default.
//
// Not read from the environment, because this process does not load the
// full Config — a chunking tool that demands a JWT secret and an S3
// endpoint is a tool people run with made-up ones. The only call this
// command makes is Embed, which sets its own 120s budget.
const generalServiceTimeout = 10 * time.Second

// applyCorpus embeds and stores everything loadCorpus produced.
func applyCorpus(
	corpus []loadedDocument, cfg *config.KnowledgeBase, force bool, logger *slog.Logger,
) error {
	// Cancelled on Ctrl-C rather than killed. Ingestion commits per
	// document, so an interrupted run leaves a correct partial corpus and
	// re-running finishes it — but only if the in-flight transaction is
	// allowed to roll back rather than having its connection yanked.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is not set")
	}
	aiURL := os.Getenv("AI_SERVICE_URL")
	if aiURL == "" {
		return fmt.Errorf("AI_SERVICE_URL is not set")
	}
	internalToken := os.Getenv("INTERNAL_TOKEN")
	if internalToken == "" {
		// Checked here rather than discovered as a 401 on the first batch.
		// `.claude/rules/security.md` requires X-Internal-Token on every
		// service-to-service call, so an empty one is a configuration
		// error and not an anonymous-access path.
		return fmt.Errorf("INTERNAL_TOKEN is not set")
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	// The general per-call budget, not an embedding one: `clients.NewAI`
	// sizes its HTTP transport from the LARGEST of its budgets, and the
	// 120s embed budget is one of them. Passing something large here
	// instead would silently raise the ceiling for every other call on
	// this client, which is the mistake in the other direction.
	ai, err := clients.NewAI(aiURL, internalToken, generalServiceTimeout)
	if err != nil {
		return fmt.Errorf("build ai client: %w", err)
	}

	store := newPGStore(pool)

	before, err := store.Counts(ctx)
	if err != nil {
		return fmt.Errorf("read corpus counts: %w", err)
	}

	documents := make([]knowledge.ChunkedDocument, 0, len(corpus))
	for _, loaded := range corpus {
		documents = append(documents, knowledge.ChunkedDocument{
			Document: loaded.doc,
			Chunks:   loaded.chunks,
		})
	}

	fmt.Printf("\ningesting %d documents (batch %d, force=%v)\n",
		len(documents), cfg.EmbedBatchSize, force)

	started := time.Now()
	report, err := knowledge.Ingest(ctx, documents, &aiEmbedder{ai: ai}, store,
		knowledge.IngestOptions{
			BatchSize: cfg.EmbedBatchSize,
			Force:     force,
			// Printed per document because a 600-document run against a
			// cold local model takes minutes, and silence during it is
			// indistinguishable from a hang.
			Progress: func(result knowledge.DocumentResult) {
				verb := "ingested"
				if result.Skipped {
					verb = "unchanged"
				}
				fmt.Printf("  %-9s %-44s %d chunks\n", verb, result.Path, result.Chunks)
			},
		})

	// Reported even on failure. A run that died at document 412 did real
	// work on 411 of them, and "how far did it get" is the first question.
	if report != nil {
		printIngestReport(report, time.Since(started))
	}
	if err != nil {
		return err
	}

	after, err := store.Counts(ctx)
	if err != nil {
		return fmt.Errorf("read corpus counts: %w", err)
	}
	printCorpusState(before, after, logger)

	return nil
}

func printIngestReport(report *knowledge.IngestReport, elapsed time.Duration) {
	fmt.Printf("\nrun\n")
	fmt.Printf("  documents          %d\n", report.Documents)
	fmt.Printf("  ingested           %d\n", report.Ingested)
	fmt.Printf("  unchanged          %d\n", report.Skipped)
	fmt.Printf("  chunks written     %d\n", report.Chunks)
	fmt.Printf("  embeddings         %d\n", report.Embeddings)
	if report.Model != "" {
		fmt.Printf("  embedding model    %s\n", report.Model)
	}
	fmt.Printf("  elapsed            %s\n", elapsed.Round(time.Millisecond))
}

func printCorpusState(before, after CorpusCounts, logger *slog.Logger) {
	fmt.Printf("\nstored corpus\n")
	fmt.Printf("  documents          %d (was %d)\n", after.Documents, before.Documents)
	fmt.Printf("  chunks             %d (was %d)\n", after.Chunks, before.Chunks)
	fmt.Printf("  embedding models   %d\n", after.EmbeddingModels)

	// Both of these are states the corpus can be in without anything
	// having errored, and both are invisible from the application side —
	// which is why they are checked here rather than left to be noticed.
	if after.Unembedded > 0 {
		logger.Warn("chunks without embeddings",
			slog.Int64("count", after.Unembedded),
			slog.String("why", "invisible to vector search while still matching "+
				"keyword search; reads as a ranking mystery rather than as missing data"))
	}
	if after.EmbeddingModels > 1 {
		logger.Warn("the corpus holds vectors from more than one model",
			slog.Int64("models", after.EmbeddingModels),
			slog.String("why", "cosine similarity between vectors from different "+
				"models is a number with no meaning, so ranking is arbitrary"),
			slog.String("fix", "re-run with --force"))
	}
}
