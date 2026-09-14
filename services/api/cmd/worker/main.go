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
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/logging"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis"
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
	deleter := users.NewDeleter(
		queries,
		users.NewSessionDirectory(queries),
		cfg.AccountDeleteGrace,
		log,
	)

	log.Info("worker ready",
		slog.String("jobs", "hard-delete"),
		slog.Duration("delete_grace", cfg.AccountDeleteGrace),
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
