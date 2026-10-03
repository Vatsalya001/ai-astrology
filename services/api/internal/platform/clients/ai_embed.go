package clients

import (
	"context"
	"fmt"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients/aiclient"
)

// embedTimeout is the budget for one batch of embeddings.
//
// Generous, and for a different reason from the completion budget. A
// completion is interactive and a slow one is a user staring at a
// spinner; an embed batch is an ingestion job nobody is waiting on, and
// sixty-four chunks through a local model on a cold cache genuinely takes
// tens of seconds. Short enough that a hung provider does not stall a
// 600-document run indefinitely.
const embedTimeout = 120 * time.Second

// Embed turns a batch of texts into vectors.
//
// ── Why this is not behind the completion semaphore ──
//
// `maxConcurrentCompletions` bounds concurrent SPEND on interactive model
// calls. Ingestion is neither: it runs on a local embedding model at zero
// cost (PHASE-05 task 5.4, "full corpus ingested at zero cost"), it is
// invoked by an operator rather than by a user, and `ingest-kb` is
// single-threaded by design. Queueing it behind eight in-flight chat
// completions would make a corpus rebuild wait on unrelated traffic — and
// more importantly, a batch job holding completion slots would make chat
// return 429 to real users while the corpus loads.
//
// ── Why this IS marked idempotent ──
//
// Unlike a completion, re-sending an embed request cannot produce a
// different answer and cannot duplicate a write: embedding is a pure
// function of the input text, and ai-service holds `astro_ro`, so it has
// no write path to duplicate. A replay after a lost response is therefore
// strictly better than failing the batch — which is the whole reason
// `MarkIdempotent` exists as an opt-in rather than being inferred.
func (a *AI) Embed(ctx context.Context, texts []string) (*aiclient.EmbedResponse, error) {
	if len(texts) == 0 {
		// An empty batch is a valid thing for a caller to hold and a
		// request for zero embeddings is not. Returning early beats
		// sending the service an empty array and interpreting whatever it
		// says about it — and the route would 422 on it anyway, which
		// would read as a bug in the corpus rather than in the loop.
		return &aiclient.EmbedResponse{Embeddings: [][]float32{}}, nil
	}

	budget := a.embedTimeout
	if budget <= 0 {
		budget = embedTimeout
	}

	ctx, cancel := context.WithTimeout(
		MarkIdempotent(withCallerContext(ctx)), budget)
	defer cancel()

	resp, err := a.api.EmbedV1EmbedPostWithResponse(ctx, aiclient.EmbedRequest{Texts: texts})
	if err != nil {
		return nil, fmt.Errorf("%w: embed: %v", ErrAIUnavailable, err)
	}
	if resp.JSON200 == nil {
		return nil, aiStatusFailure(resp.StatusCode())
	}

	// Checked here as well as in the route, because the two checks protect
	// against different things. The route's check catches a provider that
	// truncated a batch. This one catches a client, a proxy or a future
	// contract change that reshaped the response — and the consequence is
	// the same either way and is the worst kind: the caller zips vectors
	// against chunks BY POSITION, so a short list silently attaches every
	// embedding from that point on to the wrong passage. The corpus then
	// retrieves confidently and wrongly, and nothing anywhere errors.
	if got := len(resp.JSON200.Embeddings); got != len(texts) {
		return nil, fmt.Errorf(
			"%w: ai-service returned %d vectors for %d texts; zipping those by "+
				"position would attach every embedding to the wrong text",
			ErrAIRejected, got, len(texts))
	}

	return resp.JSON200, nil
}
