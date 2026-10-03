package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
	"github.com/go-playground/validator/v10"
)

// KnowledgeBase is the ingestion configuration from PHASE-05 §9.
//
// Its own struct, and loadable on its own, for one reason: `cmd/ingest-kb`
// needs these three values and nothing else. Going through Config would
// make a chunking dry-run demand a JWT secret, an S3 endpoint and a Redis
// URL — and a tool that cannot run without production-shaped secrets is a
// tool people run with made-up ones.
//
// Embedded into Config as well, so the service and the command read one
// definition. Two copies of `envDefault:"350"` is a chunk size that differs
// between the process that writes the corpus and the process that reasons
// about it, which is undetectable from either side.
type KnowledgeBase struct {
	// ChunkSizeTokens is the target window. PHASE-05 §4 and §9: 350.
	//
	// Bounded above because the window has to fit the embedding model's
	// context — `nomic-embed-text` accepts 2048 — and below because an
	// overlap must fit inside it.
	ChunkSizeTokens int `env:"KB_CHUNK_SIZE_TOKENS" envDefault:"350" validate:"required,min=32,max=2000"`

	// ChunkOverlapTokens is how much of the previous chunk's tail each
	// chunk repeats. §9: 50.
	ChunkOverlapTokens int `env:"KB_CHUNK_OVERLAP_TOKENS" envDefault:"50" validate:"min=0,max=1000"`

	// EmbedBatchSize is how many chunks go to `/v1/embed` per request.
	// §9: 64. `MAX_BATCH` on the Python side is 256, deliberately above
	// this so the configured batch cannot trip it.
	EmbedBatchSize int `env:"KB_EMBED_BATCH_SIZE" envDefault:"64" validate:"required,min=1,max=256"`
}

// LoadKnowledgeBase parses just the ingestion settings.
func LoadKnowledgeBase() (*KnowledgeBase, error) {
	var cfg KnowledgeBase

	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("parse environment: %w", err)
	}
	if err := validator.New().Struct(&cfg); err != nil {
		return nil, fmt.Errorf("validate knowledge base configuration: %w", err)
	}
	if err := cfg.check(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// check catches the cross-field condition the `validate` tags cannot
// express.
func (k *KnowledgeBase) check() error {
	// An overlap at or above the window size makes every window restart
	// where the previous one started. `knowledge.ChunkConfig` refuses it
	// too, but refusing it here means the failure names the environment
	// variable rather than surfacing from inside the chunker.
	if k.ChunkOverlapTokens >= k.ChunkSizeTokens {
		return fmt.Errorf(
			"KB_CHUNK_OVERLAP_TOKENS (%d) must be smaller than KB_CHUNK_SIZE_TOKENS (%d)",
			k.ChunkOverlapTokens, k.ChunkSizeTokens,
		)
	}
	return nil
}
