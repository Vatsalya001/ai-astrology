package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ── The ingestion pipeline ──
//
// PHASE-05 §4:
//
//	read authored markdown → chunk (Go, deterministic) → POST /v1/embed
//	(batched) → INSERT documents + chunks in one transaction
//
// This file is the last two arrows. Chunking is `ChunkDocument`; reading
// the files is `cmd/ingest-kb`, which owns the filesystem so this package
// stays pure enough to test without one.

// Embedder is the slice of ai-service this package needs.
//
// Declared HERE, by the consumer, rather than taken from the clients
// package — the Go boundary rule in `.claude/CLAUDE.md`. The practical
// payoff is in the test: an ingestion test substitutes a deterministic
// embedder and never touches the network, which is what lets a full
// pipeline test run in CI under the rule that CI never calls a model.
type Embedder interface {
	// Embed returns one vector per text, in the same order.
	Embed(ctx context.Context, texts []string) (vectors [][]float32, model string, err error)
}

// Store is the slice of the database this package needs.
//
// Also consumer-declared. Note what is NOT here: no generic Exec, no
// query builder. Ingestion can upsert a document, clear its chunks and
// insert chunks, and that is the whole of its authority over the schema.
type Store interface {
	// WithinTransaction runs fn against a transactional view of the store.
	// A non-nil return rolls back.
	WithinTransaction(ctx context.Context, fn func(tx Store) error) error

	// StoredChecksum reports what is already held for a document identity.
	// found is false when the identity is absent.
	StoredChecksum(ctx context.Context, identity Identity) (checksum string, chunks int, found bool, err error)

	// UpsertDocument writes the document and returns its id.
	UpsertDocument(ctx context.Context, doc *Document) (documentID string, err error)

	// DeleteChunks removes every chunk of a document.
	DeleteChunks(ctx context.Context, documentID string) error

	// InsertChunk writes one chunk. vector is pgvector text form.
	InsertChunk(ctx context.Context, documentID string, chunk Chunk, vector, model string) error
}

// Identity is what `kd_identity_idx` is built on.
type Identity struct {
	Title    string
	Language string
	Version  int
}

func (d *Document) Identity() Identity {
	return Identity{Title: d.Title, Language: d.Language, Version: d.Version}
}

// IngestOptions configures one run.
type IngestOptions struct {
	// BatchSize is how many chunks go to /v1/embed per request.
	// `KB_EMBED_BATCH_SIZE`, 64 by default.
	BatchSize int

	// Force re-embeds documents whose checksum already matches.
	//
	// The escape hatch for the one case the checksum cannot detect: the
	// CHUNKER changed, so the same bytes now produce different chunks. The
	// golden files in testdata are what make that change visible in
	// review; this flag is what acts on it.
	Force bool

	// Progress, if set, is called once per document. For a CLI spinner —
	// a 600-document run with a cold model takes minutes and silence
	// during it is indistinguishable from a hang.
	Progress func(result DocumentResult)
}

// DocumentResult is what happened to one document.
type DocumentResult struct {
	Path     string
	Identity Identity
	Chunks   int

	// Skipped is true when the stored checksum matched and Force was off.
	Skipped bool
}

// IngestReport is what happened to the run.
type IngestReport struct {
	Documents  int
	Ingested   int
	Skipped    int
	Chunks     int
	Embeddings int
	Model      string
}

// ErrEmbeddingModelChanged means one run saw vectors from two models.
//
// Fatal rather than a warning. Vectors from different models are not
// comparable — cosine similarity between them is a number with no
// meaning — so a corpus holding both ranks arbitrarily, and nothing about
// the result looks wrong. Migration 000007 stores `embedding_model` per
// chunk precisely so this is detectable; this is the detection.
var ErrEmbeddingModelChanged = errors.New("knowledge: embedding model changed mid-run")

// Ingest embeds and stores a chunked corpus.
//
// ── Why embedding happens OUTSIDE the transaction ──
//
// Each document gets its own transaction, and the HTTP call to
// /v1/embed is made before it opens. Embedding a batch takes seconds —
// tens of them on a cold local model — and a transaction held open
// across a network round trip holds its locks for the same duration. Over
// 600 documents that is a transaction open for most of the run, which
// blocks `ALTER TABLE`, holds back vacuum, and turns one slow provider
// into a database incident.
//
// ── Why one transaction per document, not one for the corpus ──
//
// §4 requires documents and chunks to go in together, and that is what
// matters: `kc_unembedded_idx` exists to find chunks without vectors, and
// a document visible without its chunks is a document retrieval cannot
// reach but the corpus count includes. A per-document transaction
// guarantees that.
//
// Wrapping the whole corpus in one transaction would add all-or-nothing
// across documents, and that is worth less than it costs here: ingestion
// is idempotent, so a run that dies halfway is fixed by running it again,
// and in exchange a per-document transaction keeps locks short and makes
// the run resumable.
func Ingest(
	ctx context.Context,
	documents []ChunkedDocument,
	embedder Embedder,
	store Store,
	opts IngestOptions,
) (*IngestReport, error) {
	if opts.BatchSize < 1 {
		return nil, fmt.Errorf("batch size is %d, must be at least 1", opts.BatchSize)
	}

	report := &IngestReport{Documents: len(documents)}

	for _, document := range documents {
		result, err := ingestOne(ctx, document, embedder, store, opts, report)
		if err != nil {
			// The path is in the error so a failure at document 412 of 600
			// names a file. Partial progress is kept, not rolled back: the
			// documents already committed are correct and re-running skips
			// them.
			return report, fmt.Errorf("%s: %w", document.Document.Path, err)
		}

		if result.Skipped {
			report.Skipped++
		} else {
			report.Ingested++
			report.Chunks += result.Chunks
		}

		if opts.Progress != nil {
			opts.Progress(result)
		}
	}

	return report, nil
}

// ChunkedDocument is a document and its chunks, as produced by
// ChunkDocument.
type ChunkedDocument struct {
	Document *Document
	Chunks   []Chunk
}

func ingestOne(
	ctx context.Context,
	document ChunkedDocument,
	embedder Embedder,
	store Store,
	opts IngestOptions,
	report *IngestReport,
) (DocumentResult, error) {
	doc := document.Document
	result := DocumentResult{
		Path:     doc.Path,
		Identity: doc.Identity(),
		Chunks:   len(document.Chunks),
	}

	if !opts.Force {
		checksum, storedChunks, found, err := store.StoredChecksum(ctx, doc.Identity())
		if err != nil {
			return result, fmt.Errorf("read stored checksum: %w", err)
		}
		// All three conditions, not just the checksum. A matching checksum
		// with zero chunks is a previous run that died between the
		// document insert and the chunk inserts, and skipping that leaves
		// a document in the corpus with nothing retrievable in it — which
		// the §16 document count would report as present.
		if found && checksum == doc.Checksum && storedChunks > 0 {
			result.Skipped = true
			return result, nil
		}
	}

	texts := make([]string, len(document.Chunks))
	for index, chunk := range document.Chunks {
		texts[index] = chunk.Content
	}

	vectors, model, err := embedBatched(ctx, embedder, texts, opts.BatchSize)
	if err != nil {
		return result, err
	}

	if report.Model == "" {
		report.Model = model
	} else if report.Model != model {
		return result, fmt.Errorf("%w: %q then %q. Cosine similarity between "+
			"vectors from different models is a number with no meaning, so a corpus "+
			"holding both ranks arbitrarily. Re-ingest with --force",
			ErrEmbeddingModelChanged, report.Model, model)
	}
	report.Embeddings += len(vectors)

	err = store.WithinTransaction(ctx, func(tx Store) error {
		documentID, err := tx.UpsertDocument(ctx, doc)
		if err != nil {
			return fmt.Errorf("upsert document: %w", err)
		}

		// Replace, never merge. Re-chunking can produce a different NUMBER
		// of chunks, and an upsert keyed on (document_id, chunk_index)
		// would leave the previous run's tail behind: orphan chunks at
		// indexes this run never reached, still matching keyword search,
		// still being retrieved, and the only chunks in the corpus whose
		// text no authored file contains.
		if err := tx.DeleteChunks(ctx, documentID); err != nil {
			return fmt.Errorf("clear old chunks: %w", err)
		}

		for index, chunk := range document.Chunks {
			if err := tx.InsertChunk(
				ctx, documentID, chunk, FormatVector(vectors[index]), model,
			); err != nil {
				return fmt.Errorf("insert chunk %d: %w", chunk.Index, err)
			}
		}

		return nil
	})
	if err != nil {
		return result, err
	}

	return result, nil
}

// embedBatched sends the texts in batches and returns the vectors in
// order.
//
// Batched because a 600-document corpus is a few thousand chunks, and one
// request each is both slow and the fastest way to get rate-limited by a
// hosted provider. Reassembled in order because the caller zips vectors
// against chunks by position.
func embedBatched(
	ctx context.Context, embedder Embedder, texts []string, batchSize int,
) ([][]float32, string, error) {
	vectors := make([][]float32, 0, len(texts))
	model := ""

	for start := 0; start < len(texts); start += batchSize {
		end := min(start+batchSize, len(texts))

		batch, batchModel, err := embedder.Embed(ctx, texts[start:end])
		if err != nil {
			return nil, "", fmt.Errorf("embed chunks %d-%d: %w", start, end-1, err)
		}

		// The alignment check again, at the last place that can still tell
		// which batch was short. The client checks it too; both are cheap
		// and the failure is silent misalignment of every vector from this
		// point on.
		if len(batch) != end-start {
			return nil, "", fmt.Errorf(
				"embed chunks %d-%d: got %d vectors for %d texts",
				start, end-1, len(batch), end-start)
		}

		if model == "" {
			model = batchModel
		} else if model != batchModel {
			return nil, "", fmt.Errorf("%w: within one document, %q then %q",
				ErrEmbeddingModelChanged, model, batchModel)
		}

		vectors = append(vectors, batch...)
	}

	if model == "" {
		// Only reachable with no texts at all, which ChunkDocument refuses
		// to produce. Named rather than returning an empty model that
		// would land in `embedding_model` and violate kc_model_not_blank
		// from inside the transaction.
		return nil, "", errors.New("no chunks to embed")
	}

	return vectors, model, nil
}

// FormatVector renders a vector in pgvector's text input form.
//
// `[0.1,0.2,…]`, which is what `::vector` parses. See the header of
// db/queries/knowledge.sql for why the vector crosses into Postgres as
// text rather than through a pgvector Go type.
//
// Formatted with 'g' and -1 precision: the shortest decimal that
// round-trips back to the same float32. Choosing a fixed precision
// instead would either lose bits of every value in the corpus or pad
// 768 floats per chunk with digits that carry nothing.
func FormatVector(vector []float32) string {
	var out strings.Builder
	// 12 bytes per value is about right for a normalised embedding and
	// saves a dozen reallocations per chunk.
	out.Grow(len(vector)*12 + 2)

	out.WriteByte('[')
	for index, value := range vector {
		if index > 0 {
			out.WriteByte(',')
		}
		out.WriteString(strconv.FormatFloat(float64(value), 'g', -1, 32))
	}
	out.WriteByte(']')

	return out.String()
}
