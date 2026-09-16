// Command worker runs background jobs for api-service.
//
// Deliberately a separate binary from cmd/api, even though Phase 0 has no
// jobs to run yet. Two reasons:
//
//   - Workloads differ. Phase 3's PDF worker needs headless Chromium in
//     its image; the API binary must stay small. Splitting later means
//     re-plumbing config, logging and shutdown at the moment you are
//     already busy.
//   - Scaling differs. Workers scale on queue depth, the API on request
//     rate.
//
// Phase 2 adds the transit refresh job, Phase 3 the PDF renderer,
// Phase 6 the daily-horoscope generator and memory decay.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/config"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/analytics"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/jobs"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/logging"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/transits"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/users"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logging.New(cfg.LogLevel, "worker")
	log.Info("starting worker", slog.String("env", cfg.Env))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Connect(ctx, cfg.DatabaseURL, cfg.DatabaseMaxConns)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer database.Close()

	cache, err := redis.Connect(ctx, cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}
	defer func() { _ = cache.Close() }()

	queries := dbgen.New(database.Pool)
	// The worker is where account_deleted actually fires — the API marks
	// an account for deletion, this process is what removes it.
	events := analytics.NewLogEmitter(log)

	deleter := users.NewDeleter(
		queries,
		users.NewSessionDirectory(queries).WithAnalytics(events),
		cfg.AccountDeleteGrace,
		log,
	).WithAnalytics(events)

	// ─── The asynq side ─────────────────────────────────────────
	//
	// Two scheduling mechanisms in one process, deliberately. The
	// hard-delete pass below is a database-only sweep that is idempotent
	// and harmless to run on every replica at once, so a ticker is the
	// right size for it and rewriting it would be a regression surface
	// with no phase-2 benefit. The transit refresh calls astro-service,
	// so it needs retry with backoff and must run ONCE across the fleet.
	// See internal/platform/jobs.
	astro, err := clients.NewAstro(cfg.AstroServiceURL, cfg.InternalToken, cfg.ServiceTimeout)
	if err != nil {
		return fmt.Errorf("astro client: %w", err)
	}

	runtime, err := jobs.NewRuntime(cfg.RedisURL, cfg.WorkerConcurrency, log)
	if err != nil {
		return err
	}

	refresher := transits.NewRefresher(queries, astro, jobs.TransitRefreshInterval, log)
	if err := transits.NewRefreshHandler(
		refresher, jobs.TransitRetention, time.Now, log,
	).Register(runtime); err != nil {
		return err
	}

	if err := runtime.Start(); err != nil {
		return err
	}
	defer runtime.Shutdown()

	// One refresh immediately, not only on the next six-hourly tick.
	//
	// The same reasoning as the hard-delete sweep below: a process that
	// has just started should not make the product wait for a cron. On a
	// fresh database the transits table is EMPTY, so until the next tick
	// /kundli/transits answers 503 and Sade Sati — the most-asked
	// question in the product — has nothing behind it, for up to six
	// hours, with every health check green the whole time.
	//
	// Not fatal if it fails. The cron will come round, and a worker that
	// refuses to boot because one enqueue failed is worse than a worker
	// that starts with slightly stale transits.
	if err := runtime.EnqueueNow(jobs.NewTransitRefreshTask()); err != nil {
		log.Error("could not enqueue the startup transit refresh",
			slog.String("error", err.Error()),
			slog.String("consequence", "transits stay as they are until the next cron tick"))
	}

	log.Info("worker ready",
		slog.String("jobs", "hard-delete, transits:refresh"),
		slog.Duration("delete_grace", cfg.AccountDeleteGrace),
		slog.String("transit_cron", jobs.TransitRefreshCron),
		slog.Int("concurrency", cfg.WorkerConcurrency),
	)

	// Hourly, not continuously. The grace window is measured in days, so
	// the worst case is an account deleted an hour later than the earliest
	// moment it could have been — which nobody can perceive, and which
	// costs far less than a tight loop scanning an index all day.
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	// Once at startup too, so a deploy after downtime does not wait an
	// hour before honouring deletions that came due meanwhile.
	runHardDeletes(ctx, deleter, log)

	for {
		select {
		case <-ctx.Done():
			log.Info("worker shutdown complete")
			return nil
		case <-ticker.C:
			runHardDeletes(ctx, deleter, log)
		}
	}
}

// runHardDeletes executes the pass and logs the outcome.
//
// A failure is logged and the loop continues: a wedged deletion must not
// stop the worker, or one bad row halts everybody else's.
func runHardDeletes(ctx context.Context, deleter *users.Deleter, log *slog.Logger) {
	deleted, err := deleter.RunHardDeletes(ctx)
	if err != nil {
		log.ErrorContext(ctx, "hard delete pass failed", slog.Any("err", err))
		return
	}
	if deleted > 0 {
		// Only when something happened. An hourly "deleted 0 accounts" is
		// noise that trains people to ignore the line.
		log.InfoContext(ctx, "hard delete pass complete", slog.Int("accounts_deleted", deleted))
	}
}
