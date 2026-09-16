package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
)

// Runtime owns the asynq server and scheduler for one process.
//
// Both, together, because a worker replica needs both: the scheduler
// decides WHEN, the server decides WHO runs it. Keeping them in one type
// means shutdown is one call in the right order rather than two calls in
// whichever order the caller guessed.
type Runtime struct {
	server    *asynq.Server
	scheduler *asynq.Scheduler
	mux       *asynq.ServeMux
	client    *asynq.Client
	logger    *slog.Logger
}

// NewRuntime builds the queue runtime from a Redis URL.
func NewRuntime(redisURL string, concurrency int, logger *slog.Logger) (*Runtime, error) {
	if logger == nil {
		logger = slog.Default()
	}

	connection, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		return nil, fmt.Errorf("jobs: parse redis url: %w", err)
	}
	if concurrency < 1 {
		concurrency = 1
	}

	adapter := &slogAdapter{logger: logger}

	server := asynq.NewServer(connection, asynq.Config{
		Concurrency: concurrency,
		Queues:      map[string]int{QueueDefault: 1},
		Logger:      adapter,
		LogLevel:    asynq.InfoLevel,

		// A task that exhausts its retries must be loud. The default is a
		// line on asynq's own logger, which is easy to miss; this routes it
		// through ours with the task type attached, so "transits have been
		// stale for a day" is greppable.
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, taskErr error) {
			logger.ErrorContext(ctx, "background task failed",
				slog.String("task_type", task.Type()),
				slog.Any("err", taskErr))
		}),

		ShutdownTimeout: 30 * time.Second,
	})

	scheduler := asynq.NewScheduler(connection, &asynq.SchedulerOpts{
		Logger:   adapter,
		LogLevel: asynq.InfoLevel,
		Location: time.UTC,

		// A duplicate is the system working, not failing: it means another
		// replica's scheduler won the race, which is the entire reason the
		// uniqueness lock is there. Logging it at error level would train
		// people to ignore this handler.
		PostEnqueueFunc: func(info *asynq.TaskInfo, enqueueErr error) {
			switch {
			case errors.Is(enqueueErr, asynq.ErrDuplicateTask):
				logger.Debug("another replica already enqueued this run")
			case enqueueErr != nil:
				logger.Error("could not enqueue scheduled task", slog.Any("err", enqueueErr))
			default:
				logger.Info("scheduled task enqueued", slog.String("task_type", info.Type))
			}
		},
	})

	return &Runtime{
		server:    server,
		scheduler: scheduler,
		mux:       asynq.NewServeMux(),
		client:    asynq.NewClient(connection),
		logger:    logger,
	}, nil
}

// Handle registers a handler for a task type.
func (r *Runtime) Handle(taskType string, handler asynq.HandlerFunc) {
	r.mux.HandleFunc(taskType, handler)
}

// Schedule registers a recurring task.
func (r *Runtime) Schedule(cronspec string, task *asynq.Task) error {
	if _, err := r.scheduler.Register(cronspec, task); err != nil {
		return fmt.Errorf("jobs: schedule %s: %w", task.Type(), err)
	}
	return nil
}

// EnqueueNow puts one task on the queue immediately.
//
// For work that must not wait for the next cron tick. The transit
// refresh runs every six hours, so on a fresh database — a first deploy,
// a restored environment, a developer's machine after `docker compose
// down -v` — the transits table stays empty for up to six hours and
// /kundli/transits answers 503 the whole time. Nothing is broken and
// every health check is green, which is the worst version of this.
//
// Safe to call on every replica: TransitRefreshTask carries
// asynq.Unique, so a startup enqueue that races the scheduler's is
// deduplicated rather than doubling the load on astro-service.
func (r *Runtime) EnqueueNow(task *asynq.Task) error {
	if _, err := r.client.Enqueue(task); err != nil {
		// A duplicate is the unique lock doing its job, not a failure.
		if errors.Is(err, asynq.ErrDuplicateTask) || errors.Is(err, asynq.ErrTaskIDConflict) {
			r.logger.Info("task already queued", slog.String("type", task.Type()))
			return nil
		}
		return fmt.Errorf("jobs: enqueue %s: %w", task.Type(), err)
	}
	return nil
}

// Start brings up the scheduler and the server without blocking.
func (r *Runtime) Start() error {
	if err := r.server.Start(r.mux); err != nil {
		return fmt.Errorf("jobs: start server: %w", err)
	}
	if err := r.scheduler.Start(); err != nil {
		// The server is already up; stop it rather than leaving a process
		// that consumes tasks nothing will ever schedule.
		r.server.Shutdown()
		return fmt.Errorf("jobs: start scheduler: %w", err)
	}
	return nil
}

// Shutdown stops scheduling first, then drains.
//
// The order is the point. Stopping the server first would leave the
// scheduler enqueueing tasks into a queue nobody is reading, and the last
// few would sit there until the next deploy.
func (r *Runtime) Shutdown() {
	r.scheduler.Shutdown()
	r.server.Shutdown()
	if r.client != nil {
		_ = r.client.Close()
	}
}

// slogAdapter routes asynq's logging into the service's structured JSON.
//
// Without it asynq writes its own text format to stderr, and a log
// pipeline that parses one line per JSON object silently drops every line
// this library emits — so the queue becomes the one component of the
// system with no observability, which is the opposite of what is wanted.
type slogAdapter struct{ logger *slog.Logger }

func (a *slogAdapter) Debug(args ...any) { a.logger.Debug(fmt.Sprint(args...)) }
func (a *slogAdapter) Info(args ...any)  { a.logger.Info(fmt.Sprint(args...)) }
func (a *slogAdapter) Warn(args ...any)  { a.logger.Warn(fmt.Sprint(args...)) }
func (a *slogAdapter) Error(args ...any) { a.logger.Error(fmt.Sprint(args...)) }

// Fatal logs and returns. asynq's interface documents that the process
// will exit; asynq's own library code never calls this method, so nothing
// depends on that promise. Exiting from inside a log call would let a
// dependency terminate a process that has its own shutdown path.
func (a *slogAdapter) Fatal(args ...any) {
	a.logger.Error(fmt.Sprint(args...), slog.Bool("asynq_fatal", true))
}
