package clients

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients/aiclient"
)

// ─── the breaker on its own ──────────────────────────────────────────

type stubTransport func(*http.Request) (*http.Response, error)

func (s stubTransport) RoundTrip(req *http.Request) (*http.Response, error) { return s(req) }

func testBreaker(base stubTransport) *circuitBreaker {
	// A discarding logger: these tests trip the breaker deliberately and
	// the Error line it logs is correct behaviour, not a test failure.
	return newCircuitBreaker(base, "test", slog.New(slog.DiscardHandler))
}

func breakerRequest(t *testing.T, ctx context.Context) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://service.invalid/health", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	return req
}

func status(code int) *http.Response {
	return &http.Response{StatusCode: code, Body: http.NoBody, Header: make(http.Header)}
}

// TestAnAbandonedProbeDoesNotWedgeTheBreaker guards the bookkeeping that
// the caller-cancellation exemption could most easily break.
//
// Half-open refuses every request while one probe is outstanding. If an
// abandoned probe returned without clearing that flag — an easy thing to
// skip, since it records no failure and no success — the breaker would
// sit in half-open for ever, refusing everything, and no cooldown would
// ever end it. A cancelled request would have become a permanent outage.
func TestAnAbandonedProbeDoesNotWedgeTheBreaker(t *testing.T) {
	var healthy atomic.Bool
	br := testBreaker(func(req *http.Request) (*http.Response, error) {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		if healthy.Load() {
			return status(http.StatusOK), nil
		}
		return status(http.StatusInternalServerError), nil
	})
	// Shortened so the test does not sit out the real 30s cooldown. The
	// cooldown length is not what is under test here; the probe slot is.
	br.cooldown = time.Millisecond

	for range defaultThreshold {
		_, _ = br.RoundTrip(breakerRequest(t, context.Background()))
	}
	if br.currentState() != open {
		t.Fatalf("setup: state = %v, want open", br.currentState())
	}
	time.Sleep(5 * time.Millisecond)

	// The probe that the cooldown admits is abandoned by its caller.
	caller, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := br.RoundTrip(breakerRequest(t, withCallerContext(caller))); err == nil {
		t.Fatal("the abandoned probe should have returned the context error")
	}

	// Recovery must still be reachable.
	healthy.Store(true)
	if _, err := br.RoundTrip(breakerRequest(t, context.Background())); err != nil {
		t.Fatalf("the breaker is wedged: %v", err)
	}
	if br.currentState() != closed {
		t.Errorf("state = %v after a successful probe, want closed", br.currentState())
	}
}

// ─── the two directions, through the real client stack ───────────────

// hangingService holds the connection open until it is told to answer,
// which is what a wedged Python service looks like from here.
type hangingService struct {
	server  *httptest.Server
	healthy atomic.Bool
	calls   atomic.Int64
}

func newHangingService(t *testing.T, body string) *hangingService {
	t.Helper()
	s := &hangingService{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.calls.Add(1)
		if s.healthy.Load() {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
			return
		}
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

const okHealthBody = `{"status":"ok","service":"astro","version":"0.1.0"}`

// TestRequestsTheCallerAbandonedDoNotTripTheBreaker is the bug.
//
// A user closing a tab, or an HTTP handler upstream hitting its own
// deadline, cancels the context. That arrived at the breaker as a
// transport error and was counted, so five abandoned requests took a
// perfectly healthy astro-service out for everybody for a cooldown —
// and the sixth user, who had waited patiently, got the cached-chart
// fallback for no reason.
//
// Our own budget is deliberately generous here (5s against a 40ms
// caller) so that the only clock that runs out is the caller's.
func TestRequestsTheCallerAbandonedDoNotTripTheBreaker(t *testing.T) {
	service := newHangingService(t, okHealthBody)

	astro, err := NewAstro(service.server.URL, "token-long-enough", 5*time.Second)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}

	for i := range defaultThreshold + 1 {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		err := astro.Health(ctx)
		cancel()
		if err == nil {
			t.Fatalf("call %d: the service never answered, Health must fail", i)
		}
	}

	// The service was healthy the whole time — it was the callers who
	// went away. The next user must not pay for that.
	service.healthy.Store(true)
	if err := astro.Health(context.Background()); err != nil {
		t.Fatalf("%d abandoned requests opened the circuit against a healthy service: %v",
			defaultThreshold+1, err)
	}
}

// TestAServiceThatHangsThroughOurBudgetStillTripsTheBreaker is the
// negative case for the test above, and the reason the fix could not
// simply be "ignore every context error".
//
// A hung service is the single worst failure the breaker exists for:
// every request holds a goroutine for the full timeout. It reaches the
// breaker as exactly the same context.DeadlineExceeded, on exactly the
// same expired context, as the abandoned requests above. Only the owner
// of the clock differs — here the caller waits for ever and OUR 40ms
// transport budget is what expires.
func TestAServiceThatHangsThroughOurBudgetStillTripsTheBreaker(t *testing.T) {
	service := newHangingService(t, okHealthBody)

	astro, err := NewAstro(service.server.URL, "token-long-enough", 40*time.Millisecond)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}

	for i := range defaultThreshold {
		// context.Background(): this caller never gives up.
		if err := astro.Health(context.Background()); err == nil {
			t.Fatalf("call %d: the service never answered, Health must fail", i)
		}
	}

	err = astro.Health(context.Background())
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("after %d timeouts against a hung service the circuit is still closed "+
			"(got %v); every request now waits out the full budget holding a goroutine",
			defaultThreshold, err)
	}
	if got := service.calls.Load(); got != defaultThreshold {
		t.Errorf("%d requests reached the hung service, want %d", got, defaultThreshold)
	}
}

// ─── the same two directions on the completion path ──────────────────

// TestACompletionThatExhaustsOurBudgetTripsTheBreaker pins the ordering
// inside Complete.
//
// Complete layers a 90s budget of its own on the caller's context. If
// the caller boundary were marked AFTER that instead of before, our own
// budget would look like the caller's patience, and an ai-service hung
// for 90s a request at a time would never open the circuit — the exact
// outage the breaker is for, made invisible by the fix for the other
// direction.
func TestACompletionThatExhaustsOurBudgetTripsTheBreaker(t *testing.T) {
	service := newHangingService(t, `{"result":{"text":"x"},"telemetry":{"trace_id":"t","job_type":"chat_response"}}`)

	ai, err := newAI(service.server.URL, "token-long-enough", 40*time.Millisecond, 40*time.Millisecond)
	if err != nil {
		t.Fatalf("newAI: %v", err)
	}

	for range defaultThreshold {
		_, _ = ai.Complete(context.Background(), aiclient.CompleteRequest{Message: "x"})
	}

	_, err = ai.Complete(context.Background(), aiclient.CompleteRequest{Message: "x"})
	if !errors.Is(err, ErrAIUnavailable) {
		t.Fatalf("want ErrAIUnavailable, got %v", err)
	}
	// Complete reports a tripped breaker as plain unavailability, so the
	// observable difference is that the request never left the process.
	if got := service.calls.Load(); got != defaultThreshold {
		t.Errorf("%d completions reached the hung service, want %d — the circuit "+
			"never opened and every caller is still waiting out the budget",
			got, defaultThreshold)
	}
}

func TestAbandonedCompletionsDoNotTripTheBreaker(t *testing.T) {
	// The costly direction of the same bug: an admin watching the panel
	// abandon six requests must not make the next paying user's
	// completion fail against a provider that is answering fine.
	service := newHangingService(t, `{"result":{"text":"Saturn is traditionally read as patience."},"telemetry":{"trace_id":"t","job_type":"chat_response"}}`)

	ai, err := newAI(service.server.URL, "token-long-enough", 5*time.Second, 5*time.Second)
	if err != nil {
		t.Fatalf("newAI: %v", err)
	}

	for i := range defaultThreshold + 1 {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		_, err := ai.Complete(ctx, aiclient.CompleteRequest{Message: "x"})
		cancel()
		if err == nil {
			t.Fatalf("call %d: the service never answered, Complete must fail", i)
		}
	}

	service.healthy.Store(true)
	envelope, err := ai.Complete(context.Background(), aiclient.CompleteRequest{Message: "x"})
	if err != nil {
		t.Fatalf("%d abandoned completions opened the circuit against a healthy "+
			"ai-service: %v", defaultThreshold+1, err)
	}
	if envelope.Result.Text == "" {
		t.Error("the completion returned an empty result")
	}
}

// ─── the choke point itself ──────────────────────────────────────────

func TestTheInjectorMarksTheCallerBoundary(t *testing.T) {
	// Without this, classify() falls back to counting every context
	// error and the exemption never fires in production.
	req := breakerRequest(t, context.Background())

	if err := headerInjector("token-long-enough")(req.Context(), req); err != nil {
		t.Fatalf("headerInjector: %v", err)
	}
	if !hasCallerContext(req.Context()) {
		t.Error("no caller boundary on an outbound request")
	}
}

func TestTheInjectorDoesNotOverwriteAnExistingBoundary(t *testing.T) {
	// The negative case, and the one that costs money if it is wrong.
	// Complete marks the boundary before layering its 90s budget on top.
	// If the injector then re-marked the request, our own expired budget
	// would be indistinguishable from a caller who had walked away, and
	// an ai-service hung for 90s a request at a time would never open
	// the circuit.
	//
	// The caller here is context.Background(): it never gives up. The
	// cancellation below stands in for OUR budget expiring.
	ours, spendOurBudget := context.WithCancel(withCallerContext(context.Background()))
	spendOurBudget()

	req := breakerRequest(t, ours)
	if err := headerInjector("token-long-enough")(req.Context(), req); err != nil {
		t.Fatalf("headerInjector: %v", err)
	}

	if callerAbandoned(req.Context()) {
		t.Error("our own expired budget was relabelled as the caller's; a hung " +
			"service will never trip the breaker")
	}
}
