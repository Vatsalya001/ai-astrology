package transits

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/jobs"
)

// RefreshHandler is the asynq handler behind jobs.TypeTransitRefresh.
//
// It lives here rather than in platform/jobs because platform must never
// import a domain package — the rule that keeps the dependency graph
// acyclic. platform/jobs owns the task NAME and the schedule; this owns
// what the task does.
type RefreshHandler struct {
	refresher *Refresher
	retention time.Duration
	now       func() time.Time
	logger    *slog.Logger
}

// NewRefreshHandler builds the handler. `now` is injected so the tests can
// place a run inside a chosen slot instead of racing the wall clock.
func NewRefreshHandler(
	refresher *Refresher,
	retention time.Duration,
	now func() time.Time,
	logger *slog.Logger,
) *RefreshHandler {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &RefreshHandler{refresher: refresher, retention: retention, now: now, logger: logger}
}

// Register wires the handler and its schedule into a runtime.
func (h *RefreshHandler) Register(runtime *jobs.Runtime) error {
	runtime.Handle(jobs.TypeTransitRefresh, h.Run)
	return runtime.Schedule(jobs.TransitRefreshCron, jobs.NewTransitRefreshTask())
}

// Run refreshes the transit table, then prunes.
//
// Returning an error hands the task back to asynq for retry with
// backoff — which is the reason this is a queued job and not a ticker.
// Six hours to the next scheduled attempt is far too long to wait out a
// thirty-second astro restart.
func (h *RefreshHandler) Run(ctx context.Context, _ *asynq.Task) error {
	now := h.now()

	written, err := h.refresher.Refresh(ctx, now)
	if err != nil {
		if clients.IsUnavailable(err) {
			// Retryable, and expected occasionally. Warn rather than error:
			// the stored rows are still being served, so this is degraded,
			// not broken.
			h.logger.WarnContext(ctx, "transit refresh deferred; astro-service is unavailable",
				slog.Any("err", err))
		}
		return fmt.Errorf("transit refresh: %w", err)
	}
	if written == 0 {
		// A 200 with no planets in it. Nothing was stored, so pruning now
		// would delete good rows and replace them with nothing.
		return fmt.Errorf("transit refresh: astro-service returned no positions")
	}

	// Prune only after a successful write. The other order empties the
	// table during exactly the outage it exists to survive.
	if _, pruneErr := h.refresher.Prune(ctx, now, h.retention); pruneErr != nil {
		// Not returned: the refresh succeeded and retrying it to fix a
		// failed DELETE would recompute everything for no benefit. Stale
		// rows past the retention window cost disk, not correctness.
		h.logger.ErrorContext(ctx, "transit prune failed", slog.Any("err", pruneErr))
	}
	return nil
}
