package httpapi

import (
	"context"
	"net/http"
	"sync"
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
	Error     string      `json:"error,omitempty"`
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
					check.Error = err.Error()
					outcome = StatusDegraded
					if p.Critical {
						outcome = StatusError
					}
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
