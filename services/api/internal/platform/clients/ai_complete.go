package clients

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients/aiclient"
)

// ErrAIUnavailable means ai-service could not be reached, or every
// provider behind it failed.
//
// Mapped to a 503 with a retryable flag, per PHASE-04 §2, so the UI can
// show a real message rather than a spinner that never resolves.
var ErrAIUnavailable = errors.New("clients: ai-service unavailable")

// ErrAIRejected means ai-service refused the request — a bug on this
// side. Never retried, because it will fail identically forever.
var ErrAIRejected = errors.New("clients: ai-service rejected the request")

// ErrAIRateLimited means this process is already making as many
// concurrent model calls as it is allowed to.
//
// A LOCAL limit, not the provider's. PHASE-04 §14 lists rate limiting on
// the internal completion path as a security item and says why: "a
// runaway loop is a real cost event". The provider's own 429 arrives
// after the money is spent; this one arrives before.
var ErrAIRateLimited = errors.New("clients: ai completion rate limit reached")

// maxConcurrentCompletions bounds in-flight model calls per process.
//
// Chosen as a COST bound rather than a throughput one. Eight concurrent
// deep-tier calls is a few cents a second; eight hundred is a bill
// nobody authorised, and the difference between the two is one bug in a
// retry loop. Raising it is a deliberate decision, which is why it is a
// named constant rather than a config default.
const maxConcurrentCompletions = 8

// completionTimeout is the budget for one completion, end to end.
//
// Much longer than the general ServiceTimeout because a deep-tier
// interpretation genuinely takes tens of seconds, and because the
// pipeline behind it may make four model calls. Short enough that a
// hung provider does not hold a connection open indefinitely.
const completionTimeout = 90 * time.Second

// Complete runs the AI pipeline and returns the result with its
// telemetry.
//
// The envelope comes back WHOLE. The caller persists `Telemetry`
// alongside whatever it writes, in one transaction — see internal/ailogs
// for why that matters.
func (a *AI) Complete(
	ctx context.Context,
	body aiclient.CompleteRequest,
) (*aiclient.AIResponseEnvelopeCompleteResult, error) {
	release, ok := a.acquire()
	if !ok {
		return nil, ErrAIRateLimited
	}
	defer release()

	// A context deadline can only make a request finish SOONER, never
	// later, so this alone was not enough: the client was built with the
	// general 10s ServiceTimeout and capped every completion at 10s
	// regardless of what this line said. newAI now sizes the transport
	// from the same value, and TestACompletionIsNotCappedByTheGeneralTimeout
	// is what says so.
	budget := a.completionTimeout
	if budget <= 0 {
		budget = completionTimeout
	}

	// Mark the boundary BEFORE the budget, never after. Below this line
	// every deadline is ours, and the breaker has to be able to tell the
	// two apart: an ai-service that hangs through the full 90s is an
	// outage worth opening the circuit for, while a user who closed the
	// tab is not. Both reach the breaker as context.DeadlineExceeded.
	// See classify() in breaker.go.
	ctx, cancel := context.WithTimeout(withCallerContext(ctx), budget)
	defer cancel()

	// Deliberately NOT marked idempotent. Unlike a chart computation,
	// replaying a completion spends money again and may produce a
	// different answer — so the retry transport must not replay it. See
	// MarkIdempotent.
	resp, err := a.api.CompleteV1CompletePostWithResponse(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("%w: complete: %v", ErrAIUnavailable, err)
	}

	if resp.JSON200 == nil {
		return nil, aiStatusFailure(resp.StatusCode())
	}
	return resp.JSON200, nil
}

// aiStatusFailure maps a status to one of the two sentinels.
//
// The split is the same one the Python adapters make and for the same
// reason: 5xx and 429 recover and should fail over or retry; a 4xx will
// fail identically forever and retrying it turns one bad request into
// several. The response body is deliberately not included — it can echo
// the request, and `.claude/rules/security.md` keeps request content out
// of error strings.
func aiStatusFailure(status int) error {
	if status >= http.StatusInternalServerError || status == http.StatusTooManyRequests {
		return fmt.Errorf("%w: ai-service returned %d", ErrAIUnavailable, status)
	}
	return fmt.Errorf("%w: ai-service returned %d", ErrAIRejected, status)
}

// ─── the local concurrency limit ─────────────────────────────────────

// acquire takes a slot, or reports that there is none.
//
// A counting semaphore rather than a token bucket, because the resource
// being protected is concurrent SPEND, not request rate. Ten requests a
// second that each finish in 200ms cost far less than two that run for a
// minute each, and a rate limiter cannot tell them apart.
//
// Non-blocking rather than queueing: a caller that waits behind a full
// queue eventually times out having achieved nothing, and the user has
// been staring at a spinner the whole time. A fast 429 lets the UI say
// "we're busy, try again" while it is still true.
//
// The channel itself is built in newAI, not here. Creating it lazily on
// this path raced InFlight, which reads the same field from the admin
// endpoint's goroutine. A zero-value AI — which no constructor produces
// — sends on a nil channel, which is never ready, so it refuses every
// completion rather than making an unbounded number of them.
func (a *AI) acquire() (func(), bool) {
	select {
	case a.slots <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-a.slots }) }, true
	default:
		return nil, false
	}
}

// MaxConcurrentCompletions exposes the bound for the admin config view.
//
// A function rather than an exported constant so the number stays a
// single definition: an exported const would be copied into a dashboard
// literal the first time someone wanted to render "3 of 8".
func MaxConcurrentCompletions() int { return maxConcurrentCompletions }

// InFlight reports how many completions are running.
//
// Exposed for the admin config view and for tests. `len` on a buffered
// channel is a racy VALUE and that is fine here — it is a gauge for a
// human, never something anything branches on. The FIELD is a different
// matter: it is written once in newAI and never again, which is what
// makes this safe to call from the admin request goroutine while
// Complete runs. `len` of a nil channel is 0, so a zero-value AI reads
// as idle rather than panicking.
func (a *AI) InFlight() int {
	return len(a.slots)
}
