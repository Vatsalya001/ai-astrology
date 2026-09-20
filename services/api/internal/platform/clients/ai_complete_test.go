package clients

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients/aiclient"
)

func aiStub(t *testing.T, handler http.HandlerFunc) *AI {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	ai, err := NewAI(server.URL, "token-long-enough", 5*time.Second)
	if err != nil {
		t.Fatalf("NewAI: %v", err)
	}
	return ai
}

func okEnvelope(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"result":    map[string]any{"text": "Saturn is traditionally read as patience."},
		"telemetry": map[string]any{"trace_id": "t-1", "job_type": "chat_response"},
	})
}

func TestCompleteReturnsTheWholeEnvelope(t *testing.T) {
	// The telemetry must come back with the result, not separately.
	// Splitting them is how a request that cost money ends up unbilled.
	ai := aiStub(t, okEnvelope)

	envelope, err := ai.Complete(context.Background(), aiclient.CompleteRequest{Message: "hello"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if envelope.Telemetry.TraceId != "t-1" {
		t.Errorf("telemetry did not survive: %+v", envelope.Telemetry)
	}
	if envelope.Result.Text == "" {
		t.Error("result text was dropped")
	}
}

// ─── the local rate limit ────────────────────────────────────────────

// TestConcurrentCompletionsAreBounded is the cost guard.
//
// PHASE-04 §14 lists rate limiting on the internal completion path as a
// security item and says why: "a runaway loop is a real cost event". The
// provider's own 429 arrives after the money is spent; this one arrives
// before.
func TestConcurrentCompletionsAreBounded(t *testing.T) {
	var peak, current int64
	release := make(chan struct{})

	ai := aiStub(t, func(w http.ResponseWriter, r *http.Request) {
		now := atomic.AddInt64(&current, 1)
		for {
			was := atomic.LoadInt64(&peak)
			if now <= was || atomic.CompareAndSwapInt64(&peak, was, now) {
				break
			}
		}
		<-release
		atomic.AddInt64(&current, -1)
		okEnvelope(w, r)
	})

	// Twice the bound, so the limiter has to turn some away.
	attempts := maxConcurrentCompletions * 2
	var wg sync.WaitGroup
	var limited int64

	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ai.Complete(context.Background(), aiclient.CompleteRequest{Message: "x"})
			if errors.Is(err, ErrAIRateLimited) {
				atomic.AddInt64(&limited, 1)
			}
		}()
	}

	// Let the in-flight calls pile up before releasing them, so the peak
	// is real rather than an artefact of scheduling.
	time.Sleep(150 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := atomic.LoadInt64(&peak); got > maxConcurrentCompletions {
		t.Errorf("%d completions ran concurrently, bound is %d", got, maxConcurrentCompletions)
	}
	if atomic.LoadInt64(&limited) == 0 {
		t.Error("no request was rate limited; the bound was never reached and this proves nothing")
	}
}

func TestSlotsAreReleasedAfterACall(t *testing.T) {
	// The negative case for the test above. A limiter that never
	// released would pass it and then refuse every request forever after
	// the eighth — an outage that looks like a rate limit.
	ai := aiStub(t, okEnvelope)

	for i := range maxConcurrentCompletions * 3 {
		if _, err := ai.Complete(context.Background(), aiclient.CompleteRequest{Message: "x"}); err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
	}

	if n := ai.InFlight(); n != 0 {
		t.Errorf("%d slots still held after every call returned", n)
	}
}

func TestSlotsAreReleasedAfterAFailure(t *testing.T) {
	// The leak that matters most: a slot held by a failed call is never
	// returned, so an outage permanently reduces capacity even after the
	// service recovers.
	ai := aiStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	for range maxConcurrentCompletions + 2 {
		_, _ = ai.Complete(context.Background(), aiclient.CompleteRequest{Message: "x"})
	}

	if n := ai.InFlight(); n != 0 {
		t.Errorf("%d slots leaked on the failure path", n)
	}
}

// TestAFailedCompletionIsNotReplayed is the most expensive silent bug
// available in this file.
//
// The retry transport replays anything marked idempotent. A chart
// computation is safe to replay — astro-service has no database, so
// there is no write to duplicate. A completion is NOT: replaying it
// spends money again, at the provider, and may return a different
// answer. `MarkIdempotent` is opt-in per call, and the bug is a single
// line added by someone copying the astro client.
//
// This test exists because break-testing found nothing caught it: adding
// `ctx = MarkIdempotent(ctx)` to Complete left the whole suite green
// while turning every 5xx into three paid calls.
func TestAFailedCompletionIsNotReplayed(t *testing.T) {
	var calls int64
	ai := aiStub(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&calls, 1)
		// 503 is the status the retry transport WOULD retry, which is
		// what makes this the right probe: a status it ignores anyway
		// would pass whether or not the request was marked idempotent.
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	_, err := ai.Complete(context.Background(), aiclient.CompleteRequest{Message: "x"})

	if !errors.Is(err, ErrAIUnavailable) {
		t.Fatalf("want ErrAIUnavailable, got %v", err)
	}
	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Errorf("the completion was sent %d times; a replayed completion is paid for again", got)
	}
}

// ─── error classification ────────────────────────────────────────────

func TestStatusesMapToTheRightSentinel(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{http.StatusInternalServerError, ErrAIUnavailable},
		{http.StatusBadGateway, ErrAIUnavailable},
		{http.StatusServiceUnavailable, ErrAIUnavailable},
		// 429 from ai-service means its own provider chain is throttled,
		// which recovers — so unavailable rather than rejected.
		{http.StatusTooManyRequests, ErrAIUnavailable},
		// These are our bug and will fail identically forever.
		{http.StatusBadRequest, ErrAIRejected},
		{http.StatusUnauthorized, ErrAIRejected},
		{http.StatusUnprocessableEntity, ErrAIRejected},
	}

	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			ai := aiStub(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			})

			_, err := ai.Complete(context.Background(), aiclient.CompleteRequest{Message: "x"})

			if !errors.Is(err, tc.want) {
				t.Errorf("status %d: want %v, got %v", tc.status, tc.want, err)
			}
		})
	}
}

func TestTheErrorCarriesNoResponseBody(t *testing.T) {
	// An error body can echo the request. .claude/rules/security.md keeps
	// request content out of error strings, and a message built from the
	// status code alone cannot leak one however the shape changes.
	ai := aiStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"message was: my birth time is 04:35 in Prayagraj"}`))
	})

	_, err := ai.Complete(context.Background(), aiclient.CompleteRequest{Message: "x"})

	if err == nil {
		t.Fatal("want an error")
	}
	if got := err.Error(); containsAny(got, "Prayagraj", "04:35", "birth time") {
		t.Errorf("the error echoed the request body: %q", got)
	}
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		for i := 0; i+len(n) <= len(haystack); i++ {
			if haystack[i:i+len(n)] == n {
				return true
			}
		}
	}
	return false
}

// TestACompletionIsNotCappedByTheGeneralTimeout pins the interaction
// between two budgets that looked independent and were not.
//
// http.Client.Timeout bounds the whole call and a per-request context
// can only ever make a request finish SOONER. The AI client was built
// with the general 10s ServiceTimeout, so the 90s context Complete set
// was dead code: every completion was capped at 10s, under a comment
// claiming it had a minute and a half. A deep-tier interpretation that
// legitimately takes thirty seconds failed as a timeout.
//
// Scaled down so the test is fast: a 50ms transport budget against a
// 400ms completion budget, with a handler that sleeps 200ms. Sized from
// the constants means the assertion survives them changing.
func TestACompletionIsNotCappedByTheGeneralTimeout(t *testing.T) {
	const (
		general    = 50 * time.Millisecond
		completion = 400 * time.Millisecond
		serverWork = 200 * time.Millisecond
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(serverWork)
		okEnvelope(w, r)
	}))
	t.Cleanup(server.Close)

	ai, err := newAI(server.URL, "token-long-enough", general, completion)
	if err != nil {
		t.Fatalf("newAI: %v", err)
	}

	started := time.Now()
	envelope, err := ai.Complete(context.Background(), aiclient.CompleteRequest{Message: "x"})
	elapsed := time.Since(started)

	if err != nil {
		t.Fatalf("a %v completion failed under a %v general timeout after %v: %v\n"+
			"The transport budget is capping the call; the completion budget is "+
			"dead code.", serverWork, general, elapsed, err)
	}
	if envelope.Result.Text == "" {
		t.Error("the completion returned an empty result")
	}
}

// TestTheCompletionBudgetStillBounds is the negative case.
//
// Without it, "make the transport budget enormous" would pass the test
// above and remove every bound — a hung provider would hold a
// connection until the process died.
func TestTheCompletionBudgetStillBounds(t *testing.T) {
	const (
		general    = 50 * time.Millisecond
		completion = 120 * time.Millisecond
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		okEnvelope(w, r)
	}))
	t.Cleanup(server.Close)

	ai, err := newAI(server.URL, "token-long-enough", general, completion)
	if err != nil {
		t.Fatalf("newAI: %v", err)
	}

	started := time.Now()
	_, err = ai.Complete(context.Background(), aiclient.CompleteRequest{Message: "x"})
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("a 2s response was accepted under a 120ms completion budget")
	}
	if elapsed > time.Second {
		t.Errorf("the completion budget did not bound the call: took %v", elapsed)
	}
}

// TestTheRealClientUsesTheCompletionBudget guards the wiring itself.
//
// The two tests above both call newAI directly. NewAI — the constructor
// cmd/api actually uses — could stop passing completionTimeout and they
// would both still pass.
func TestTheRealClientUsesTheCompletionBudget(t *testing.T) {
	ai, err := NewAI("http://127.0.0.1:1", "token-long-enough", 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewAI: %v", err)
	}

	if ai.completionTimeout != completionTimeout {
		t.Errorf("NewAI set completionTimeout=%v, want %v — production completions "+
			"are capped by the general service timeout", ai.completionTimeout, completionTimeout)
	}
}
