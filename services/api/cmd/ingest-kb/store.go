package main

// Postgres and ai-service adapters for `knowledge.Store` and
// `knowledge.Embedder`.
//
// They live here rather than in `internal/knowledge` on purpose: that
// package's whole value is that chunking can be reasoned about and
// golden-tested without a database, a model or a network. The interfaces
// it declares are satisfied here, at the only place that wires them —
// which is also what lets the pipeline test substitute a deterministic
// embedder and run in CI under the rule that CI never calls a model.

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/knowledge"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// ─── the store ───────────────────────────────────────────────────────

// pgStore implements knowledge.Store against Postgres.
//
// Holds both a pool and a queries handle. The pool is nil inside a
// transaction, which is what makes a nested WithinTransaction a named
// error rather than a second transaction nobody meant to open.
type pgStore struct {
	pool    *pgxpool.Pool
	queries *dbgen.Queries
}

func newPGStore(pool *pgxpool.Pool) *pgStore {
	return &pgStore{pool: pool, queries: dbgen.New(pool)}
}

func (s *pgStore) WithinTransaction(ctx context.Context, fn func(tx knowledge.Store) error) error {
	if s.pool == nil {
		return errors.New("already inside a transaction")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// Rollback after a successful Commit is a no-op, so this needs no flag
	// tracking whether the commit happened.
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(&pgStore{queries: s.queries.WithTx(tx)}); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (s *pgStore) StoredChecksum(
	ctx context.Context, identity knowledge.Identity,
) (string, int, bool, error) {
	row, err := s.queries.GetKnowledgeDocumentChecksum(
		ctx, dbgen.GetKnowledgeDocumentChecksumParams{
			Title:    identity.Title,
			Language: identity.Language,
			Version:  int32(identity.Version),
		})
	if errors.Is(err, pgx.ErrNoRows) {
		// Absent, not an error. A first run has nothing stored for any
		// identity in the corpus.
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}

	return row.SourceChecksum, int(row.ChunkCount), true, nil
}

func (s *pgStore) UpsertDocument(
	ctx context.Context, doc *knowledge.Document,
) (string, error) {
	row, err := s.queries.UpsertKnowledgeDocument(ctx, dbgen.UpsertKnowledgeDocumentParams{
		Title:    doc.Title,
		Category: doc.Category,
		// The whole authored body, so the stored row is a complete record
		// of the document a human reviewed. Retrieval reads chunks, not
		// this — but a corpus you cannot reconstruct from the database is
		// a corpus whose provenance claims cannot be checked.
		Content:         doc.Body,
		Language:        doc.Language,
		Source:          doc.Source,
		Authority:       int16(doc.Authority),
		AstrologySystem: doc.AstrologySystem,
		Metadata:        doc.Metadata,
		SourceChecksum:  doc.Checksum,
	})
	if err != nil {
		return "", err
	}

	return uuid.UUID(row.ID.Bytes).String(), nil
}

func (s *pgStore) DeleteChunks(ctx context.Context, documentID string) error {
	id, err := parseUUID(documentID)
	if err != nil {
		return err
	}
	_, err = s.queries.DeleteKnowledgeChunks(ctx, id)
	return err
}

func (s *pgStore) InsertChunk(
	ctx context.Context, documentID string, chunk knowledge.Chunk, vector, model string,
) error {
	id, err := parseUUID(documentID)
	if err != nil {
		return err
	}

	return s.queries.InsertKnowledgeChunk(ctx, dbgen.InsertKnowledgeChunkParams{
		DocumentID:     id,
		Content:        chunk.Content,
		ChunkIndex:     int32(chunk.Index),
		TokenCount:     int32(chunk.TokenCount),
		Embedding:      vector,
		EmbeddingModel: model,
		Metadata:       chunk.Metadata,
	})
}

// CorpusCounts is what the database holds, for the --status report.
type CorpusCounts struct {
	Documents       int64
	Chunks          int64
	Unembedded      int64
	EmbeddingModels int64
}

func (s *pgStore) Counts(ctx context.Context) (CorpusCounts, error) {
	row, err := s.queries.CountKnowledgeCorpus(ctx)
	if err != nil {
		return CorpusCounts{}, err
	}
	return CorpusCounts{
		Documents:       row.Documents,
		Chunks:          row.Chunks,
		Unembedded:      row.Unembedded,
		EmbeddingModels: row.EmbeddingModels,
	}, nil
}

func parseUUID(value string) (pgtype.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("parse document id %q: %w", value, err)
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}, nil
}

// ─── the embedder ────────────────────────────────────────────────────

// aiEmbedder implements knowledge.Embedder against ai-service.
type aiEmbedder struct {
	ai *clients.AI
}

func (e *aiEmbedder) Embed(
	ctx context.Context, texts []string,
) ([][]float32, string, error) {
	response, err := e.ai.Embed(ctx, texts)
	if err != nil {
		return nil, "", err
	}

	// The model name comes from the RESPONSE, not from local config.
	//
	// It is stored in `embedding_model` on every chunk, and the point of
	// storing it is to know what actually produced the vector. Reading it
	// from this process's environment would record what this process
	// BELIEVED ai-service was running — and those differ exactly when it
	// matters, which is after somebody changed the model on one side.
	if response.Model == "" {
		return nil, "", errors.New(
			"ai-service returned no model name; storing a blank `embedding_model` " +
				"would make a future model change undetectable and unfixable")
	}

	return response.Embeddings, response.Model, nil
}
