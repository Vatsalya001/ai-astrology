package shares

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis/ratelimit"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/reqctx"
)

// HTTPErrorWriter is httpapi.WriteError, injected so this package does
// not import httpapi.
type HTTPErrorWriter func(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error)

// Limiter is the rate limiter, declared by the consumer so this package
// names only the one method it uses.
type Limiter interface {
	Allow(ctx context.Context, rule ratelimit.Rule, subject string) (ratelimit.Result, error)
}

/*
CreateLimit bounds how fast an account can mint bearer credentials.

Each one is a working link to a birth chart that survives until it
expires or is revoked. An unlimited endpoint is a way to manufacture
thousands of them — and the MaxLivePerUser cap does not close that on
its own, because revoking and re-minting in a loop stays under it
while producing an unbounded number of tokens that were once valid.

Twenty an hour is far above sharing with your family and far below
anything worth doing programmatically.
*/
var CreateLimit = ratelimit.Rule{
	Name:   "share_create",
	Max:    20,
	Window: time.Hour,
}

/*
ResolveLimit is keyed on the CLIENT, not on a user, because a viewer
has no account.

It is the only thing standing between the share endpoint and someone
walking the token space. That walk is hopeless — 128 bits — so this is
not really about guessing; it is about a token that HAS leaked being
replayed at volume, and about the endpoint not becoming a free way to
generate database load.
*/
var ResolveLimit = ratelimit.Rule{
	Name:   "share_resolve",
	Max:    60,
	Window: time.Minute,
}

// ChartReader loads the chart a resolved share points at.
//
// Declared here by the consumer, so this package does not import charts
// and the two stay independent.
type ChartReader interface {
	SharedChart(ctx context.Context, userID, profileID uuid.UUID) (any, error)
}

type Handler struct {
	svc      *Service
	charts   ChartReader
	writeErr HTTPErrorWriter
	// clientIP resolves the rate-limit subject for the public route.
	// Injected because the trusted-proxy decision lives in httpapi.
	clientIP func(*http.Request) string
}

func NewHandler(
	svc *Service,
	charts ChartReader,
	writeErr HTTPErrorWriter,
	clientIP func(*http.Request) string,
) *Handler {
	if clientIP == nil {
		clientIP = func(r *http.Request) string { return r.RemoteAddr }
	}
	return &Handler{svc: svc, charts: charts, writeErr: writeErr, clientIP: clientIP}
}

// ─── POST /charts/{birthProfileId}/shares ────────────────────────────

type createRequest struct {
	// Days the link should last. Zero means the default.
	//
	// Days rather than seconds because it is a human choice presented as
	// "30 days" in the UI, and a seconds field invites a client to send
	// something the server then has to reject.
	ExpiresInDays int `json:"expires_in_days"`
}

// Create issues a link.
func (h *Handler) Create(limiter Limiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, profileID, ok := h.scope(w, r)
		if !ok {
			return
		}

		if !h.allow(w, r, limiter, CreateLimit, principal.UserID.String(),
			"You have created several share links recently. Please try again later.") {
			return
		}

		var body createRequest
		if r.ContentLength > 0 {
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
				h.badRequest(w, r, "The request body could not be read.")
				return
			}
		}

		ttl := time.Duration(body.ExpiresInDays) * 24 * time.Hour

		share, err := h.svc.Create(r.Context(), principal.UserID, profileID, ttl)
		switch {
		case err == nil:
		case errors.Is(err, ErrNotFound):
			h.notFound(w, r)
			return
		case errors.Is(err, ErrTooManyLive):
			// 409, not 429. This is not "too fast", it is "too many at
			// once", and the remedy is revoking a link rather than
			// waiting — which the message says.
			h.writeErr(w, r, http.StatusConflict, "CONFLICT",
				"You have reached the maximum number of active share links. "+
					"Revoke one you no longer need, then try again.", nil)
			return
		default:
			h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
				"Something went wrong. Please try again.", err)
			return
		}

		/*
		   no-store, and here it is not a formality.

		   This response carries the ONLY copy of the plaintext token that
		   will ever exist. A shared cache holding it would be holding a
		   working credential to a birth chart, retrievable after the
		   owner has revoked the link.
		*/
		w.Header().Set("Cache-Control", "no-store, private")
		writeJSON(w, http.StatusCreated, share)
	}
}

// ─── GET /charts/{birthProfileId}/shares ─────────────────────────────

// List returns the links for a profile, live and dead.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	principal, profileID, ok := h.scope(w, r)
	if !ok {
		return
	}

	list, err := h.svc.List(r.Context(), principal.UserID, profileID)
	if err != nil {
		h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
			"Something went wrong. Please try again.", err)
		return
	}

	// No token is present on any of these — see Share.Token and toShare.
	// The header is belt and braces for a future field that forgets.
	w.Header().Set("Cache-Control", "no-store, private")
	writeJSON(w, http.StatusOK, map[string]any{"shares": list})
}

// ─── DELETE /charts/{birthProfileId}/shares/{shareId} ────────────────

// Revoke kills a link.
func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := h.scope(w, r)
	if !ok {
		return
	}

	shareID, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "shareId")))
	if err != nil {
		// 404 rather than 400: a malformed id and an unknown one are the
		// same fact to the caller.
		h.notFound(w, r)
		return
	}

	share, err := h.svc.Revoke(r.Context(), principal.UserID, shareID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			h.notFound(w, r)
			return
		}
		h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
			"Something went wrong. Please try again.", err)
		return
	}

	writeJSON(w, http.StatusOK, share)
}

// ─── GET /shared/{token} ─────────────────────────────────────────────

/*
View is the whole point, and the only public route in this package.

It sits outside `authenticate`, because the person opening a shared
link has no account here and is not going to make one to look at their
nephew's chart. The token is the authorisation.

The chart id comes from the RESOLVED SHARE, never from the request,
for the same reason the print route works that way: a handler that
read an id from the URL and merely checked the token was valid would
turn any share link into a reader for every chart. There is no profile
id in this route's path to be tempted by.
*/
func (h *Handler) View(limiter Limiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.allow(w, r, limiter, ResolveLimit, h.clientIP(r),
			"Too many requests. Please try again in a moment.") {
			return
		}

		token := chi.URLParam(r, "token")

		resolved, err := h.svc.Resolve(r.Context(), token)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				// Expired, revoked and never-existed are one answer. A
				// viewer learning WHICH would learn that a link was once
				// real, and that its owner turned it off.
				h.notFound(w, r)
				return
			}
			h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
				"Something went wrong. Please try again.", err)
			return
		}

		chart, err := h.charts.SharedChart(r.Context(), resolved.UserID, resolved.ProfileID)
		if err != nil {
			// Includes the profile having been deleted between resolve
			// and read. 404 is the honest answer.
			h.notFound(w, r)
			return
		}

		/*
		   no-store on a public URL that anyone may hold.

		   The body is somebody's chart. A CDN or corporate proxy caching
		   it would keep serving that chart after the owner revoked the
		   link — the one remedy they have — from infrastructure neither
		   they nor we control.
		*/
		w.Header().Set("Cache-Control", "no-store, private")

		// Nothing here should ever be indexed, and a share link WILL end
		// up somewhere a crawler can see it.
		w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")

		writeJSON(w, http.StatusOK, map[string]any{
			"scope": resolved.Scope,
			"chart": chart,
		})
	}
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

// allow applies a rate limit, failing OPEN on an unreachable limiter.
//
// Open rather than closed, matching every other limited route in this
// service: a Redis outage must not take the product down. Returns false
// when the caller has been refused and a response is already written.
func (h *Handler) allow(
	w http.ResponseWriter,
	r *http.Request,
	limiter Limiter,
	rule ratelimit.Rule,
	subject, message string,
) bool {
	if limiter == nil {
		return true
	}

	result, err := limiter.Allow(r.Context(), rule, subject)
	if err != nil {
		// Logged by the caller's middleware chain via the trace id; the
		// request proceeds.
		return true
	}
	if !result.Allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(result.RetryAfter.Seconds())+1))
		h.writeErr(w, r, http.StatusTooManyRequests, "RATE_LIMITED", message, nil)
		return false
	}
	return true
}

func (h *Handler) notFound(w http.ResponseWriter, r *http.Request) {
	h.writeErr(w, r, http.StatusNotFound, "NOT_FOUND", "Not found.", nil)
}

func (h *Handler) badRequest(w http.ResponseWriter, r *http.Request, message string) {
	h.writeErr(w, r, http.StatusBadRequest, "VALIDATION_FAILED", message, nil)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
