package httpapi

import (
	"net/http"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/config"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis/ratelimit"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/users"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Version is stamped at build time via -ldflags.
var Version = "dev"

// Deps is everything the HTTP layer needs.
//
// Passing one struct rather than N arguments keeps the signature stable
// as domain services arrive in Phase 1 and beyond.
type Deps struct {
	Config *config.Config
	DB     *db.DB
	Redis  *redis.Client
	Astro  *clients.Astro
	AI     *clients.AI

	// Auth is nil only in tests that exercise the operational endpoints.
	// When nil the /auth routes are simply not mounted, which is honest:
	// a route that exists and 500s is worse than one that 404s.
	Auth       *auth.Handler
	AuthIssuer *auth.Issuer
	Users      *users.Handler
	Sessions   *users.SessionDirectory
	Deleter    *users.Deleter
	Exporter   *users.Exporter
	FreshOTP   *auth.FreshOTP
	// TrustProxy must match what the auth handler was built with. Both
	// derive the client IP; if they disagreed, the limiter and the stored
	// hash would key on different addresses for the same request.
	TrustProxy bool

	// Limiter drives the per-IP backstop and the route-specific limits.
	//
	// Required whenever Auth or Users is mounted — every one of those
	// routes depends on it, and a nil here would be a nil-pointer panic on
	// the first request rather than at startup. NewRouter checks.
	Limiter *ratelimit.Limiter
}

// NewRouter builds the HTTP handler.
//
// Middleware order matters and is deliberate:
//
//	Recover      — outermost, so it catches panics from everything below
//	TraceID      — must run before AccessLog so the log line has an ID
//	AccessLog
//	SecurityHeaders
//	CORS
//	Timeout      — innermost, bounds the handler itself
func NewRouter(d Deps) http.Handler {
	// A programming error, so it fails at construction. The alternative is
	// a nil-pointer panic on whichever request first reaches a limited
	// route — in production, at an unpredictable moment, with a stack
	// trace instead of a reason.
	if (d.Auth != nil || d.Users != nil) && d.Limiter == nil {
		panic("httpapi: Deps.Limiter is required when the auth or users routes are mounted")
	}

	r := chi.NewRouter()

	r.Use(Recover)
	r.Use(TraceID)
	r.Use(AccessLog)
	r.Use(SecurityHeaders)

	// NOTE: chi's middleware.RealIP is deliberately NOT used. It rewrites
	// r.RemoteAddr from X-Forwarded-For / X-Real-IP whether or not the
	// infrastructure actually sets them, which makes the client IP
	// attacker-controlled (GHSA-3fxj-6jh8-hvhx and related).
	//
	// That matters here specifically: Phase 1 rate-limits per IP and
	// stores a salted hash of it. A spoofable IP would let an attacker
	// bypass the limiter with a header. Phase 1 adds a trusted-proxy-aware
	// resolver that only honours the header from known proxy addresses.

	r.Use(cors.Handler(cors.Options{
		// Restricted to the configured web origin. Not "*" — this API
		// will carry credentials from Phase 1 onward.
		AllowedOrigins:   []string{d.Config.WebURL},
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Trace-Id", "Idempotency-Key"},
		ExposedHeaders:   []string{"X-Trace-Id", "Retry-After"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Use(middleware.Timeout(30 * time.Second))

	// ─── Operational endpoints ──────────────────────────────────
	r.Get("/health", HealthHandler("api", Version, probers(d)))
	r.Get("/ready", ReadyHandler("api", Version))

	// ─── API v1 ─────────────────────────────────────────────────
	r.Route("/api/v1", func(r chi.Router) {
		// The backstop, ahead of every route below. Route-specific limits
		// are narrower and live in their handlers; this one exists so an
		// endpoint added later is limited without anyone remembering to.
		if d.Limiter != nil {
			r.Use(GlobalThrottle(d.Limiter, d.TrustProxy, d.Config.IPHashSalt))
		}

		r.Get("/meta", metaHandler(d.Config))

		if d.Auth != nil {
			mountAuth(r, d)
		}

		// Phase 2 mounts /birth-profiles, /charts, /places.
		// Phase 5 mounts /ai and /conversations.
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, http.StatusNotFound, CodeNotFound, "Not found.", nil)
	})

	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, http.StatusMethodNotAllowed, CodeBadRequest, "Method not allowed.", nil)
	})

	// otelhttp wraps the whole router: it extracts W3C traceparent from
	// inbound requests and starts a server span. Outermost so the span
	// covers every middleware below it, and so TraceID can read the span
	// ID that otelhttp just established.
	//
	// A no-op tracer provider is installed when OTEL_EXPORTER_OTLP_ENDPOINT
	// is unset, so this costs almost nothing in development.
	return otelhttp.NewHandler(r, "api",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			// Method + path pattern, never the raw path: a path can carry
			// an ID, and high-cardinality span names are useless anyway.
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				return r.Method + " " + rc.RoutePattern()
			}
			return r.Method
		}),
	)
}

// probers lists every dependency the health endpoint reports on.
//
// Postgres and Redis are critical: without them this service cannot do
// anything useful. The Python services are not — Phase 2 and Phase 5
// explicitly require that cached charts still serve when astro-service is
// down, and that the whole non-AI product still works when ai-service is
// down. Marking them non-critical here encodes that design decision.
func probers(d Deps) []Prober {
	list := []Prober{
		{Name: "postgres", Critical: true, Probe: plain(d.DB.Ping)},
		{Name: "redis", Critical: true, Probe: plain(d.Redis.Ping)},
		{Name: "astro", Critical: false, Probe: plain(d.Astro.Health)},
		// The only prober reporting a detail: which model backend is
		// configured. See clients.AI.Health for why that belongs here.
		{Name: "ai", Critical: false, Probe: d.AI.Health},
	}

	// Storage and mail are reported only when a probe URL is configured.
	// Neither is critical: object storage matters from Phase 3 (PDFs)
	// and mail from Phase 1 (OTP), and until then their absence should
	// not colour the service's health.
	if url := d.Config.StorageHealthURL; url != "" {
		list = append(list, Prober{Name: "storage", Critical: false, Probe: plain(httpProbe(url))})
	}
	if url := d.Config.MailHealthURL; url != "" {
		list = append(list, Prober{Name: "mail", Critical: false, Probe: plain(httpProbe(url))})
	}

	return list
}

// metaHandler exposes non-sensitive runtime facts the web app needs:
// which features are switched on, and which build is running.
func metaHandler(cfg *config.Config) http.HandlerFunc {
	type features struct {
		AIChat        bool `json:"ai_chat"`
		Voice         bool `json:"voice"`
		Astrologers   bool `json:"astrologers"`
		Compatibility bool `json:"compatibility"`
		Payments      bool `json:"payments"`
		PDF           bool `json:"pdf"`
	}
	type meta struct {
		Service  string   `json:"service"`
		Version  string   `json:"version"`
		Env      string   `json:"env"`
		Phase    string   `json:"phase"`
		Features features `json:"features"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, meta{
			Service: "api",
			Version: Version,
			Env:     cfg.Env,
			Phase:   "0 — Foundation",
			Features: features{
				AIChat:        cfg.FeatureAIChat,
				Voice:         cfg.FeatureVoice,
				Astrologers:   cfg.FeatureAstrologers,
				Compatibility: cfg.FeatureCompatibility,
				Payments:      cfg.FeaturePayments,
				PDF:           cfg.FeaturePDF,
			},
		})
	}
}

// mountAuth registers the authentication routes.
//
// Split out so the route table is readable in one screen — which is where
// a route accidentally mounted outside the authenticated group would be
// spotted.
func mountAuth(r chi.Router, d Deps) {
	authenticate := auth.Authenticate(d.AuthIssuer, AuthMiddlewareErrorWriter)

	r.Route("/auth", func(r chi.Router) {
		// Public. These are how someone with no token gets one, so they
		// cannot sit behind Authenticate. Rate limiting is applied inside
		// each handler rather than as middleware, because the subject
		// differs per route — identifier here, token there.
		r.Post("/otp/request", d.Auth.RequestOTP)
		r.Post("/otp/verify", d.Auth.VerifyOTP)
		r.Post("/refresh", d.Auth.Refresh)

		// Logout takes the refresh token, so it does not require a valid
		// ACCESS token — an expired session must still be closable.
		r.Post("/logout", d.Auth.Logout)

		// logout-all does require one. It is the "someone has my account"
		// button and must not be triggerable by whoever holds a single
		// stolen refresh token.
		r.Group(func(r chi.Router) {
			r.Use(authenticate)
			r.Post("/logout-all", d.Auth.LogoutAll(d.Sessions))
		})
	})

	if d.Users == nil {
		return
	}

	r.Route("/users", func(r chi.Router) {
		// Every route below this line requires a valid access token.
		// Mounted as a group rather than per-route so a new endpoint
		// cannot be added outside the guard by forgetting a line.
		r.Use(authenticate)

		r.Get("/me", d.Users.Me)
		r.Patch("/me", d.Users.PatchMe)
		r.Get("/me/preferences", d.Users.Preferences)
		r.Patch("/me/preferences", d.Users.PatchPreferences)
		r.Get("/me/sessions", d.Users.ListSessions)
		r.Delete("/me/sessions/{id}", d.Users.RevokeSession)

		// Deletion and export additionally require a FRESH OTP, checked
		// inside each handler. A valid access token is not enough for
		// either: fifteen minutes of validity and an unlocked laptop is
		// not the bar for erasing an account.
		if d.Deleter != nil && d.FreshOTP != nil {
			r.Post("/me/challenge", d.Users.Challenge(d.FreshOTP, d.Limiter))
			r.Post("/me/delete", d.Users.RequestDeletion(d.Deleter, d.FreshOTP))
			r.Post("/me/delete/cancel", d.Users.CancelDeletion(d.Deleter))
		}
		if d.Exporter != nil && d.FreshOTP != nil {
			r.Get("/me/export", d.Users.Export(d.Exporter, d.FreshOTP))
		}
	})
}

// authErrorWriter adapts WriteError to the signature the auth package
// declares, so that package does not import httpapi.
func authErrorWriter(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error) {
	WriteError(w, r, status, ErrorCode(code), message, cause)
}

// AuthMiddlewareErrorWriter is the same adapter for the middleware, whose
// signature carries no cause.
func AuthMiddlewareErrorWriter(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	WriteError(w, r, status, ErrorCode(code), message, nil)
}

// AuthErrorWriter is exported for wiring in main.
var AuthErrorWriter = authErrorWriter
