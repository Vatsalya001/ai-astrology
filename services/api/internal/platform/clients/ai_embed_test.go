package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestEmbedReturnsTheVectorsAndTheModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Texts []string `json:"texts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		embedResponse(w, len(body.Texts), "nomic-embed-text")
	}))
	t.Cleanup(server.Close)

	ai := mustEmbedClient(t, server.URL)

	response, err := ai.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Embeddings) != 3 {
		t.Errorf("got %d vectors", len(response.Embeddings))
	}
	// Stored in `embedding_model` on every chunk. Migration 000007's
	// comment says why: a corpus embedded across a model change is
	// unfixable without it.
	if response.Model != "nomic-embed-text" {
		t.Errorf("model is %q", response.Model)
	}
}

func TestEmbedRefusesAShortVectorList(t *testing.T) {
	// The dangerous failure, and the reason this is checked here as well
	// as in the route: the caller zips vectors against chunks BY POSITION,
	// so a short list attaches every embedding from that point on to the
	// wrong passage. The corpus then retrieves confidently and wrongly and
	// nothing anywhere errors.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		embedResponse(w, 2, "m") // asked for three
	}))
	t.Cleanup(server.Close)

	_, err := mustEmbedClient(t, server.URL).
		Embed(context.Background(), []string{"a", "b", "c"})
	if err == nil {
		t.Fatal("a short vector list was accepted")
	}
	if !errors.Is(err, ErrAIRejected) {
		t.Errorf("error is %v, want ErrAIRejected", err)
	}
}

func TestEmbedMapsStatusesToTheRightSentinel(t *testing.T) {
	// Same split as Complete and for the same reason: 5xx and 429 recover
	// and the ingestion job should retry; a 4xx will fail identically
	// forever, and retrying it turns one bad batch into several.
	cases := map[int]error{
		http.StatusServiceUnavailable:    ErrAIUnavailable,
		http.StatusInternalServerError:   ErrAIUnavailable,
		http.StatusTooManyRequests:       ErrAIUnavailable,
		http.StatusUnprocessableEntity:   ErrAIRejected,
		http.StatusUnauthorized:          ErrAIRejected,
		http.StatusRequestEntityTooLarge: ErrAIRejected,
	}

	for status, want := range cases {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(status)
				}))
			t.Cleanup(server.Close)

			_, err := mustEmbedClient(t, server.URL).
				Embed(context.Background(), []string{"a"})
			if !errors.Is(err, want) {
				t.Errorf("status %d gave %v, want %v", status, err, want)
			}
		})
	}
}

func TestEmbedSendsNoRequestForAnEmptyBatch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		embedResponse(w, 0, "m")
	}))
	t.Cleanup(server.Close)

	response, err := mustEmbedClient(t, server.URL).Embed(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Error("an empty batch reached the network; the route would 422 it, " +
			"which would read as a bug in the corpus rather than in the loop")
	}
	if response.Embeddings == nil {
		t.Error("an empty batch returned a nil slice rather than an empty one")
	}
}

func TestEmbedCarriesTheInternalToken(t *testing.T) {
	// `.claude/rules/security.md`: X-Internal-Token on every
	// service-to-service call. Asserted rather than assumed, because the
	// header is added by a request editor several frames away from here
	// and a new client built without it would 401 only at runtime.
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get(headerInternalToken)
		embedResponse(w, 1, "m")
	}))
	t.Cleanup(server.Close)

	if _, err := mustEmbedClient(t, server.URL).
		Embed(context.Background(), []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if seen != "token-long-enough" {
		t.Errorf("token header is %q", seen)
	}
}

func TestEmbedIsRetriedBecauseItIsIdempotent(t *testing.T) {
	// Unlike a completion, re-sending an embed request cannot produce a
	// different answer and cannot duplicate a write: embedding is a pure
	// function of the text, and ai-service holds `astro_ro`. So a replay
	// after a 503 is strictly better than failing a 600-document run —
	// which is what `MarkIdempotent` is for, and it is opt-in precisely so
	// this decision is visible at the call site.
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		embedResponse(w, 1, "m")
	}))
	t.Cleanup(server.Close)

	response, err := mustEmbedClient(t, server.URL).
		Embed(context.Background(), []string{"a"})
	if err != nil {
		t.Fatalf("a retryable 503 was not retried: %v", err)
	}
	if got := attempts.Load(); got < 2 {
		t.Errorf("%d attempt(s) — a POST is only replayed when marked idempotent", got)
	}
	if len(response.Embeddings) != 1 {
		t.Errorf("got %d vectors after the retry", len(response.Embeddings))
	}
}

func TestEmbedDoesNotHoldACompletionSlot(t *testing.T) {
	// `maxConcurrentCompletions` bounds concurrent SPEND on interactive
	// calls. A batch job holding those slots would make chat return 429 to
	// real users while the corpus loads — which is exactly backwards, and
	// invisible until it happens in production.
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		embedResponse(w, 1, "m")
	}))
	t.Cleanup(server.Close)

	ai := mustEmbedClient(t, server.URL)

	// Fill every completion slot, so a shared semaphore would block.
	for range MaxConcurrentCompletions() {
		if _, ok := ai.acquire(); !ok {
			t.Fatal("could not fill the completion slots")
		}
	}

	done := make(chan error, 1)
	go func() {
		_, err := ai.Embed(context.Background(), []string{"a"})
		done <- err
	}()

	// The handler is blocked, so reaching it is the proof: an Embed behind
	// the semaphore would have returned ErrAIRateLimited immediately
	// instead of getting this far.
	select {
	case err := <-done:
		t.Fatalf("Embed returned before the server responded: %v "+
			"(ErrAIRateLimited means it is behind the completion semaphore)", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(release)
	if err := <-done; err != nil {
		t.Errorf("Embed failed: %v", err)
	}
}

// TestAnEmbedIsNotCappedByTheCompletionTimeout is the Phase 5 half of
// TestACompletionIsNotCappedByTheGeneralTimeout.
//
// http.Client.Timeout bounds the whole call and a per-request context can
// only make a request finish SOONER. `embedTimeout` is 120s — longer than
// a completion's 90s, because sixty-four chunks through a cold local model
// genuinely takes that long — so sizing the transport from
// max(general, completion) would have capped every embed at 90s under a
// comment claiming it had two minutes.
//
// Scaled down so the test is fast, and sized from its own constants so the
// assertion survives them changing.
func TestAnEmbedIsNotCappedByTheCompletionTimeout(t *testing.T) {
	const (
		general    = 30 * time.Millisecond
		completion = 60 * time.Millisecond
		embed      = 600 * time.Millisecond
		serverWork = 250 * time.Millisecond
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(serverWork)
		embedResponse(w, 1, "m")
	}))
	t.Cleanup(server.Close)

	ai, err := newAI(server.URL, "token-long-enough", general, completion, embed)
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	_, err = ai.Embed(context.Background(), []string{"a"})
	elapsed := time.Since(started)

	if err != nil {
		t.Fatalf("a %v embed failed under a %v completion budget after %v: %v\n"+
			"The transport is sized from the wrong max; the embed budget is dead code.",
			serverWork, completion, elapsed, err)
	}
}

// TestTheEmbedBudgetStillBounds is the negative case.
//
// Without it, "size the transport from something enormous" would pass the
// test above and remove every bound — a hung provider would hold a
// connection until the ingestion process died.
//
// ── Why `general` is the LARGEST budget here ──
//
// Because otherwise this test proves that A bound exists rather than that
// the EMBED bound exists, and those are different claims. The transport is
// sized from max(general, completion, embed), so with embed as the largest
// value the transport and the per-call deadline are the same number and
// either one alone cuts the call. A mutation deleting the per-call deadline
// survived exactly that way.
//
// Making `general` the largest separates them: the transport allows 2s,
// the per-call embed deadline is 120ms, and only the latter can explain a
// request that fails after 120ms.
func TestTheEmbedBudgetStillBounds(t *testing.T) {
	const (
		general    = 2 * time.Second
		completion = 30 * time.Millisecond
		embed      = 120 * time.Millisecond
		serverWork = 800 * time.Millisecond
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(serverWork)
		embedResponse(w, 1, "m")
	}))
	t.Cleanup(server.Close)

	ai, err := newAI(server.URL, "token-long-enough", general, completion, embed)
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	_, err = ai.Embed(context.Background(), []string{"a"})
	elapsed := time.Since(started)

	if err == nil {
		t.Fatalf("a %v response was accepted under a %v embed budget after %v; "+
			"nothing is bounding the call but the %v transport",
			serverWork, embed, elapsed, general)
	}
	// Fired from the embed budget, not from the transport. Measured
	// rather than inferred: the call returns in ~121ms, and a failure
	// anywhere near 2s would mean the per-call deadline is gone and the
	// transport caught it instead.
	//
	// The margin is 4x rather than tight because this runs on CI machines
	// under load. It is still six times below the 2s transport, which is
	// the only other thing that could end this call.
	//
	// (The TEST takes ~800ms regardless: httptest's Close waits for the
	// in-flight handler to finish sleeping. That is not the Embed call,
	// and reading the test's own duration as the call's would make this
	// assertion look satisfied by anything.)
	if elapsed > 4*embed {
		t.Errorf("the call failed after %v, which is the %v transport rather "+
			"than the %v embed budget", elapsed, general, embed)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────

func mustEmbedClient(t *testing.T, url string) *AI {
	t.Helper()
	// Budgets short enough that a hanging test fails fast, and equal so
	// nothing here depends on which one the transport picked — the two
	// tests above are where that interaction is pinned.
	ai, err := newAI(url, "token-long-enough",
		2*time.Second, 2*time.Second, 2*time.Second)
	if err != nil {
		t.Fatalf("newAI: %v", err)
	}
	return ai
}

func embedResponse(w http.ResponseWriter, count int, model string) {
	vectors := make([][]float32, count)
	for index := range vectors {
		vectors[index] = []float32{0.1, 0.2, 0.3}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"embeddings":  vectors,
		"model":       model,
		"dimensions":  3,
		"provider_id": "test",
		"latency_ms":  1,
	})
}
