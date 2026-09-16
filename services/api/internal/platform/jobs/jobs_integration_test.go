//go:build integration

package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/jobs"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/testsupport"
)

// Against real Redis, because every property here lives in Redis: the
// uniqueness lock is a key with a TTL, and a fake would be asserting that
// the fake works.

func startRedis(ctx context.Context, t *testing.T) (addr string, stop func()) {
	t.Helper()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor: wait.ForLog("Ready to accept connections").
				WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		testsupport.ContainerUnavailable(t, "Redis", err)
	}

	host, _ := container.Host(ctx)
	port, _ := container.MappedPort(ctx, "6379/tcp")

	return fmt.Sprintf("%s:%s", host, port.Port()), func() {
		_ = container.Terminate(context.Background())
	}
}

// This is the whole reason the transit refresh is a queued task and not
// a ticker. Every worker replica runs a scheduler, all of them fire on
// the same cron minute, and without a lock that is N identical calls to
// astro-service and N racing upserts of the same rows every six hours.
func TestTwoSchedulersFiringTogetherEnqueueOneTask(t *testing.T) {
	ctx := context.Background()
	addr, stop := startRedis(ctx, t)
	defer stop()

	// Two clients standing in for two replicas' schedulers, enqueueing
	// the identical task definition at the same instant.
	replicaA := asynq.NewClient(asynq.RedisClientOpt{Addr: addr})
	defer func() { _ = replicaA.Close() }()
	replicaB := asynq.NewClient(asynq.RedisClientOpt{Addr: addr})
	defer func() { _ = replicaB.Close() }()

	if _, err := replicaA.Enqueue(jobs.NewTransitRefreshTask()); err != nil {
		t.Fatalf("the first replica could not enqueue at all: %v", err)
	}

	_, err := replicaB.Enqueue(jobs.NewTransitRefreshTask())
	if !errors.Is(err, asynq.ErrDuplicateTask) {
		t.Fatalf("the second replica enqueued a second copy (err = %v); "+
			"a fleet of N workers would make N calls to astro-service every six hours "+
			"and race N upserts of the same rows", err)
	}
}

// …and the guard above is only meaningful if it would notice the option
// going missing. Same type, same payload, no Unique: both must land.
//
// Without this, a refactor that dropped asynq.Unique would leave the test
// above passing for some unrelated reason and nobody would know.
func TestWithoutTheUniqueOptionBothCopiesLand(t *testing.T) {
	ctx := context.Background()
	addr, stop := startRedis(ctx, t)
	defer stop()

	client := asynq.NewClient(asynq.RedisClientOpt{Addr: addr})
	defer func() { _ = client.Close() }()

	plain := func() *asynq.Task {
		return asynq.NewTask(jobs.TypeTransitRefresh, nil, asynq.Queue(jobs.QueueDefault))
	}

	if _, err := client.Enqueue(plain()); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if _, err := client.Enqueue(plain()); err != nil {
		t.Fatalf("the second copy was refused (%v) even without asynq.Unique — "+
			"the deduplication in the previous test is coming from somewhere else, "+
			"so that test is not proving what it claims", err)
	}
}

// The uniqueness lock must be shorter than the interval it guards.
//
// asynq holds the lock for the TTL or until the task completes. A TTL at
// or above six hours would eventually suppress a genuine run — and the
// symptom is transits quietly going stale for twelve hours, which looks
// exactly like nothing being wrong.
func TestTheUniquenessLockExpiresBeforeTheNextRun(t *testing.T) {
	if jobs.TransitRefreshLockTTL >= jobs.TransitRefreshInterval {
		t.Fatalf("lock TTL %s is not shorter than the %s interval; "+
			"a real scheduled run would eventually be mistaken for a duplicate",
			jobs.TransitRefreshLockTTL, jobs.TransitRefreshInterval)
	}
}

// The runtime wiring end to end: a scheduled task reaches the handler
// registered for its type. Every piece of this — ParseRedisURI on the
// config's URL form, the mux, Start's ordering — fails only at worker
// boot otherwise, which is the worst place to find out.
func TestAScheduledTaskReachesItsHandler(t *testing.T) {
	ctx := context.Background()
	addr, stop := startRedis(ctx, t)
	defer stop()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// The URL form the config actually holds, not a bare host:port.
	runtime, err := jobs.NewRuntime("redis://"+addr, 2, logger)
	if err != nil {
		t.Fatalf("NewRuntime with a redis:// URL: %v", err)
	}

	ran := make(chan struct{}, 1)
	runtime.Handle(jobs.TypeTransitRefresh, func(context.Context, *asynq.Task) error {
		select {
		case ran <- struct{}{}:
		default:
		}
		return nil
	})

	// A one-second cron rather than the six-hourly one, because the thing
	// under test is the wiring, not the schedule. TestTheCronFiresOnSlot
	// Boundaries covers the real spec.
	if scheduleErr := runtime.Schedule("@every 1s",
		asynq.NewTask(jobs.TypeTransitRefresh, nil, asynq.Queue(jobs.QueueDefault))); scheduleErr != nil {
		t.Fatalf("Schedule: %v", scheduleErr)
	}

	if startErr := runtime.Start(); startErr != nil {
		t.Fatalf("Start: %v", startErr)
	}
	defer runtime.Shutdown()

	select {
	case <-ran:
	case <-time.After(30 * time.Second):
		t.Fatal("a scheduled task never reached the handler registered for its type")
	}
}

// A bad Redis URL must fail at construction, where it is one clear line,
// rather than at the first cron fire six hours into a deploy.
func TestABadRedisURLFailsAtConstruction(t *testing.T) {
	if _, err := jobs.NewRuntime("not-a-url", 2, nil); err == nil {
		t.Fatal("NewRuntime accepted a malformed Redis URL")
	}
}
