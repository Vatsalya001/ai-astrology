// Package clients holds the typed clients api-service uses to reach the
// internal Python services.
//
// The per-service clients in astroclient/ and aiclient/ are GENERATED
// from each service's OpenAPI document (ADR-005) and must never be
// edited by hand — a hand-written cross-service client drifts silently
// from the contract it claims to implement. Regenerate with:
//
//	task contracts
//
// This file owns only the concerns the generator does not: the internal
// auth token, trace propagation, timeouts, and a uniform health probe.
package clients

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients/aiclient"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients/astroclient"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/logging"
)

const (
	headerInternalToken = "X-Internal-Token"
	headerTraceID       = "X-Trace-Id"
)

// headerInjector adds the internal token and propagates the trace ID on
// every outbound request.
//
// Trace propagation is what makes one user request correlatable across
// all three services; without it, a failure in Python cannot be tied to
// the Go request that caused it.
func headerInjector(token string) func(context.Context, *http.Request) error {
	return func(ctx context.Context, req *http.Request) error {
		req.Header.Set(headerInternalToken, token)
		if id := logging.TraceIDFrom(ctx); id != "" {
			req.Header.Set(headerTraceID, id)
		}
		return nil
	}
}

// httpClient builds the transport stack shared by both clients.
//
// Timeout is the budget for the WHOLE call including retries, not per
// attempt. A caller that asked for 10s should wait 10s, not 30.
func httpClient(timeout time.Duration, name string) *http.Client {
	// Ordered deliberately: breaker OUTSIDE retry.
	//
	// Inside, the breaker would see each retry as a separate failure and
	// trip three times faster than configured. Outside, it sees one
	// logical call — request plus its retries — which is the unit an
	// operator means by "five consecutive failures", and it short-circuits
	// the whole retry sequence rather than just its last attempt.
	return &http.Client{
		Timeout: timeout,
		Transport: newCircuitBreaker(
			&retryTransport{
				name: name,
				base: &http.Transport{
					MaxIdleConns:        100,
					MaxIdleConnsPerHost: 10,
					IdleConnTimeout:     90 * time.Second,
				},
			},
			name,
			nil,
		),
	}
}

// ─── astro-service ───────────────────────────────────────────────────

type Astro struct {
	api *astroclient.ClientWithResponses
}

func NewAstro(baseURL, token string, timeout time.Duration) (*Astro, error) {
	api, err := astroclient.NewClientWithResponses(
		strings.TrimRight(baseURL, "/"),
		astroclient.WithHTTPClient(httpClient(timeout, "astro")),
		astroclient.WithRequestEditorFn(headerInjector(token)),
	)
	if err != nil {
		return nil, fmt.Errorf("build astro client: %w", err)
	}
	return &Astro{api: api}, nil
}

// Health probes the service. The caller bounds the wait via ctx.
//
// No detail to report: astro-service is deterministic by construction,
// with no provider to choose and nothing configurable that an operator
// would need to see on a status page.
func (a *Astro) Health(ctx context.Context) error {
	resp, err := a.api.HealthHealthGetWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("astro: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("astro: unexpected status %d", resp.StatusCode())
	}
	if resp.JSON200 == nil || resp.JSON200.Status != "ok" {
		return fmt.Errorf("astro: reported unhealthy")
	}
	return nil
}

// ─── ai-service ──────────────────────────────────────────────────────

type AI struct {
	api *aiclient.ClientWithResponses

	// Bounds concurrent model calls per process. See ai_complete.go —
	// a counting semaphore, because the resource being protected is
	// concurrent SPEND rather than request rate.
	//
	// Lazily created so a zero-value AI (and every existing test that
	// builds one) still works; `slotsOnce` is what makes that safe
	// under concurrent first calls.
	slots     chan struct{}
	slotsOnce sync.Once
}

func NewAI(baseURL, token string, timeout time.Duration) (*AI, error) {
	api, err := aiclient.NewClientWithResponses(
		strings.TrimRight(baseURL, "/"),
		aiclient.WithHTTPClient(httpClient(timeout, "ai")),
		aiclient.WithRequestEditorFn(headerInjector(token)),
	)
	if err != nil {
		return nil, fmt.Errorf("build ai client: %w", err)
	}
	return &AI{api: api}, nil
}

// Health probes ai-service and reports which model backend it is wired
// to.
//
// That pairing is deliberate. Invariant 3 — no real user data reaches a
// free model tier — is enforced at ai-service startup by app/guards.py,
// but an invariant enforced only at boot is invisible afterwards. The
// status page is where an operator can see, without reading a config
// file, that production is on a paid tier and development is not.
//
// ai-service does NOT probe the provider itself, by design: a health
// check that calls a language model costs money on every poll and adds
// seconds to an endpoint that should take milliseconds. So this reports
// configuration, not reachability, and says so on the page.
func (a *AI) Health(ctx context.Context) (string, error) {
	resp, err := a.api.HealthHealthGetWithResponse(ctx)
	if err != nil {
		return "", fmt.Errorf("ai: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("ai: unexpected status %d", resp.StatusCode())
	}
	if resp.JSON200 == nil || resp.JSON200.Status != "ok" {
		return "", fmt.Errorf("ai: reported unhealthy")
	}
	return providerDetail(resp.JSON200.Provider, resp.JSON200.ProviderTier), nil
}

// providerDetail renders the model backend for display.
//
// Empty rather than half-rendered if either field is missing: "openai-
// compatible · " tells an operator less than showing nothing and is
// easier to misread as a truncated value.
func providerDetail(provider, tier string) string {
	if provider == "" || tier == "" {
		return ""
	}
	return provider + " · " + tier
}
