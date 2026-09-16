package clients

import (
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

	// A 5xx counts as a failure; a 4xx does not. A 422 means we sent
	// something invalid, and tripping the breaker on our own bad request
	// would take the service down for everybody because one caller had a
	// bug.
	failed := err != nil || (resp != nil && resp.StatusCode >= 500)

	c.record(failed)
	return resp, err
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

func (c *circuitBreaker) record(failed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == halfOpen {
		c.probeInFlight = false
	}

	if !failed {
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
