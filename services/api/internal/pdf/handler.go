package pdf

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis/ratelimit"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/reqctx"
)

// Enqueuer puts a task on the queue. Declared here so the handler does
// not depend on the whole jobs runtime, and so a test can count.
type Enqueuer interface {
	Enqueue(task *asynq.Task) error
}

// Limiter is the rate limiter, declared by the consumer so this package
// names only the one method it uses.
type Limiter interface {
	Allow(ctx context.Context, rule ratelimit.Rule, subject string) (ratelimit.Result, error)
}

/*
RenderLimit is the tightest limit in the service, and it should be.

One request here starts a browser process on a worker for up to ninety
seconds. That is by a wide margin the most expensive thing an
authenticated user can ask this API to do — the recompute route, which
already carries a limit for being "the only route that calls
astro-service unconditionally", costs a fraction of it.

Without a limit it is a button that converts one account into as many
concurrent Chrome processes as the fleet will start, and a PDF queue
full of one person's renders is a download that never arrives for
anybody else. The specification names it directly: "Rate limit PDF
generation (CPU-expensive and trivially abusable)."

Ten an hour is far above real use — a person downloads their chart
once, and perhaps once more per relative — and far below what it takes
to hurt anything.
*/
var RenderLimit = ratelimit.Rule{
	Name:   "pdf_render",
	Max:    10,
	Window: time.Hour,
}

// JobStatus is the store surface the handler needs — both halves,
// unlike the worker, which only writes.
//
// An interface for the same reason as StatusWriter: these tests assert
// on status codes and authorisation, neither of which is a property of
// Redis, and requiring a container to check them would mean they ran
// rarely.
type JobStatus interface {
	StatusWriter
	Get(ctx context.Context, userID, jobID uuid.UUID) (Status, error)
}

// Handler is the request side of PDF generation.
//
// Both routes sit under `/charts/{birthProfileId}`, behind
// RequireProfileOwnership — so the profile in the path has already been
// proved to belong to the caller before either method runs, and both
// read it from the context rather than the URL.
type Handler struct {
	queue    Enqueuer
	status   JobStatus
	writeErr HTTPErrorWriter
	logger   *slog.Logger
}

// HTTPErrorWriter is httpapi.WriteError, injected so this package does
// not import httpapi.
type HTTPErrorWriter func(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error)

func NewHandler(queue Enqueuer, status JobStatus, writeErr HTTPErrorWriter) *Handler {
	return &Handler{queue: queue, status: status, writeErr: writeErr, logger: slog.Default()}
}

// WithLogger replaces the logger, so a test can assert on what was
// recorded when the limiter was unreachable — the branch that silently
// lets a request through.
func (h *Handler) WithLogger(logger *slog.Logger) *Handler {
	if logger != nil {
		h.logger = logger
	}
	return h
}

// ─── POST /charts/{birthProfileId}/pdf ───────────────────────────────

// Create enqueues a render and returns the job id to poll.
//
// 202, not 200: the work has been accepted and has not been done. A 200
// here would be a lie that a client is entitled to act on.
//
// Takes the limiter as an argument rather than holding it, the same
// shape as charts.Recompute — so the route table shows at a glance which
// endpoints are limited, instead of that fact being buried in whichever
// dependencies a handler happened to be constructed with.
func (h *Handler) Create(limiter Limiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.create(w, r, limiter)
	}
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, limiter Limiter) {
	principal, profileID, ok := h.scope(w, r)
	if !ok {
		return
	}

	/*
	   Keyed on the user, not the IP.

	   The cost is per account, and an IP limit would punish everyone
	   behind one mobile carrier's NAT — which, in this product's market,
	   is a large share of the users.

	   Checked BEFORE the job id is minted and before the status is
	   written, so a refused request leaves nothing behind. Limiting after
	   the write would fill Redis with "queued" jobs for renders that were
	   never enqueued, and a client holding one of those job ids would
	   poll it until the TTL expired.
	*/
	if limiter != nil {
		result, err := limiter.Allow(r.Context(), RenderLimit, principal.UserID.String())
		if err != nil {
			/*
			   Fails OPEN, matching the recompute route and the global
			   throttle.

			   The trade is deliberate and worth stating, because failing
			   closed would be defensible on a route this expensive. The
			   reason it does not: the status store is the SAME Redis, so
			   an outage already means no render can report its result.
			   Refusing as well turns a degraded feature into a broken
			   one, and buys protection only against an attacker who
			   happens to strike during that outage.
			*/
			h.logger.WarnContext(r.Context(),
				"pdf rate limiter unavailable; allowing", slog.Any("err", err))
		} else if !result.Allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(result.RetryAfter.Seconds())+1))
			h.writeErr(w, r, http.StatusTooManyRequests, "RATE_LIMITED",
				"You have requested several PDFs recently. Please try again later.", nil)
			return
		}
	}

	jobID := uuid.New()
	payload := RenderPayload{
		JobID:     jobID,
		UserID:    principal.UserID,
		ProfileID: profileID,
		Locale:    normaliseLocale(r.URL.Query().Get("locale")),
	}

	task, err := NewRenderTask(payload)
	if err != nil {
		h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
			"Something went wrong. Please try again.", err)
		return
	}

	/*
	   Status first, enqueue second.

	   The other order has a window: the worker can pick the task up,
	   write "running", and then this goroutine overwrites it with
	   "queued" — after which the client polls a job that has already
	   finished and sees it go backwards. Writing "queued" before the
	   task exists makes that impossible.

	   The cost is a "queued" row for a task that then fails to enqueue,
	   which the error path below leaves behind. It expires on its own,
	   and a job stuck at "queued" is a far better failure than a
	   completed job reported as pending.
	*/
	if err := h.status.Set(r.Context(), principal.UserID, jobID, Status{State: StateQueued}); err != nil {
		h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
			"Something went wrong. Please try again.", err)
		return
	}

	if err := h.queue.Enqueue(task); err != nil {
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			// Cannot happen with a fresh uuid, and is handled anyway: a
			// conflict means the job exists, which is what the client
			// wanted. Reporting 500 for "already doing it" would be wrong.
			writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID.String()})
			return
		}
		h.writeErr(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE",
			"We could not start your download. Please try again in a moment.", err)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID.String()})
}

// ─── GET /charts/{birthProfileId}/pdf/{jobId} ────────────────────────

// Status reports on a render.
//
// The job id in the path is never trusted on its own. The status key is
// composed from the AUTHENTICATED user plus that id, so polling somebody
// else's job reads a key that does not exist — see statusKey. The
// ownership middleware above cannot help here: it guards the profile in
// the path, and a job id is not a profile.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := h.scope(w, r)
	if !ok {
		return
	}

	jobID, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "jobId")))
	if err != nil {
		// 404 rather than 400. A malformed id and an unknown id are the
		// same fact to the caller — that there is nothing here — and
		// distinguishing them costs a lookup for anyone sending garbage.
		h.notFound(w, r)
		return
	}

	status, err := h.status.Get(r.Context(), principal.UserID, jobID)
	if err != nil {
		if errors.Is(err, ErrNoJob) {
			h.notFound(w, r)
			return
		}
		h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
			"Something went wrong. Please try again.", err)
		return
	}

	// The body carries a signed URL to private birth data. It must not
	// be cached anywhere, by anyone.
	w.Header().Set("Cache-Control", "no-store, private")
	writeJSON(w, http.StatusOK, status)
}

// ─── helpers ─────────────────────────────────────────────────────────

func (h *Handler) scope(w http.ResponseWriter, r *http.Request) (auth.Principal, uuid.UUID, bool) {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required.", nil)
		return auth.Principal{}, uuid.Nil, false
	}

	// From the context, never from the URL — the ownership middleware put
	// it there, and a value the URL supplies is a value nobody checked.
	profileID, ok := reqctx.ProfileIDFrom(r.Context())
	if !ok {
		h.notFound(w, r)
		return auth.Principal{}, uuid.Nil, false
	}
	return principal, profileID, true
}

func (h *Handler) notFound(w http.ResponseWriter, r *http.Request) {
	h.writeErr(w, r, http.StatusNotFound, "NOT_FOUND", "Not found.", nil)
}

// normaliseLocale maps the request's locale onto what the print page
// supports, defaulting rather than rejecting.
//
// An allowlist, because the value is interpolated into a URL the worker
// then navigates to. A pass-through would let a request steer that
// navigation — the query string is the one part of the print URL a
// caller can influence.
func normaliseLocale(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "hi":
		return "hi"
	default:
		return "en"
	}
}

// writeJSON is local rather than shared: this package must not import
// httpapi, and the alternative — a platform package for four lines — is
// more indirection than it removes.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status line is already written, so there is nothing to tell
		// the client. Logged by the access-log middleware via the short
		// byte count.
		_ = err
	}
}
