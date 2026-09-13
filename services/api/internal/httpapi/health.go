package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
)

// healthProbeTimeout bounds the whole health check.
//
// A health endpoint that hangs because a dependency hangs is worse than
// useless — it turns one sick dependency into an apparently sick service
// and can take a load balancer's whole pool out.
const healthProbeTimeout = 2 * time.Second

type CheckStatus string

const (
	StatusOK       CheckStatus = "ok"
	StatusError    CheckStatus = "error"
	StatusDegraded CheckStatus = "degraded"
)

type Check struct {
	Status    CheckStatus `json:"status"`
	LatencyMS int64       `json:"latency_ms"`
	// Reason is a closed-vocabulary code, never the underlying error
	// text. See classifyProbeError.
	Reason ProbeReason `json:"reason,omitempty"`
}

// ProbeReason is why a probe failed, in terms safe to hand to any caller.
//
// `/health` is unauthenticated by necessity — load balancers and uptime
// checks cannot present a credential — so whatever it returns is public.
// `err.Error()` on a failed probe is typically
// `Get "http://localhost:8025/readyz": dial tcp 127.0.0.1:8025: connect:
// connection refused`, which hands an attacker the internal hostname,
// port and path for free.
//
// The full error still reaches the log, keyed by trace ID, which is where
// the project's error rule says detail belongs.
type ProbeReason string

const (
	ReasonTimeout     ProbeReason = "timeout"     // probe exceeded its deadline
	ReasonUnreachable ProbeReason = "unreachable" // refused, no route, DNS failure
	ReasonUnavailable ProbeReason = "unavailable" // reachable but not healthy
)

// classifyProbeError maps an arbitrary error onto the closed vocabulary.
//
// Three buckets is enough to act on: a timeout means the dependency is
// overloaded, unreachable means it is down or misrouted, and unavailable
// means it answered and said no. Anything finer would start encoding the
// topology this function exists to hide.
func classifyProbeError(err error) ProbeReason {
	if errors.Is(err, context.DeadlineExceeded) {
		return ReasonTimeout
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ReasonTimeout
	}

	var dnsErr *net.DNSError
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.As(err, &dnsErr) {
		return ReasonUnreachable
	}

	return ReasonUnavailable
}

type HealthResponse struct {
	Status  CheckStatus      `json:"status"`
	Service string           `json:"service"`
	Version string           `json:"version"`
	Checks  map[string]Check `json:"checks"`
}

// Prober is one named dependency that can be health-checked.
type Prober struct {
	Name string
	// Critical marks a dependency the service cannot function without.
	// A failing critical dependency makes the whole service "error";
	// a failing non-critical one makes it "degraded".
	Critical bool
	Probe    func(context.Context) error
}

// HealthHandler probes every dependency concurrently and reports each
// one independently.
//
// Reporting per-dependency status rather than a single boolean is the
// whole point: with three backend services plus Postgres, Redis and
// object storage, "the API is down" is not an actionable statement.
func HealthHandler(service, version string, probers []Prober) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), healthProbeTimeout)
		defer cancel()

		var (
			mu      sync.Mutex
			checks  = make(map[string]Check, len(probers))
			worst   = StatusOK
			group   errgroup.Group
			degrade = func(s CheckStatus) {
				// error beats degraded beats ok
				if s == StatusError || (s == StatusDegraded && worst == StatusOK) {
					worst = s
				}
			}
		)

		for _, p := range probers {
			p := p
			group.Go(func() error {
				start := time.Now()
				err := p.Probe(ctx)
				latency := time.Since(start).Milliseconds()

				check := Check{Status: StatusOK, LatencyMS: latency}
				outcome := StatusOK

				if err != nil {
					check.Status = StatusError
					check.Reason = classifyProbeError(err)
					outcome = StatusDegraded
					if p.Critical {
						outcome = StatusError
					}

					// The detail the response deliberately withholds. The
					// context handler attaches trace_id, so this line and
					// the client's response are correlatable.
					slog.ErrorContext(ctx, "health probe failed",
						slog.String("dependency", p.Name),
						slog.Bool("critical", p.Critical),
						slog.String("reason", string(check.Reason)),
						slog.Any("err", err),
					)
				}

				mu.Lock()
				checks[p.Name] = check
				degrade(outcome)
				mu.Unlock()

				// Never propagate the error: one failing probe must not
				// abort the others. We want the full picture.
				return nil
			})
		}

		_ = group.Wait()

		status := http.StatusOK
		if worst == StatusError {
			status = http.StatusServiceUnavailable
		}

		WriteJSON(w, status, HealthResponse{
			Status:  worst,
			Service: service,
			Version: version,
			Checks:  checks,
		})
	}
}

// ReadyHandler is the liveness/readiness probe for orchestrators.
//
// Separate from /health deliberately: a container platform restarting the
// pod because a downstream service blipped is almost never what you want.
// This answers only "is this process alive and serving?".
func ReadyHandler(service, version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, HealthResponse{
			Status:  StatusOK,
			Service: service,
			Version: version,
			Checks:  map[string]Check{},
		})
	}
}
