package httpapi

import (
	"net/http"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/config"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
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
		r.Get("/meta", metaHandler(d.Config))

		// Phase 1 mounts /auth and /users here.
		// Phase 2 mounts /birth-profiles, /charts, /places.
		// Phase 5 mounts /ai and /conversations.
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, http.StatusNotFound, CodeNotFound, "Not found.", nil)
	})

	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, http.StatusMethodNotAllowed, CodeBadRequest, "Method not allowed.", nil)
	})

	return r
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
		{Name: "postgres", Critical: true, Probe: d.DB.Ping},
		{Name: "redis", Critical: true, Probe: d.Redis.Ping},
		{Name: "astro", Critical: false, Probe: d.Astro.Health},
		{Name: "ai", Critical: false, Probe: d.AI.Health},
	}

	// Storage and mail are reported only when a probe URL is configured.
	// Neither is critical: object storage matters from Phase 3 (PDFs)
	// and mail from Phase 1 (OTP), and until then their absence should
	// not colour the service's health.
	if url := d.Config.StorageHealthURL; url != "" {
		list = append(list, Prober{Name: "storage", Critical: false, Probe: httpProbe(url)})
	}
	if url := d.Config.MailHealthURL; url != "" {
		list = append(list, Prober{Name: "mail", Critical: false, Probe: httpProbe(url)})
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
