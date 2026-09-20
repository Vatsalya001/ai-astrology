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
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients/aiclient"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients/astroclient"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/logging"
)

const (
	headerInternalToken = "X-Internal-Token"
	headerTraceID       = "X-Trace-Id"
)

// headerInjector adds the internal token, propagates the trace ID, and
// records the caller's context on every outbound request.
//
// Trace propagation is what makes one user request correlatable across
// all three services; without it, a failure in Python cannot be tied to
// the Go request that caused it.
//
// The caller-context marker is here for the same reason the headers
// are: this is the one place every generated client method passes
// through. Applied per call site it would be forgotten on exactly the
// method that needed it, and the symptom — a breaker that opens against
// a healthy service — would be blamed on the service. It is the last
// point at which the context is still the caller's: http.Client.Timeout
// replaces it a few frames later. See classify() in breaker.go.
//
// Only if absent: a caller that has already marked its own boundary
// (AI.Complete, which layers a 90s budget on top) knows better than
// this function does, and overwriting it would relabel our own budget
// as the caller's patience.
func headerInjector(token string) func(context.Context, *http.Request) error {
	return func(ctx context.Context, req *http.Request) error {
		req.Header.Set(headerInternalToken, token)
		if id := logging.TraceIDFrom(ctx); id != "" {
			req.Header.Set(headerTraceID, id)
		}

		if !hasCallerContext(req.Context()) {
			// In place because RequestEditorFn hands us the request by
			// pointer and http.Client.Do reads the context from it; a
			// returned copy would be dropped on the floor.
			*req = *req.WithContext(withCallerContext(req.Context()))
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
	// Written ONCE, in newAI, and never again. It used to be created
	// lazily inside a sync.Once on the Complete path, which was a data
	// race: InFlight — called by the admin config endpoint, from a
	// different request goroutine — read the field without taking part
	// in that Once, so there was no happens-before edge between the
	// write and the read.
	//
	// "It is only a gauge, a stale read is harmless" is not a defence.
	// A race with no ordering edge is undefined behaviour, not a wrong
	// number: the reader may see the pointer before the channel it
	// points at is initialised, and `go test -race` fails the build
	// either way — which is what the constitution means by running
	// every Go test under -race.
	slots chan struct{}

	// Per-call budget for Complete. A field rather than the bare
	// constant so a test can shrink it — and so it is visibly the same
	// value the transport was sized from.
	completionTimeout time.Duration
}

func NewAI(baseURL, token string, timeout time.Duration) (*AI, error) {
	return newAI(baseURL, token, timeout, completionTimeout)
}

// newAI takes both budgets so a test can shrink them.
//
// ── Why the transport budget is the LARGER of the two ──
//
// http.Client.Timeout bounds the whole call and cannot be extended by a
// per-request context — a context deadline can only ever make a request
// finish SOONER. Built with the general 10s ServiceTimeout, the 90s
// context that Complete sets was dead code: every completion was capped
// at 10s, and a deep-tier interpretation that legitimately takes thirty
// seconds failed as a timeout with a comment above it claiming it had a
// minute and a half.
//
// Raising it is safe for the other caller on this client. Health bounds
// itself: httpapi/health.go wraps each probe in its own context, so a
// long transport budget cannot make the status page hang.
func newAI(baseURL, token string, timeout, completion time.Duration) (*AI, error) {
	transportBudget := timeout
	if completion > transportBudget {
		transportBudget = completion
	}

	api, err := aiclient.NewClientWithResponses(
		strings.TrimRight(baseURL, "/"),
		aiclient.WithHTTPClient(httpClient(transportBudget, "ai")),
		aiclient.WithRequestEditorFn(headerInjector(token)),
	)
	if err != nil {
		return nil, fmt.Errorf("build ai client: %w", err)
	}
	return &AI{
		api: api,
		// Eager, so the field is only ever written here — before the
		// pointer is published to any other goroutine. See the field.
		slots:             make(chan struct{}, maxConcurrentCompletions),
		completionTimeout: completion,
	}, nil
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
