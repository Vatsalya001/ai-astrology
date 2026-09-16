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
	"github.com/Vatsalya001/ai-astrology/services/api/internal/birthprofiles"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/charts"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/config"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/httpapi"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/places"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/analytics"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/audit"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/logging"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/observability"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis/ratelimit"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/transits"
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
	// Structured JSON on stdout, the same sink as every other log line,
	// so the pipeline that already ships logs ships events too. No vendor,
	// no SDK, no key — and no ADR needed, because no new technology.
	events := analytics.NewLogEmitter(log)

	userService := users.NewService(queries).WithAnalytics(events)
	sessionDirectory := users.NewSessionDirectory(queries).WithAnalytics(events)

	authService := auth.NewService(auth.ServiceConfig{
		OTP:       auth.NewOTPStore(cache.Client, cfg.OTPTTL, cfg.OTPMaxAttempts),
		Channel:   channel,
		Rotator:   auth.NewRotator(issuer, auth.NewPostgresSessionStore(queries)),
		Users:     userService,
		Audit:     recorder,
		Events:    events,
		Logger:    log,
		OTPLength: cfg.OTPLength,
	})

	freshOTP := auth.NewFreshOTP(
		auth.NewOTPStore(cache.Client, cfg.OTPTTL, cfg.OTPMaxAttempts),
		channel, userService, cfg.OTPLength,
	)
	deleter := users.NewDeleter(queries, sessionDirectory, cfg.AccountDeleteGrace, log).
		WithAnalytics(events)

	limiter := ratelimit.New(cache.Client)

	// Not behind a trusted proxy in development. X-Forwarded-For is
	// client-supplied, and this phase rate-limits per IP — see
	// auth.ClientIP.
	//
	// One variable, read by both the handler and the router's backstop:
	// they each derive the client IP, and if they disagreed the limiter
	// and the stored hash would key on different addresses for the same
	// request.
	const trustProxy = false

	authHandler := auth.NewHandler(auth.HandlerConfig{
		Service:    authService,
		Limiter:    limiter,
		IPSalt:     cfg.IPHashSalt,
		TrustProxy: trustProxy,
		// The refresh cookie is Secure in anything but local plain HTTP.
		Secure:     !cfg.IsDevelopment(),
		RefreshTTL: cfg.JWTRefreshTTL,
		WriteError: httpapi.AuthErrorWriter,
	})

	// ─── Phase 2 ────────────────────────────────────────────────
	placeService := places.NewService(queries)
	profileService := birthprofiles.NewService(queries).WithAnalytics(events)
	chartService := charts.NewService(queries, database.Pool, astroClient, profileService, log).
		WithAnalytics(events)
	transitReader := transits.NewReader(queries)

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
		Limiter:    limiter,
		TrustProxy: trustProxy,

		BirthProfiles: birthprofiles.NewHandler(
			profileService, placeAdapter{placeService}, httpapi.AuthErrorWriter),
		Places: places.NewHandler(placeService, httpapi.AuthErrorWriter),
		Charts: charts.NewHandler(chartService, httpapi.AuthErrorWriter),
		// The chart service supplies the natal Moon sign the gochara is
		// rotated onto. Passed as an interface the transits package
		// declares, so neither domain imports the other.
		Transits:     transits.NewHandler(transitReader, chartService, httpapi.AuthErrorWriter),
		ProfileOwner: profileService,
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

// placeAdapter narrows the places service to what birthprofiles needs.
//
// The two packages describe the same row with different structs on
// purpose: birthprofiles needs four fields and should not acquire an
// opinion about population ranking or country codes. The adapter lives
// here, in the composition root, so neither domain imports the other.
type placeAdapter struct{ svc *places.Service }

func (a placeAdapter) Get(ctx context.Context, id int32) (birthprofiles.Place, error) {
	place, err := a.svc.Get(ctx, id)
	if err != nil {
		return birthprofiles.Place{}, err
	}
	return birthprofiles.Place{
		Name:      place.Name,
		Latitude:  place.Latitude,
		Longitude: place.Longitude,
		Timezone:  place.Timezone,
	}, nil
}
