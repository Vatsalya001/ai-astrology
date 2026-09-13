package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func okProbe(context.Context) error  { return nil }
func badProbe(context.Context) error { return errors.New("dial tcp: connection refused") }

func doHealth(t *testing.T, probers []Prober) (int, HealthResponse) {
	t.Helper()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	HealthHandler("api", "test", probers)(rec, req)

	var body HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, rec.Body.String())
	}
	return rec.Code, body
}

func TestHealthAllOK(t *testing.T) {
	code, body := doHealth(t, []Prober{
		{Name: "postgres", Critical: true, Probe: okProbe},
		{Name: "astro", Critical: false, Probe: okProbe},
	})

	if code != http.StatusOK {
		t.Errorf("status = %d, want 200", code)
	}
	if body.Status != StatusOK {
		t.Errorf("overall = %q, want %q", body.Status, StatusOK)
	}
	if len(body.Checks) != 2 {
		t.Errorf("got %d checks, want 2", len(body.Checks))
	}
}

// TestHealthNonCriticalFailureDegrades is the machine-readable form of
// invariant #4.
//
// Phase 2 requires cached charts to serve when astro-service is down, and
// Phase 5 requires everything except chat to work without ai-service. If
// a non-critical failure returned 503, a load balancer would pull this
// instance out of rotation and take the whole product down over a
// dependency the product does not need.
func TestHealthNonCriticalFailureDegrades(t *testing.T) {
	code, body := doHealth(t, []Prober{
		{Name: "postgres", Critical: true, Probe: okProbe},
		{Name: "redis", Critical: true, Probe: okProbe},
		{Name: "astro", Critical: false, Probe: badProbe},
	})

	if code != http.StatusOK {
		t.Errorf("status = %d, want 200 — a non-critical failure must NOT return 503", code)
	}
	if body.Status != StatusDegraded {
		t.Errorf("overall = %q, want %q", body.Status, StatusDegraded)
	}
	if body.Checks["astro"].Status != StatusError {
		t.Errorf("astro check = %q, want %q", body.Checks["astro"].Status, StatusError)
	}
	if body.Checks["postgres"].Status != StatusOK {
		t.Error("a healthy dependency was marked unhealthy")
	}
	if body.Checks["astro"].Error == "" {
		t.Error("failing check carries no error message; operators need to know why")
	}
}

func TestHealthCriticalFailureIsAnOutage(t *testing.T) {
	code, body := doHealth(t, []Prober{
		{Name: "postgres", Critical: true, Probe: badProbe},
		{Name: "astro", Critical: false, Probe: okProbe},
	})

	if code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 — a critical dependency is down", code)
	}
	if body.Status != StatusError {
		t.Errorf("overall = %q, want %q", body.Status, StatusError)
	}
}

// TestHealthErrorBeatsDegraded pins the severity ordering: one critical
// failure must not be masked by other dependencies merely degrading.
func TestHealthErrorBeatsDegraded(t *testing.T) {
	code, body := doHealth(t, []Prober{
		{Name: "astro", Critical: false, Probe: badProbe},
		{Name: "postgres", Critical: true, Probe: badProbe},
	})

	if body.Status != StatusError {
		t.Errorf("overall = %q, want %q — error must outrank degraded", body.Status, StatusError)
	}
	if code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", code)
	}
}

// TestHealthProbesRunConcurrently guards the property that makes the 2s
// budget workable. Run serially, three 300ms probes would take 900ms.
func TestHealthProbesRunConcurrently(t *testing.T) {
	slow := func(context.Context) error {
		time.Sleep(300 * time.Millisecond)
		return nil
	}

	start := time.Now()
	_, body := doHealth(t, []Prober{
		{Name: "a", Probe: slow},
		{Name: "b", Probe: slow},
		{Name: "c", Probe: slow},
	})
	elapsed := time.Since(start)

	if len(body.Checks) != 3 {
		t.Fatalf("got %d checks, want 3", len(body.Checks))
	}
	if elapsed > 700*time.Millisecond {
		t.Errorf("three 300ms probes took %v — they appear to run serially", elapsed)
	}
}

// TestHealthOneSlowProbeDoesNotStallOthers: a hanging dependency must not
// make the health endpoint itself hang. That turns one sick dependency
// into an apparently sick service and can empty a load balancer pool.
func TestHealthOneSlowProbeDoesNotStallOthers(t *testing.T) {
	hang := func(ctx context.Context) error {
		<-ctx.Done() // blocks until the handler's budget expires
		return ctx.Err()
	}

	start := time.Now()
	_, body := doHealth(t, []Prober{
		{Name: "fast", Critical: true, Probe: okProbe},
		{Name: "hanging", Critical: false, Probe: hang},
	})
	elapsed := time.Since(start)

	if elapsed > healthProbeTimeout+time.Second {
		t.Errorf("handler took %v; it must be bounded by the %v budget", elapsed, healthProbeTimeout)
	}
	if body.Checks["fast"].Status != StatusOK {
		t.Error("a healthy probe was affected by a hanging one")
	}
	if body.Checks["hanging"].Status != StatusError {
		t.Error("the hanging probe should be reported as an error")
	}
}

func TestHealthEveryProbeRunsExactlyOnce(t *testing.T) {
	var calls atomic.Int32
	counting := func(context.Context) error {
		calls.Add(1)
		return nil
	}

	doHealth(t, []Prober{
		{Name: "a", Probe: counting},
		{Name: "b", Probe: counting},
	})

	if got := calls.Load(); got != 2 {
		t.Errorf("probes ran %d times, want 2", got)
	}
}

// TestReadyIsIndependentOfDependencies: /ready answers "is this process
// alive", nothing more. A container platform restarting the pod because a
// downstream service blipped is almost never what you want.
func TestReadyIsIndependentOfDependencies(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	ReadyHandler("api", "test")(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	var body HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Status != StatusOK {
		t.Errorf("status = %q, want %q", body.Status, StatusOK)
	}
	if len(body.Checks) != 0 {
		t.Errorf("/ready reported %d dependency checks; it must report none", len(body.Checks))
	}
}
