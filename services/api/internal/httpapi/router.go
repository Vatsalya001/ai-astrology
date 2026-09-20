package httpapi

import (
	"net/http"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/ailogs"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/birthprofiles"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/charts"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/config"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/pdf"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/places"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis/ratelimit"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/shares"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/transits"
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

	// ─── Phase 2 ────────────────────────────────────────────────
	// Nil until wired, and nil means the routes are simply not mounted —
	// a route that exists and 500s is worse than one that 404s.
	BirthProfiles *birthprofiles.Handler
	Places        *places.Handler
	Charts        *charts.Handler
	Transits      *transits.Handler

	// PDF is nil until the queue and object storage are wired. Nil means
	// the two PDF routes are simply not mounted — a route that exists
	// and 500s is worse than one that 404s.
	PDF *pdf.Handler

	// Shares mounts the share-link routes, including the one public
	// route in the astrology subtree.
	Shares *shares.Handler

	// ─── Phase 4 ────────────────────────────────────────────────
	// AILogs serves the admin AI views. Nil means they are not mounted,
	// which is the right default: an admin surface that exists without a
	// backing service is a 500 waiting behind an auth check.
	AILogs *ailogs.Handler
	// ProfileOwner gates the chart and transit subtrees. Required
	// whenever those are mounted; see mountAstrology.
	ProfileOwner ProfileOwnership

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
	r := newChiRouter(d)

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

// newChiRouter builds the route table.
//
// Split from NewRouter so the route table can be WALKED — otelhttp's
// wrapper hides the chi.Routes interface, and the ownership test needs to
// enumerate every mounted route rather than trust a hand-written list of
// paths that would go stale the first time somebody adds an endpoint.
func newChiRouter(d Deps) chi.Router {
	// A programming error, so it fails at construction. The alternative is
	// a nil-pointer panic on whichever request first reaches a limited
	// route — in production, at an unpredictable moment, with a stack
	// trace instead of a reason.
	/*
	  PDF is in this check for a sharper reason than the other two.

	  `Deps.Limiter` is a concrete *ratelimit.Limiter, and pdf.Create takes
	  an INTERFACE. A nil pointer assigned to an interface produces a
	  non-nil interface holding a nil value, so the handler's own
	  `limiter != nil` guard would pass and `Allow` would be called on a
	  nil receiver — a panic on the first download, in production, rather
	  than a message here.

	  That guard is still worth keeping in the handler: it is what lets a
	  test mount the route unlimited. This is what makes sure the real
	  router never does.
	*/
	if (d.Auth != nil || d.Users != nil || d.PDF != nil || d.Shares != nil) && d.Limiter == nil {
		panic("httpapi: Deps.Limiter is required when the auth, users, PDF or share routes are mounted")
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
			mountAstrology(r, d)
			mountAdmin(r, d)
		}

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

// mountAstrology registers the Phase 2 routes.
//
// Everything here is behind Authenticate, including the place search.
// That is a decision rather than an oversight: onboarding happens after
// sign-in in this product, so no legitimate anonymous caller needs the
// gazetteer, and leaving it open would publish a 200k-row dataset behind
// a prefix-scan endpoint for anyone who felt like mirroring it.
func mountAstrology(r chi.Router, d Deps) {
	if d.AuthIssuer == nil {
		return
	}
	authenticate := auth.Authenticate(d.AuthIssuer, AuthMiddlewareErrorWriter)

	if d.Places != nil {
		r.Group(func(r chi.Router) {
			r.Use(authenticate)
			r.Get("/places/search", d.Places.Search)
		})
	}

	// A programming error, so it fails at construction rather than by
	// serving somebody else's birth profile on the first request.
	if (d.BirthProfiles != nil || d.Charts != nil || d.Transits != nil) && d.ProfileOwner == nil {
		panic("httpapi: Deps.ProfileOwner is required when any profile-scoped route is mounted")
	}

	if d.BirthProfiles == nil {
		return
	}

	r.Route("/birth-profiles", func(r chi.Router) {
		// Mounted as a group rather than per-route, so a new endpoint
		// cannot be added outside the guard by forgetting a line.
		r.Use(authenticate)

		// Collection routes. No {id}, so nothing to own: both are scoped
		// to the caller by the service.
		r.Post("/", d.BirthProfiles.Create)
		r.Get("/", d.BirthProfiles.List)

		// Everything addressing a specific profile goes behind the
		// ownership check, as a group. The handlers below read the
		// verified ID out of the request context and never out of the
		// URL, so none of them can be reached with an unchecked one.
		r.Group(func(r chi.Router) {
			r.Use(RequireProfileOwnership(d.ProfileOwner, "id"))

			r.Get("/{id}", d.BirthProfiles.Get)
			r.Patch("/{id}", d.BirthProfiles.Update)
			r.Delete("/{id}", d.BirthProfiles.Delete)
			r.Get("/{id}/versions", d.BirthProfiles.Versions)
		})
	})

	// ─── Charts and dashas ──────────────────────────────────────
	//
	// Every route addresses a birth profile, so the whole subtree sits
	// behind the ownership check — there is no collection endpoint here
	// to leave outside it.
	/*
	  The print route, mounted OUTSIDE `/charts` — not inside it.

	  Its caller is headless Chrome on the PDF worker, which has no
	  session, so it cannot sit under `authenticate`. The single-use
	  token it carries IS the authorisation: redeeming it yields the
	  user and the profile the worker scoped it to.

	  It is a sibling rather than a child because `/charts` applies
	  `authenticate` with `r.Use` to its whole subtree. A `/charts/print`
	  registered inside that block would be authenticated no matter what
	  the comment above it claimed, and chi cannot mount `/charts/print`
	  on the parent while `/charts` is itself mounted.

	  There is no profile id in the path or the query — only `?token=`.
	  So "trust the token but read the id from the request", which turns
	  any valid token into a reader for every chart, is not a mistake
	  this route is able to make.
	*/
	if d.Charts != nil {
		r.Get("/print/chart", d.Charts.Print)
	}

	/*
	  The shared-chart route, public for the same reason and mounted the
	  same way — a sibling of `/charts`, not a child of it.

	  The person opening a shared link has no account here and is not
	  going to make one to look at their nephew's chart. The opaque token
	  in the path is the authorisation, and the server resolves it to a
	  user and a profile: the URL carries no birth details and no ids,
	  which is what the phase's security checklist requires of it.

	  Rate-limited on the client rather than on a user, because there is
	  no user. See shares.ResolveLimit.
	*/
	if d.Shares != nil {
		r.Get("/shared/{token}", d.Shares.View(d.Limiter))
	}

	if d.Charts != nil {
		r.Route("/charts", func(r chi.Router) {
			r.Use(authenticate)

			// Group, NOT r.Use on the Route above — and this is not style.
			//
			// Middleware added with r.Use on a sub-router runs BEFORE chi
			// matches the route within it, so chi.URLParam returns "" and
			// the ownership check sees an unparseable ID on every request.
			// It fails closed, which is the right direction, but it 404s
			// the owner too. Group attaches the middleware to the endpoint
			// chain instead, after the match, where the parameter exists.
			//
			// Found by TestEveryProfileScopedRouteRefusesAStranger, which
			// asserts the OWNER does not get 404 — the half of that test
			// that looked like belt-and-braces.
			r.Group(func(r chi.Router) {
				r.Use(RequireProfileOwnership(d.ProfileOwner, "birthProfileId"))

				r.Get("/{birthProfileId}", d.Charts.Get)
				r.Get("/{birthProfileId}/dashas", d.Charts.Dashas)
				r.Get("/{birthProfileId}/dashas/current", d.Charts.Current)

				// The only route here that calls astro-service
				// unconditionally, so it carries its own per-user limit on
				// top of the global per-IP backstop.
				r.Post("/{birthProfileId}/recompute", d.Charts.Recompute(d.Limiter))

				/*
				  PDF generation, inside the ownership group.

				  Both routes address a birth profile, so both belong here
				  — and both read the profile from the context the
				  middleware filled rather than from the URL.

				  The polling route needs one thing this group cannot give
				  it: the job id in its path is not a profile, so the
				  middleware says nothing about who owns it. That check is
				  in the status key, which is composed from the
				  authenticated user. See pdf/status.go.
				*/
				if d.PDF != nil {
					// Limited, and visibly so. One request here starts a
					// browser for up to ninety seconds, which makes it the
					// most expensive thing an authenticated user can ask
					// this service to do — more so than the recompute route
					// on the line above.
					r.Post("/{birthProfileId}/pdf", d.PDF.Create(d.Limiter))
					r.Get("/{birthProfileId}/pdf/{jobId}", d.PDF.Status)
				}

				/*
				  Share management, for the OWNER. The public half is
				  mounted above, outside this subtree entirely.

				  Creating is limited: each call mints a bearer credential
				  to a birth chart, and the live-links cap does not bound
				  that on its own — revoke-and-remint in a loop stays
				  under the cap while producing unbounded tokens.
				*/
				if d.Shares != nil {
					r.Post("/{birthProfileId}/shares", d.Shares.Create(d.Limiter))
					r.Get("/{birthProfileId}/shares", d.Shares.List)
					r.Delete("/{birthProfileId}/shares/{shareId}", d.Shares.Revoke)
				}
			})
		})
	}

	// ─── Transits ───────────────────────────────────────────────
	if d.Transits != nil {
		r.Route("/astrology", func(r chi.Router) {
			r.Use(authenticate)

			// Global. No profile in the path and nothing to own: these
			// positions are identical for every person alive at that
			// instant, which is what makes the table free of personal data
			// in the first place.
			r.Get("/transits", d.Transits.Global)

			r.Group(func(r chi.Router) {
				r.Use(RequireProfileOwnership(d.ProfileOwner, "birthProfileId"))
				r.Get("/transits/{birthProfileId}", d.Transits.Natal)
			})
		})
	}
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

// mountAdmin mounts the SUPER_ADMIN-only AI views. PHASE-04 §10.
//
// ── Why the whole subtree is one group ──
//
// `Authenticate` then `RequireRole` are applied to the group rather than
// to each route. A per-route check is a check someone forgets when they
// add the sixth endpoint, and the thing being protected here is every
// user's cost and safety history.
//
// ── Why SUPER_ADMIN and not ADMIN ──
//
// §10 says SUPER_ADMIN, and the reason is the playground: it spends real
// money against the production provider on demand. `auth.RequireRole`
// compares roles exactly with no hierarchy, so naming only SUPER_ADMIN
// here genuinely excludes ADMIN rather than relying on an ordering that
// does not exist.
func mountAdmin(r chi.Router, d Deps) {
	if d.AILogs == nil || d.AuthIssuer == nil {
		return
	}

	authenticate := auth.Authenticate(d.AuthIssuer, AuthMiddlewareErrorWriter)

	r.Route("/admin/ai", func(r chi.Router) {
		r.Use(authenticate)
		r.Use(auth.RequireRole(AuthMiddlewareErrorWriter, auth.RoleSuperAdmin))

		r.Get("/config", d.AILogs.GetConfig)
		r.Get("/usage", d.AILogs.GetUsage)
		r.Get("/incidents", d.AILogs.GetIncidents)
		r.Post("/test", d.AILogs.Playground)
	})
}
