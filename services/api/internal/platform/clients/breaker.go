package clients

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// ErrCircuitOpen is returned while the breaker is refusing calls.
//
// Distinguishable from a transport error on purpose: the chart service
// treats it as "astro is down, serve the cached chart" rather than as a
// one-off failure worth retrying.
var ErrCircuitOpen = errors.New("clients: circuit is open")

// Why a breaker at all, given retries already exist.
//
// Retrying is right for a blip and wrong for an outage. When
// astro-service is genuinely down, every request spends its full timeout
// and both retries before failing — ten seconds times three — and those
// requests pile up holding Go routines, database connections and, in a
// browser, the user's patience.
//
// The breaker converts that into an immediate, cheap failure so the
// caller can fall back to a cached chart. Which is the property the spec
// asks for: a user must be able to view their existing Kundli when the
// compute service is unavailable.
const (
	// defaultThreshold consecutive failures before opening.
	//
	// Consecutive, not a rate: a service that fails one request in five
	// is degraded but usable, and opening on it would turn a partial
	// outage into a total one.
	defaultThreshold = 5

	// defaultCooldown before a single probe is allowed through.
	defaultCooldown = 30 * time.Second
)

type circuitState int

const (
	closed circuitState = iota
	open
	halfOpen
)

// circuitBreaker wraps a RoundTripper.
type circuitBreaker struct {
	base   http.RoundTripper
	name   string
	logger *slog.Logger

	threshold int
	cooldown  time.Duration

	mu            sync.Mutex
	state         circuitState
	failures      int
	openedAt      time.Time
	probeInFlight bool
}

func newCircuitBreaker(base http.RoundTripper, name string, logger *slog.Logger) *circuitBreaker {
	if logger == nil {
		logger = slog.Default()
	}
	return &circuitBreaker{
		base:      base,
		name:      name,
		logger:    logger,
		threshold: defaultThreshold,
		cooldown:  defaultCooldown,
	}
}

func (c *circuitBreaker) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := c.allow(); err != nil {
		return nil, err
	}

	resp, err := c.base.RoundTrip(req)
	c.record(classify(req, resp, err))
	return resp, err
}

// ─── what a round trip proves ────────────────────────────────────────

// evidence is what one round trip says about the service's health.
//
// The third value is the one that matters. A request the CALLER
// abandoned says nothing either way — the user closed the tab, or the
// HTTP handler above us hit its own deadline — and counting those as
// service failures is how five abandoned requests take a perfectly
// healthy provider out for everybody for a cooldown.
type evidence int

const (
	serviceAnswered evidence = iota
	serviceFailed
	noEvidence
)

// classify decides which of the three a round trip was.
//
// A 5xx counts as a failure; a 4xx does not. A 422 means we sent
// something invalid, and tripping the breaker on our own bad request
// would take the service down for everybody because one caller had a
// bug.
//
// The hard case is a context error, because both kinds arrive here
// looking identical. http.Client.Timeout does not interrupt the request
// from the outside: it REPLACES the request's context with one carrying
// its own deadline. So "the user closed the tab after 200ms" and "the
// service hung through our entire budget" are both
// context.DeadlineExceeded, on a context that is Done, with a deadline
// that has passed. `req.Context().Err()` cannot separate them.
//
// What can is the caller's own context, carried down as a value by
// whoever owned it before any budget of ours was layered on top — see
// headerInjector and AI.Complete. If that context is still alive, the
// deadline that expired was OURS, and a service that cannot answer
// inside our budget has failed in the way the breaker exists to catch:
// a hung service is worse than a refused one, because every request
// holds a goroutine for the full timeout.
//
// A request carrying no marker is counted, not ignored. Missing the
// evidence of a real outage is the more expensive mistake, so the
// unmarked path keeps the old behaviour.
func classify(req *http.Request, resp *http.Response, err error) evidence {
	if err == nil {
		if resp != nil && resp.StatusCode >= 500 {
			return serviceFailed
		}
		return serviceAnswered
	}

	if isContextError(err) && callerAbandoned(req.Context()) {
		return noEvidence
	}
	return serviceFailed
}

// isContextError narrows the exemption to the two errors a caller can
// actually cause.
//
// A refused connection that happens to land in the same microsecond as
// a cancellation is still real evidence that the service is down, and
// discarding it would let an outage hide behind ordinary user
// behaviour.
func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// callerContextKey carries the caller's own context down to the breaker.
type callerContextKey struct{}

// withCallerContext marks ctx as the boundary between the caller's
// patience and ours.
//
// Call this at the point a context arrives from outside this package
// and BEFORE deriving any deadline of our own from it. Everything
// derived afterwards is our budget, and its expiry is the service's
// fault, not the caller's.
func withCallerContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, callerContextKey{}, ctx)
}

// callerAbandoned reports whether the caller gave up before we did.
func callerAbandoned(ctx context.Context) bool {
	caller, ok := ctx.Value(callerContextKey{}).(context.Context)
	return ok && caller.Err() != nil
}

// hasCallerContext reports whether a boundary has already been marked.
func hasCallerContext(ctx context.Context) bool {
	_, ok := ctx.Value(callerContextKey{}).(context.Context)
	return ok
}

func (c *circuitBreaker) allow() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch c.state {
	case closed:
		return nil

	case open:
		if time.Since(c.openedAt) < c.cooldown {
			return ErrCircuitOpen
		}
		// Cooldown elapsed: let exactly ONE request through to find out
		// whether the service is back. Letting them all through would
		// hit a recovering service with the full backlog at the moment
		// it is least able to take it.
		c.state = halfOpen
		c.probeInFlight = true
		return nil

	case halfOpen:
		if c.probeInFlight {
			return ErrCircuitOpen
		}
		c.probeInFlight = true
		return nil
	}

	return nil
}

func (c *circuitBreaker) record(what evidence) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == halfOpen {
		c.probeInFlight = false
	}

	// An abandoned request leaves the counters exactly as it found them.
	// The probe slot above is still released first, deliberately: a
	// half-open probe whose caller walked away would otherwise leave
	// probeInFlight set for ever, and half-open refuses everything while
	// a probe is outstanding — one cancelled request would become a
	// permanent outage that no cooldown ever ends.
	if what == noEvidence {
		return
	}

	if what == serviceAnswered {
		if c.state != closed {
			c.logger.Info("circuit closed", slog.String("service", c.name))
		}
		c.state = closed
		c.failures = 0
		return
	}

	c.failures++

	// A failed probe reopens immediately rather than counting towards the
	// threshold again — the service has just told us it is still down.
	if c.state == halfOpen {
		c.state = open
		c.openedAt = time.Now()
		c.logger.Warn("circuit reopened after a failed probe", slog.String("service", c.name))
		return
	}

	if c.state == closed && c.failures >= c.threshold {
		c.state = open
		c.openedAt = time.Now()
		c.logger.Error("circuit opened",
			slog.String("service", c.name),
			slog.Int("consecutive_failures", c.failures),
			slog.Duration("cooldown", c.cooldown))
	}
}

// state reports the current state, for tests.
func (c *circuitBreaker) currentState() circuitState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}
