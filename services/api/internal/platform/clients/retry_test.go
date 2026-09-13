package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// countingServer records how many times it was called and answers with a
// scripted sequence of statuses.
func countingServer(t *testing.T, statuses ...int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := int(calls.Add(1))
		status := statuses[len(statuses)-1]
		if n <= len(statuses) {
			status = statuses[n-1]
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"status":"ok","service":"astro","version":"0.1.0"}`))
	}))
	t.Cleanup(srv.Close)

	return srv, &calls
}

// TestRetriesTransientFailure: a 503 followed by a 200 must succeed.
// This is the restart/deploy case the retry exists for.
func TestRetriesTransientFailure(t *testing.T) {
	srv, calls := countingServer(t, http.StatusServiceUnavailable, http.StatusOK)

	astro, err := NewAstro(srv.URL, "token-long-enough", 5*time.Second)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}

	if err := astro.Health(context.Background()); err != nil {
		t.Fatalf("Health failed despite the second attempt succeeding: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("server called %d times, want 2 (one failure, one success)", got)
	}
}

func TestGivesUpAfterMaxAttempts(t *testing.T) {
	srv, calls := countingServer(t, http.StatusServiceUnavailable)

	astro, err := NewAstro(srv.URL, "token-long-enough", 5*time.Second)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}

	if err := astro.Health(context.Background()); err == nil {
		t.Fatal("Health succeeded against a permanently failing server")
	}
	if got := calls.Load(); got != maxAttempts {
		t.Errorf("server called %d times, want %d", got, maxAttempts)
	}
}

// TestDoesNotRetryClientErrors: re-sending a 400 just sends the same
// malformed request again, three times as slowly.
func TestDoesNotRetryClientErrors(t *testing.T) {
	for _, status := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
	} {
		srv, calls := countingServer(t, status)

		astro, err := NewAstro(srv.URL, "token-long-enough", 5*time.Second)
		if err != nil {
			t.Fatalf("NewAstro: %v", err)
		}
		_ = astro.Health(context.Background())

		if got := calls.Load(); got != 1 {
			t.Errorf("status %d: server called %d times, want 1 — 4xx must not be retried",
				status, got)
		}
	}
}

// TestRetriesRateLimit: 429 is transient, unlike other 4xx.
func TestRetriesRateLimit(t *testing.T) {
	srv, calls := countingServer(t, http.StatusTooManyRequests, http.StatusOK)

	astro, err := NewAstro(srv.URL, "token-long-enough", 5*time.Second)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}
	if err := astro.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("server called %d times, want 2 — 429 should be retried", got)
	}
}

// TestCancellationStopsRetrying: a caller who has given up must not have
// work continue on their behalf.
func TestCancellationStopsRetrying(t *testing.T) {
	srv, calls := countingServer(t, http.StatusServiceUnavailable)

	astro, err := NewAstro(srv.URL, "token-long-enough", 5*time.Second)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}

	// Long enough for the first attempt, too short for the backoff+retry.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_ = astro.Health(ctx)

	if got := calls.Load(); got > 1 {
		t.Errorf("server called %d times after the context expired; retries must stop", got)
	}
}

// TestTimeoutBoundsTheWholeCall: the caller's timeout is the budget for
// every attempt combined, not per attempt. Someone who asked for 200ms
// must not wait 600ms.
func TestTimeoutBoundsTheWholeCall(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer slow.Close()

	astro, err := NewAstro(slow.URL, "token-long-enough", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}

	start := time.Now()
	_ = astro.Health(context.Background())
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Errorf("call took %v with a 200ms timeout — the budget is being applied per attempt", elapsed)
	}
}

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	// Jitter makes this probabilistic, so assert on bounds rather than
	// exact values.
	for attempt := 1; attempt <= 6; attempt++ {
		d := backoff(attempt)
		if d <= 0 {
			t.Fatalf("attempt %d: non-positive backoff %v", attempt, d)
		}
		upper := time.Duration(float64(maxBackoff) * (1 + jitterFraction))
		if d > upper {
			t.Errorf("attempt %d: backoff %v exceeds the capped upper bound %v", attempt, d, upper)
		}
	}
}

func TestCanRetryOnlyIdempotentMethods(t *testing.T) {
	cases := map[string]bool{
		http.MethodGet:     true,
		http.MethodHead:    true,
		http.MethodOptions: true,
		http.MethodPost:    false,
		http.MethodPatch:   false,
		http.MethodDelete:  false,
		http.MethodPut:     false,
	}

	for method, want := range cases {
		req := httptest.NewRequest(method, "/", nil)
		if got := canRetry(req); got != want {
			t.Errorf("canRetry(%s) = %v, want %v", method, got, want)
		}
	}
}
