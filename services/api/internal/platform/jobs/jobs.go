// Package jobs is the background-queue layer: asynq task names, the
// schedule, and the handlers that run them.
//
// Why a queue and not the ticker cmd/worker already has. The hard-delete
// pass is a database-only sweep: idempotent, cheap, and harmless to run
// on every replica at once, so a ticker is the right size for it. The
// transit refresh is not. It calls an external service, it needs retry
// with backoff when that service is down, and running it on N replicas
// means N identical calls to astro-service and N racing upserts of the
// same rows.
//
// asynq buys exactly those three things: a Redis-backed lock that makes
// N schedulers enqueue ONE task, retry with backoff, and a dead-letter
// queue that keeps the failure visible instead of logging it into the
// void. It is the queue named in the specification's technology table,
// so this is not a new choice, only the phase in which it starts paying.
package jobs

import (
	"time"

	"github.com/hibiken/asynq"
)

// Task types. Namespaced by domain so the asynq web UI groups usefully
// once there are a dozen of them.
const (
	TypeTransitRefresh = "transits:refresh"
)

// QueueDefault is the only queue in Phase 2. Phase 3's PDF renderer gets
// its own, because a slow render must not sit in front of anything.
const QueueDefault = "default"

const (
	// TransitRefreshInterval is the specification's six hours.
	TransitRefreshInterval = 6 * time.Hour

	// TransitRefreshCron fires on the aligned boundary — 00:00, 06:00,
	// 12:00, 18:00 UTC — rather than "every six hours from whenever this
	// process booted". Alignment matters because Refresher.Slot truncates
	// to the same boundary: a deploy at 03:17 must not start writing rows
	// stamped 03:17 while the rest of the fleet writes 00:00.
	TransitRefreshCron = "0 */6 * * *"

	// TransitRefreshLockTTL is how long the uniqueness lock is held.
	//
	// Half the interval. Long enough that every replica's scheduler firing
	// in the same minute collapses to one task; short enough that the
	// genuine next run six hours later is never mistaken for a duplicate.
	// A TTL at or above the interval would eventually suppress a real run.
	TransitRefreshLockTTL = TransitRefreshInterval / 2

	// TransitRetention is how long a stored slot is kept.
	//
	// Fourteen days, which is far more than the UI shows. The reason is
	// the outage story: transits are what a user falls back on when astro
	// is unreachable, and a retention window shorter than an outage
	// deletes the fallback precisely when it is needed.
	TransitRetention = 14 * 24 * time.Hour

	// TransitRefreshMaxRetry is generous on purpose. Six hours until the
	// next scheduled attempt means a transient astro failure that is not
	// retried leaves the table stale for the whole window.
	TransitRefreshMaxRetry = 5
)

// NewTransitRefreshTask builds the periodic task.
//
// The payload is empty, and that is required rather than lazy: asynq
// derives the uniqueness key from queue + type + payload, so putting the
// timestamp in the payload would make every enqueue unique and silently
// disable the deduplication this exists for. The handler decides "now"
// when it runs, which is also the more correct instant — the moment of
// execution, not the moment of scheduling.
func NewTransitRefreshTask() *asynq.Task {
	return asynq.NewTask(TypeTransitRefresh, nil,
		asynq.Queue(QueueDefault),
		asynq.Unique(TransitRefreshLockTTL),
		asynq.MaxRetry(TransitRefreshMaxRetry),
		asynq.Timeout(2*time.Minute),
	)
}
