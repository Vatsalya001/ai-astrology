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
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/logging"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis"
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

	log.Info("worker ready — no jobs registered yet (Phase 0)")

	// Heartbeat until signalled. This keeps the process honest: it proves
	// config, database and Redis all work in the worker's own context,
	// not just the API's.
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("worker shutdown complete")
			return nil
		case <-ticker.C:
			log.Debug("worker heartbeat")
		}
	}
}
