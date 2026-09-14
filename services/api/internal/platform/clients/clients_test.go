package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/logging"
)

// healthStub stands in for a Python service and records what it received.
type healthStub struct {
	server      *httptest.Server
	gotToken    string
	gotTraceID  string
	requestPath string
}

func newHealthStub(t *testing.T, status int, body string) *healthStub {
	t.Helper()
	s := &healthStub{}

	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.gotToken = r.Header.Get(headerInternalToken)
		s.gotTraceID = r.Header.Get(headerTraceID)
		s.requestPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.server.Close)

	return s
}

const healthyBody = `{"status":"ok","service":"astro","version":"0.1.0"}`

// TestInternalTokenIsSent covers defence in depth for the case where the
// network boundary keeping the Python services internal is misconfigured.
func TestInternalTokenIsSent(t *testing.T) {
	stub := newHealthStub(t, http.StatusOK, healthyBody)

	astro, err := NewAstro(stub.server.URL, "s3cret-internal-token", 5*time.Second)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}
	if err := astro.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}

	if stub.gotToken != "s3cret-internal-token" {
		t.Errorf("X-Internal-Token = %q, want it set on every outbound call", stub.gotToken)
	}
	if stub.requestPath != "/health" {
		t.Errorf("path = %q, want /health", stub.requestPath)
	}
}

// TestTraceIDIsPropagated is what makes one user request correlatable
// across all three services. Without it, a failure in Python cannot be
// tied back to the Go request that caused it.
func TestTraceIDIsPropagated(t *testing.T) {
	stub := newHealthStub(t, http.StatusOK, healthyBody)

	astro, err := NewAstro(stub.server.URL, "token-long-enough", 5*time.Second)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}

	ctx := logging.WithTraceID(context.Background(), "trace-propagated-42")
	if err := astro.Health(ctx); err != nil {
		t.Fatalf("Health: %v", err)
	}

	if stub.gotTraceID != "trace-propagated-42" {
		t.Errorf("X-Trace-Id = %q, want the context's trace ID", stub.gotTraceID)
	}
}

func TestNoTraceHeaderWhenContextHasNone(t *testing.T) {
	stub := newHealthStub(t, http.StatusOK, healthyBody)

	ai, err := NewAI(stub.server.URL, "token-long-enough", 5*time.Second)
	if err != nil {
		t.Fatalf("NewAI: %v", err)
	}
	if _, err := ai.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}

	if stub.gotTraceID != "" {
		t.Errorf("X-Trace-Id = %q, want it absent when the context carries none", stub.gotTraceID)
	}
}

func TestHealthFailsOnNon200(t *testing.T) {
	stub := newHealthStub(t, http.StatusServiceUnavailable, `{"status":"error"}`)

	astro, err := NewAstro(stub.server.URL, "token-long-enough", 5*time.Second)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}
	if err := astro.Health(context.Background()); err == nil {
		t.Fatal("Health succeeded on a 503; it must fail")
	}
}

// TestHealthFailsOnUnhealthyBody: a service can answer 200 while
// reporting itself unhealthy. The body is authoritative.
func TestHealthFailsOnUnhealthyBody(t *testing.T) {
	stub := newHealthStub(t, http.StatusOK,
		`{"status":"degraded","service":"ai","version":"0.1.0"}`)

	ai, err := NewAI(stub.server.URL, "token-long-enough", 5*time.Second)
	if err != nil {
		t.Fatalf("NewAI: %v", err)
	}
	if _, err := ai.Health(context.Background()); err == nil {
		t.Fatal("Health succeeded on a 200 reporting 'degraded'; it must fail")
	}
}

func TestHealthFailsWhenServiceUnreachable(t *testing.T) {
	// Port 1 is reserved and nothing listens there.
	astro, err := NewAstro("http://127.0.0.1:1", "token-long-enough", 500*time.Millisecond)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}

	err = astro.Health(context.Background())
	if err == nil {
		t.Fatal("Health succeeded against an unreachable service")
	}
	// The service name must appear, or a health failure is unattributable.
	if !strings.Contains(err.Error(), "astro") {
		t.Errorf("error %q does not name the service", err)
	}
}

// TestContextCancellationIsHonoured: a cancelled request must stop work
// downstream rather than running to completion unobserved.
func TestContextCancellationIsHonoured(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer slow.Close()

	astro, err := NewAstro(slow.URL, "token-long-enough", 10*time.Second)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	if err := astro.Health(ctx); err == nil {
		t.Fatal("Health succeeded despite a cancelled context")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took %v; cancellation was not honoured", elapsed)
	}
}

func TestBaseURLTrailingSlashIsTolerated(t *testing.T) {
	stub := newHealthStub(t, http.StatusOK, healthyBody)

	astro, err := NewAstro(stub.server.URL+"/", "token-long-enough", 5*time.Second)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}
	if err := astro.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if stub.requestPath != "/health" {
		t.Errorf("path = %q, want /health — a trailing slash produced a double slash",
			stub.requestPath)
	}
}

func TestProviderDetail(t *testing.T) {
	cases := []struct {
		name, provider, tier, want string
	}{
		{"both present", "openai-compatible", "local", "openai-compatible · local"},
		{"paid tier", "anthropic", "paid", "anthropic · paid"},
		// Half a value is worse than none: "anthropic · " reads as a
		// truncated string and tells an operator nothing.
		{"missing tier", "anthropic", "", ""},
		{"missing provider", "", "paid", ""},
		{"both missing", "", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := providerDetail(tc.provider, tc.tier); got != tc.want {
				t.Errorf("providerDetail(%q, %q) = %q, want %q",
					tc.provider, tc.tier, got, tc.want)
			}
		})
	}
}
