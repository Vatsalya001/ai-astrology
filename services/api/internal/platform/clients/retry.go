package clients

import (
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"
)

// Retry policy for calls to the internal Python services.
//
// Deliberately conservative. The services are on the same host or the
// same private network, so a failure is usually a restart or a deploy
// rather than congestion — two quick attempts recover from that, and
// more would just add latency to a request that is going to fail anyway.
const (
	maxAttempts    = 3 // 1 initial + 2 retries
	baseBackoff    = 50 * time.Millisecond
	maxBackoff     = 500 * time.Millisecond
	jitterFraction = 0.3
)

// retryTransport retries idempotent requests on transient failures.
//
// Implemented as a RoundTripper rather than as call-site logic so every
// generated client method gets it automatically. Retry policy applied
// per-call would be forgotten on exactly the method that needed it.
type retryTransport struct {
	base http.RoundTripper
	name string
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// The body must be replayable to retry at all. Requests built by
		// the generated clients carry a GetBody; anything without one is
		// attempted exactly once rather than silently sending a truncated
		// body on the second try.
		if attempt > 1 {
			if err := rewindBody(req); err != nil {
				return nil, err
			}
		}

		resp, err := t.base.RoundTrip(req)

		if err == nil && !shouldRetryStatus(resp.StatusCode) {
			return resp, nil
		}

		if err != nil {
			lastErr = err
		} else {
			lastErr = &statusError{code: resp.StatusCode}
			// Drain and close, or the connection cannot be reused and
			// the pool leaks one socket per retry.
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
		}

		// Never retry a request the caller has already abandoned.
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		if attempt == maxAttempts || !canRetry(req) {
			break
		}

		delay := backoff(attempt)
		slog.DebugContext(req.Context(), "retrying internal call",
			slog.String("service", t.name),
			slog.Int("attempt", attempt),
			slog.Duration("delay", delay),
			slog.Any("err", lastErr),
		)

		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(delay):
		}
	}

	return nil, lastErr
}

// shouldRetryStatus: 5xx and 429 are transient; 4xx are not.
//
// Retrying a 400 just sends the same malformed request again, three
// times as slowly.
func shouldRetryStatus(code int) bool {
	return code >= 500 || code == http.StatusTooManyRequests
}

// canRetry reports whether re-sending is safe.
//
// Only idempotent methods, and only when the body can be replayed. A
// POST is excluded even though every POST this service currently makes
// happens to be a pure computation — that will stop being true, and the
// failure mode (a silently duplicated write) is the kind nobody notices
// until reconciliation.
func canRetry(req *http.Request) bool {
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		return false
	}

	// An empty body may be represented as nil OR as http.NoBody, and the
	// two are not interchangeable. net/http and httptest both use
	// http.NoBody, so a nil-only check makes every bodyless GET
	// non-retryable — which silently disables retry for exactly the
	// calls this service actually makes.
	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}

func rewindBody(req *http.Request) error {
	if req.GetBody == nil {
		return nil
	}
	body, err := req.GetBody()
	if err != nil {
		return err
	}
	req.Body = body
	return nil
}

// backoff grows exponentially, capped, with jitter.
//
// Jitter matters even with two services: without it, every in-flight
// request retries at the same instant after a restart and the service
// is hit by a synchronised wave just as it comes up.
func backoff(attempt int) time.Duration {
	d := baseBackoff * time.Duration(1<<(attempt-1))
	if d > maxBackoff {
		d = maxBackoff
	}
	jitter := 1 + (rand.Float64()*2-1)*jitterFraction
	return time.Duration(float64(d) * jitter)
}

type statusError struct{ code int }

func (e *statusError) Error() string {
	return "upstream returned status " + http.StatusText(e.code)
}

var _ error = (*statusError)(nil)
