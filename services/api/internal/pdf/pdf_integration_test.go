//go:build integration

package pdf_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/pdf"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/jobs"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/testsupport"
)

// The status store and the queue, against real Redis.
//
// The unit tests use map-backed fakes and prove the handler's and the
// renderer's LOGIC. What they cannot prove is that the real store keys
// the way the fake does, that a TTL is actually set, or that asynq puts
// the task on the queue the options asked for — asynq.Task exposes no
// way to read its own options back, so a unit test asserting on them
// would be asserting on what the test itself passed in.

func startRedis(ctx context.Context, t *testing.T) (*goredis.Client, func()) {
	t.Helper()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForLog("Ready to accept connections").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		testsupport.ContainerUnavailable(t, "redis", err)
	}

	endpoint, err := container.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("redis endpoint: %v", err)
	}

	rdb := goredis.NewClient(&goredis.Options{Addr: endpoint})
	return rdb, func() {
		_ = rdb.Close()
		_ = container.Terminate(ctx)
	}
}

// ─── the status store ────────────────────────────────────────────────

func TestAStatusRoundTripsThroughRealRedis(t *testing.T) {
	ctx := context.Background()
	rdb, stop := startRedis(ctx, t)
	defer stop()

	store := pdf.NewStatusStore(rdb)
	userID, jobID := uuid.New(), uuid.New()
	expires := time.Now().Add(pdf.SignedURLTTL).UTC().Truncate(time.Second)

	want := pdf.Status{
		State:     pdf.StateDone,
		URL:       "https://storage.example/kundli-abc.pdf?signature=x",
		ExpiresAt: &expires,
	}
	if err := store.Set(ctx, userID, jobID, want); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := store.Get(ctx, userID, jobID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.State != want.State || got.URL != want.URL {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) {
		t.Fatalf("expiry round-tripped as %v, want %v", got.ExpiresAt, expires)
	}
}

/*
The user is part of the key, on the real store.

The handler test proves the HANDLER composes the key from the
authenticated caller. This proves the STORE it composes it with
actually separates users — which is the half a map-backed fake can
only reflect back, since the fake's keying is written by the same
test that asserts on it.

Same job id, two users, two different values: if the user were absent
from the key the second Set would overwrite the first, and both reads
would return the same thing.
*/
func TestTwoUsersWithTheSameJobIDDoNotSeeEachOther(t *testing.T) {
	ctx := context.Background()
	rdb, stop := startRedis(ctx, t)
	defer stop()

	store := pdf.NewStatusStore(rdb)
	alice, bob := uuid.New(), uuid.New()
	jobID := uuid.New() // deliberately the SAME id for both

	if err := store.Set(ctx, alice, jobID, pdf.Status{
		State: pdf.StateDone, URL: "https://storage.example/alice.pdf",
	}); err != nil {
		t.Fatalf("Set alice: %v", err)
	}
	if err := store.Set(ctx, bob, jobID, pdf.Status{
		State: pdf.StateFailed, Message: "no",
	}); err != nil {
		t.Fatalf("Set bob: %v", err)
	}

	got, err := store.Get(ctx, alice, jobID)
	if err != nil {
		t.Fatalf("Get alice: %v", err)
	}
	if got.State != pdf.StateDone || got.URL != "https://storage.example/alice.pdf" {
		t.Fatalf("Alice's status is %+v — Bob's write reached her key, which means the "+
			"user is not part of it and any job id is readable by anyone holding it", got)
	}

	// And a third user sees nothing at all for the same id.
	if _, err := store.Get(ctx, uuid.New(), jobID); !errors.Is(err, pdf.ErrNoJob) {
		t.Fatalf("a third user reading the same job id got %v, want ErrNoJob", err)
	}
}

// An unknown job is ErrNoJob rather than a zero Status.
//
// A zero Status would serialise as `{"status":""}`, which the UI renders
// as an unfinished job — a spinner for work that does not exist.
func TestAnUnknownJobIsErrNoJob(t *testing.T) {
	ctx := context.Background()
	rdb, stop := startRedis(ctx, t)
	defer stop()

	store := pdf.NewStatusStore(rdb)
	if _, err := store.Get(ctx, uuid.New(), uuid.New()); !errors.Is(err, pdf.ErrNoJob) {
		t.Fatalf("got %v, want ErrNoJob", err)
	}
}

/*
The status expires, and outlives the link it describes.

StatusTTL is deliberately an hour longer than SignedURLTTL. A client
polling right at the boundary should be told "done, here is a link
that has expired" — which reads as an expired download — rather than
404, which reads as "the render never happened".
*/
func TestAStatusCarriesATTLThatOutlivesTheLink(t *testing.T) {
	ctx := context.Background()
	rdb, stop := startRedis(ctx, t)
	defer stop()

	store := pdf.NewStatusStore(rdb)
	userID, jobID := uuid.New(), uuid.New()

	if err := store.Set(ctx, userID, jobID, pdf.Status{State: pdf.StateQueued}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// The key is an implementation detail, so it is rebuilt here from the
	// same two ids rather than exported. If this ever stops matching, the
	// assertions above fail first and say so more clearly.
	key := "pdf:job:" + userID.String() + ":" + jobID.String()

	ttl, err := rdb.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 {
		t.Fatalf("the status key has TTL %v — it never expires, so every render "+
			"anyone ever requests stays in Redis forever", ttl)
	}
	if ttl <= pdf.SignedURLTTL {
		t.Fatalf("status TTL %v does not outlive the signed URL's %v. A client polling "+
			"at the boundary gets 404, which reads as \"the render never happened\" "+
			"rather than \"your link expired\"", ttl, pdf.SignedURLTTL)
	}
}

// ─── the queue ───────────────────────────────────────────────────────

/*
The render lands on its own queue.

jobs.go says so in words — "Phase 3's PDF renderer gets its own,
because a slow render must not sit in front of anything" — and this is
where that becomes a fact. It cannot be checked in a unit test:
asynq.Task has no accessor for its options, so the only way to see
which queue an option chose is to enqueue it and ask the server.
*/
func TestARenderTaskLandsOnThePDFQueue(t *testing.T) {
	rdb, stop := startRedis(context.Background(), t)
	defer stop()

	client := asynq.NewClient(asynq.RedisClientOpt{Addr: rdb.Options().Addr})
	defer func() { _ = client.Close() }()

	jobID := uuid.New()
	task, err := pdf.NewRenderTask(pdf.RenderPayload{
		JobID: jobID, UserID: uuid.New(), ProfileID: uuid.New(), Locale: "en",
	})
	if err != nil {
		t.Fatalf("NewRenderTask: %v", err)
	}

	info, err := client.Enqueue(task)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if info.Queue != pdf.QueuePDF {
		t.Fatalf("the render landed on queue %q, want %q. On the default queue a "+
			"burst of downloads delays the transit refresh for everybody",
			info.Queue, pdf.QueuePDF)
	}
	if info.ID != jobID.String() {
		t.Fatalf("asynq task id is %q, want the job id %q — the client polls on the "+
			"job id, and a task identified by something else cannot be deduplicated "+
			"against a retried request", info.ID, jobID)
	}
	if info.MaxRetry != pdf.RenderMaxRetry {
		t.Fatalf("MaxRetry is %d, want %d", info.MaxRetry, pdf.RenderMaxRetry)
	}
}

/*
The same job id enqueued twice is one task.

A user double-clicking, or a client retrying a request whose response
was lost, must not start two browsers rendering the same document.
asynq.TaskID is what provides that, and ErrTaskIDConflict is how it
says so — which the handler treats as success, because "already doing
it" is what the caller wanted.
*/
func TestTheSameJobIDEnqueuedTwiceIsOneTask(t *testing.T) {
	rdb, stop := startRedis(context.Background(), t)
	defer stop()

	client := asynq.NewClient(asynq.RedisClientOpt{Addr: rdb.Options().Addr})
	defer func() { _ = client.Close() }()

	payload := pdf.RenderPayload{
		JobID: uuid.New(), UserID: uuid.New(), ProfileID: uuid.New(), Locale: "en",
	}

	first, err := pdf.NewRenderTask(payload)
	if err != nil {
		t.Fatalf("NewRenderTask: %v", err)
	}
	if _, err := client.Enqueue(first); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}

	second, err := pdf.NewRenderTask(payload)
	if err != nil {
		t.Fatalf("NewRenderTask: %v", err)
	}
	_, err = client.Enqueue(second)
	if !errors.Is(err, asynq.ErrTaskIDConflict) {
		t.Fatalf("the second enqueue of the same job id returned %v, want "+
			"ErrTaskIDConflict. Without it a double-click starts two browsers "+
			"rendering the same document", err)
	}

	// And a DIFFERENT job id is a genuinely new render, not suppressed.
	payload.JobID = uuid.New()
	third, err := pdf.NewRenderTask(payload)
	if err != nil {
		t.Fatalf("NewRenderTask: %v", err)
	}
	if _, err := client.Enqueue(third); err != nil {
		t.Fatalf("a new job id was refused: %v — deduplication is suppressing real "+
			"work, and the user's second download never happens", err)
	}

}

/*
A real asynq server actually picks the render up.

The two tests above prove the task lands on the "pdf" queue. Neither
proves anybody is listening to it — and that is the failure this
wiring invites: asynq's server consumes only the queues named in its
config, so a task on an unlisted queue enqueues successfully, reports
successfully, and is never executed. The client polls "queued"
forever and nothing is logged, because nothing went wrong.

So this runs the REAL server, built from the same jobs.Queues() the
worker uses, and waits for the handler to fire.
*/
func TestTheWorkerActuallyConsumesTheRenderQueue(t *testing.T) {
	rdb, stop := startRedis(context.Background(), t)
	defer stop()

	addr := rdb.Options().Addr

	ran := make(chan pdf.RenderPayload, 1)
	mux := asynq.NewServeMux()
	mux.HandleFunc(pdf.TypeRender, func(_ context.Context, task *asynq.Task) error {
		payload, err := pdf.ParseRenderPayload(task.Payload())
		if err != nil {
			return err
		}
		ran <- payload
		return nil
	})

	server := asynq.NewServer(asynq.RedisClientOpt{Addr: addr}, asynq.Config{
		Concurrency: 2,
		// The same registry the worker binary uses. Hardcoding
		// {"pdf": 1} here would make this test pass while the real
		// worker ignored the queue — asserting on the test's own config
		// rather than on the service's.
		Queues:   jobs.Queues(),
		LogLevel: asynq.FatalLevel,
	})
	if err := server.Start(mux); err != nil {
		t.Fatalf("start asynq server: %v", err)
	}
	defer server.Shutdown()

	client := asynq.NewClient(asynq.RedisClientOpt{Addr: addr})
	defer func() { _ = client.Close() }()

	want := pdf.RenderPayload{
		JobID: uuid.New(), UserID: uuid.New(), ProfileID: uuid.New(), Locale: "hi",
	}
	task, err := pdf.NewRenderTask(want)
	if err != nil {
		t.Fatalf("NewRenderTask: %v", err)
	}
	if _, err := client.Enqueue(task); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	select {
	case got := <-ran:
		if got.JobID != want.JobID || got.ProfileID != want.ProfileID || got.Locale != "hi" {
			t.Fatalf("the handler ran with %+v, want %+v", got, want)
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("no worker executed the render within 20s. The task is on queue %q; "+
			"if the server's queue map does not name it, the task sits there forever "+
			"while the client polls \"queued\" and nothing is logged", pdf.QueuePDF)
	}
}
