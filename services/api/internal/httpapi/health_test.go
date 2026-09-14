package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func okProbe(context.Context) (string, error)  { return "", nil }
func badProbe(context.Context) (string, error) { return "", errors.New("dial tcp: connection refused") }

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
	if body.Checks["astro"].Reason == "" {
		t.Error("failing check carries no reason; operators need to know why")
	}
}

// The negative case for the reason vocabulary.
//
// `/health` is unauthenticated by necessity, so its body is public. A
// probe error carrying the internal host and port would hand out the
// topology. This asserts the leak is actually refused rather than
// trusting that nobody pastes err.Error() back in.
func TestHealthNeverLeaksProbeErrorDetail(t *testing.T) {
	secret := "dial tcp 127.0.0.1:8025: connect: connection refused"
	leaky := func(context.Context) (string, error) {
		return "", fmt.Errorf("Get %q: %s", "http://internal-mail.svc:8025/readyz", secret)
	}

	_, body := doHealth(t, []Prober{
		{Name: "postgres", Critical: true, Probe: okProbe},
		{Name: "mail", Critical: false, Probe: leaky},
	})

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for _, forbidden := range []string{
		secret,
		"internal-mail.svc",
		"8025",
		"readyz",
		"127.0.0.1",
	} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Errorf("health response leaks %q to an unauthenticated caller:\n%s", forbidden, raw)
		}
	}

	if got := body.Checks["mail"].Reason; got != ReasonUnavailable {
		t.Errorf("reason = %q, want %q", got, ReasonUnavailable)
	}
}

func TestClassifyProbeError(t *testing.T) {
	refused := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED},
	}

	cases := []struct {
		name string
		err  error
		want ProbeReason
	}{
		{"deadline", context.DeadlineExceeded, ReasonTimeout},
		{"wrapped deadline", fmt.Errorf("probe: %w", context.DeadlineExceeded), ReasonTimeout},
		{"connection refused", refused, ReasonUnreachable},
		{"wrapped refused", fmt.Errorf("get: %w", refused), ReasonUnreachable},
		{"dns failure", &net.DNSError{Err: "no such host", Name: "astro"}, ReasonUnreachable},
		{"non-200 body", errors.New("unexpected status 500"), ReasonUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyProbeError(tc.err); got != tc.want {
				t.Errorf("classifyProbeError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
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
	slow := func(context.Context) (string, error) {
		time.Sleep(300 * time.Millisecond)
		return "", nil
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
	hang := func(ctx context.Context) (string, error) {
		<-ctx.Done() // blocks until the handler's budget expires
		return "", ctx.Err()
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
	counting := func(context.Context) (string, error) {
		calls.Add(1)
		return "", nil
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

// Detail carries operator-facing configuration — currently which model
// backend ai-service is wired to. It travels the same public, unauthenticated
// response as everything else here, so it gets the same scrutiny as Reason.
func TestHealthSurfacesProbeDetail(t *testing.T) {
	withDetail := func(context.Context) (string, error) {
		return "openai-compatible · local", nil
	}

	_, body := doHealth(t, []Prober{
		{Name: "postgres", Critical: true, Probe: okProbe},
		{Name: "ai", Critical: false, Probe: withDetail},
	})

	if got := body.Checks["ai"].Detail; got != "openai-compatible · local" {
		t.Errorf("ai detail = %q, want the configured provider", got)
	}

	// Dependencies with nothing to report must omit the field rather than
	// emit an empty string, so the shape stays honest for consumers.
	raw, err := json.Marshal(body.Checks["postgres"])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(raw, []byte("detail")) {
		t.Errorf("a probe with no detail still emitted the key: %s", raw)
	}
}

// A failing probe must not report a stale detail alongside its error.
// "openai-compatible · paid" next to a red dot reads as though the paid
// provider is confirmed live, which is exactly the wrong inference.
func TestHealthDetailIsEmptyWhenTheProbeFails(t *testing.T) {
	failing := func(context.Context) (string, error) {
		return "", errors.New("unreachable")
	}

	_, body := doHealth(t, []Prober{
		{Name: "ai", Critical: false, Probe: failing},
	})

	if got := body.Checks["ai"].Detail; got != "" {
		t.Errorf("failing probe reported detail %q; it proves nothing about config", got)
	}
}
