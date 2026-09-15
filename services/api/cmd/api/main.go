// Command api is the HTTP entrypoint for api-service.
//
// api-service is the only public surface of this system and the only
// writer to the database. astro-service and ai-service sit behind it on
// the internal network and are never reachable from the internet.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/config"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/httpapi"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/audit"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/logging"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/observability"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis/ratelimit"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/users"
)

func main() {
	if err := run(); err != nil {
		// Config loading happens before the logger exists, so this must
		// go to stderr directly. It is also the single most common
		// startup failure, so it needs to be legible.
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logging.New(cfg.LogLevel, "api")
	log.Info("starting api-service",
		slog.String("env", cfg.Env),
		slog.String("version", httpapi.Version),
		slog.Int("port", cfg.Port),
	)

	// Signal-aware root context: Ctrl-C and SIGTERM both begin shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Observability first, so anything that fails afterwards is captured.
	// Both are no-ops when unconfigured, which is the normal case in
	// development — the instrumentation still runs, so these paths cannot
	// rot between releases.
	obsCfg := observability.Config{
		ServiceName:  "api",
		Version:      httpapi.Version,
		Environment:  cfg.Env,
		OTLPEndpoint: cfg.OTLPEndpoint,
		SampleRatio:  cfg.OTLPSampleRatio,
	}

	shutdownTracing, err := observability.Init(ctx, obsCfg)
	if err != nil {
		return fmt.Errorf("init tracing: %w", err)
	}
	defer func() { _ = shutdownTracing(context.Background()) }()

	shutdownSentry, err := observability.InitSentry(obsCfg, cfg.SentryDSN)
	if err != nil {
		return fmt.Errorf("init sentry: %w", err)
	}
	defer func() { _ = shutdownSentry(context.Background()) }()

	if cfg.OTLPEndpoint != "" {
		log.Info("tracing enabled", slog.String("otlp_endpoint", cfg.OTLPEndpoint))
	}
	if cfg.SentryDSN != "" {
		log.Info("error reporting enabled")
	}

	database, err := db.Connect(ctx, cfg.DatabaseURL, cfg.DatabaseMaxConns)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer database.Close()
	log.Info("connected to postgres")

	cache, err := redis.Connect(ctx, cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}
	defer func() { _ = cache.Close() }()
	log.Info("connected to redis")

	// The Python services are NOT dialled at startup. They are probed by
	// /health instead, because this service must keep serving cached
	// charts and the entire non-AI product when either of them is down.
	astroClient, err := clients.NewAstro(cfg.AstroServiceURL, cfg.InternalToken, cfg.ServiceTimeout)
	if err != nil {
		return fmt.Errorf("build astro client: %w", err)
	}
	aiClient, err := clients.NewAI(cfg.AIServiceURL, cfg.InternalToken, cfg.ServiceTimeout)
	if err != nil {
		return fmt.Errorf("build ai client: %w", err)
	}

	// ─── Auth (Phase 1) ──────────────────────────────────────────
	//
	// Constructed here rather than inside the router so a misconfigured
	// secret or an unusable channel is a startup failure with a named
	// cause, not a 500 on the first login attempt.
	issuer, err := auth.NewIssuer(cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
	if err != nil {
		return fmt.Errorf("build token issuer: %w", err)
	}

	channel, err := auth.NewChannel(auth.ChannelConfig{
		Channel:  cfg.AuthChannel,
		IsProd:   cfg.IsProduction(),
		SMTPHost: cfg.SMTPHost,
		SMTPPort: cfg.SMTPPort,
		SMTPFrom: cfg.SMTPFrom,
	}, log)
	if err != nil {
		return fmt.Errorf("build auth channel: %w", err)
	}
	log.Info("auth channel ready", slog.String("channel", channel.ID()))

	queries := dbgen.New(database.Pool)
	recorder := audit.NewRecorder(queries, log)
	userService := users.NewService(queries)
	sessionDirectory := users.NewSessionDirectory(queries)

	authService := auth.NewService(auth.ServiceConfig{
		OTP:       auth.NewOTPStore(cache.Client, cfg.OTPTTL, cfg.OTPMaxAttempts),
		Channel:   channel,
		Rotator:   auth.NewRotator(issuer, auth.NewPostgresSessionStore(queries)),
		Users:     userService,
		Audit:     recorder,
		Logger:    log,
		OTPLength: cfg.OTPLength,
	})

	freshOTP := auth.NewFreshOTP(
		auth.NewOTPStore(cache.Client, cfg.OTPTTL, cfg.OTPMaxAttempts),
		channel, userService, cfg.OTPLength,
	)
	deleter := users.NewDeleter(queries, sessionDirectory, cfg.AccountDeleteGrace, log)

	google := auth.NewGoogle(auth.GoogleConfig{
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		RedirectURL:  fmt.Sprintf("http://localhost:%d/api/v1/auth/oauth/google/callback", cfg.Port),
	}, cache.Client)
	if cfg.OAuthConfigured() {
		log.Info("google oauth configured")
	} else {
		// Stated at startup rather than discovered when someone clicks
		// the button and gets an error.
		log.Info("google oauth NOT configured — the route will report it as unavailable")
	}

	authHandler := auth.NewHandler(auth.HandlerConfig{
		Service: authService,
		Limiter: ratelimit.New(cache.Client),
		IPSalt:  cfg.IPHashSalt,
		// Not behind a trusted proxy in development. X-Forwarded-For is
		// client-supplied, and this phase rate-limits per IP — see
		// auth.ClientIP.
		TrustProxy: false,
		// The refresh cookie is Secure in anything but local plain HTTP.
		Secure:     !cfg.IsDevelopment(),
		RefreshTTL: cfg.JWTRefreshTTL,
		WriteError: httpapi.AuthErrorWriter,
	})

	handler := httpapi.NewRouter(httpapi.Deps{
		Config:     cfg,
		DB:         database,
		Redis:      cache,
		Astro:      astroClient,
		AI:         aiClient,
		Auth:       authHandler,
		AuthIssuer: issuer,
		Users:      users.NewHandler(userService, sessionDirectory, httpapi.AuthErrorWriter),
		Sessions:   sessionDirectory,
		Deleter:    deleter,
		Exporter:   users.NewExporter(queries),
		FreshOTP:   freshOTP,
		Google:     google,
		Linker:     userService,
	})

	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: handler,

		// ReadHeaderTimeout guards against Slowloris. The other two are
		// generous because Phase 5 streams SSE through this server and a
		// short WriteTimeout would sever long responses mid-stream.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      0, // no write deadline: required for SSE
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	// Graceful shutdown: stop accepting new connections, let in-flight
	// requests finish. 20s is longer than the 30s request timeout would
	// suggest is needed because SSE streams are drained here too.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	log.Info("shutdown complete")
	return nil
}
