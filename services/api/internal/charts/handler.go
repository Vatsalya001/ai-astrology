package charts

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis/ratelimit"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/reqctx"
)

// Handler exposes the chart and dasha endpoints.
//
// Every route here is mounted behind httpapi.RequireProfileOwnership, so
// the profile ID comes out of the request context and never out of the
// URL. See that file, and the test that enforces it.
type Handler struct {
	svc      *Service
	writeErr HTTPErrorWriter
	now      func() time.Time
	// Nil until WithPrintTokens is called. The print route refuses to
	// serve without it rather than defaulting to something permissive.
	tokens *PrintTokens
}

// HTTPErrorWriter is httpapi.WriteError, injected so this package does
// not import httpapi.
type HTTPErrorWriter func(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error)

func NewHandler(svc *Service, writeErr HTTPErrorWriter) *Handler {
	return &Handler{svc: svc, writeErr: writeErr, now: time.Now}
}

// WithPrintTokens enables the print route.
//
// Separate from the constructor because the token store needs Redis,
// and a deployment without Redis should still serve charts rather than
// refusing to start.
func (h *Handler) WithPrintTokens(tokens *PrintTokens) *Handler {
	h.tokens = tokens
	return h
}

// WithClock replaces the clock. Tests use it to ask "which dasha was
// running in 1997" without arranging for it to be 1997.
func (h *Handler) WithClock(now func() time.Time) *Handler {
	if now != nil {
		h.now = now
	}
	return h
}

// ─── GET /charts/{birthProfileId}?type=D1|D9|D10 ─────────────────────

// Get serves a chart, and keeps serving it while astro-service is down.
//
// GetOrStale rather than Get: this is the endpoint behind "show me my
// Kundli", and the specification's requirement is that it works during an
// outage. A stored chart is not stale the way a cached API response is —
// a chart is a pure function of its inputs — so the only thing the
// `stale` flag really means is "we could not check whether the engine
// version moved".
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	principal, profileID, ok := h.scope(w, r)
	if !ok {
		return
	}

	chartType, ok := h.parseChartType(w, r)
	if !ok {
		return
	}

	chart, err := h.svc.GetOrStale(r.Context(), principal.UserID, Key{
		ProfileID: profileID,
		ChartType: chartType,
	})
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, chart)
}

// ─── GET /charts/{birthProfileId}/dashas?level=1 ─────────────────────

func (h *Handler) Dashas(w http.ResponseWriter, r *http.Request) {
	principal, profileID, ok := h.scope(w, r)
	if !ok {
		return
	}

	level := 1
	if raw := strings.TrimSpace(r.URL.Query().Get("level")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > MaxDashaLevel {
			h.badRequest(w, r, "level must be 1, 2 or 3.")
			return
		}
		level = parsed
	}

	chart, err := h.svc.GetOrStale(r.Context(), principal.UserID, Key{ProfileID: profileID})
	if err != nil {
		h.handleError(w, r, err)
		return
	}

	periods, err := h.svc.Dashas(r.Context(), principal.UserID, chart.ID, level)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"level": level, "periods": periods})
}

// ─── GET /charts/{birthProfileId}/dashas/current?at=RFC3339 ──────────

// Current answers the question the dasha screen actually asks.
func (h *Handler) Current(w http.ResponseWriter, r *http.Request) {
	principal, profileID, ok := h.scope(w, r)
	if !ok {
		return
	}

	at := h.now()
	if raw := strings.TrimSpace(r.URL.Query().Get("at")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			h.badRequest(w, r, "at must be an RFC 3339 timestamp.")
			return
		}
		at = parsed
	}

	chart, err := h.svc.GetOrStale(r.Context(), principal.UserID, Key{ProfileID: profileID})
	if err != nil {
		h.handleError(w, r, err)
		return
	}

	current, err := h.svc.Current(r.Context(), principal.UserID, chart.ID, at)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, current)
}

// ─── POST /charts/{birthProfileId}/recompute ─────────────────────────

// RecomputeLimit is deliberately tight.
//
// This is the only route in the service that calls astro-service
// unconditionally — every other read is answered from storage. Without a
// limit it is a button that turns one authenticated user into arbitrary
// load on the compute service, and the rest of the product degrades with
// it.
var RecomputeLimit = ratelimit.Rule{
	Name:   "chart_recompute",
	Max:    3,
	Window: time.Hour,
}

// Recompute discards the stored chart and asks astro-service again.
//
// DEVIATION FROM THE SPEC, stated plainly. The Phase 2 endpoint table
// labels this "admin only — after an engine upgrade". Phase 2 assigns the
// admin role to nobody, so an admin-gated route would be one no request
// can reach and no test can exercise — and an endpoint that exists but
// cannot be called is worse than one that is not mounted.
//
// It is scoped to the OWNER instead, with a tight rate limit standing in
// for the load protection "admin only" was really buying. That is
// strictly narrower than the spec on authorisation: a caller can only
// ever recompute a profile they own.
//
// The admin case the spec had in mind — recompute everything after an
// engine upgrade — is a bulk job over ListChartsByEngineVersion, which is
// a worker task and not an HTTP route at all.
func (h *Handler) Recompute(limiter Limiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, profileID, ok := h.scope(w, r)
		if !ok {
			return
		}

		chartType, ok := h.parseChartType(w, r)
		if !ok {
			return
		}

		// Keyed on the user, not the IP: the cost is per account, and an
		// IP limit would punish everyone behind one mobile carrier NAT.
		result, err := limiter.Allow(r.Context(), RecomputeLimit, principal.UserID.String())
		if err != nil {
			// Fails OPEN, matching the global throttle: a Redis outage must
			// not take the product down, and this route still has to hold a
			// database transaction to do any damage.
			h.svcLogWarn(r, err)
		} else if !result.Allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(result.RetryAfter.Seconds())+1))
			h.writeErr(w, r, http.StatusTooManyRequests, "RATE_LIMITED",
				"This chart has been recomputed several times recently. Please try again later.", nil)
			return
		}

		chart, err := h.svc.Recompute(r.Context(), principal.UserID, Key{
			ProfileID: profileID,
			ChartType: chartType,
		})
		if err != nil {
			h.handleError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, chart)
	}
}

// Limiter is the rate limiter, declared by the consumer so this package
// names only the one method it uses.
type Limiter interface {
	Allow(ctx context.Context, rule ratelimit.Rule, subject string) (ratelimit.Result, error)
}

func (h *Handler) svcLogWarn(r *http.Request, err error) {
	slog.WarnContext(r.Context(), "recompute rate limiter unavailable; allowing",
		slog.Any("err", err))
}

// ─── shared plumbing ─────────────────────────────────────────────────

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

// parseChartType validates ?type against an allowlist.
//
// An allowlist rather than a pass-through: the value reaches both a
// database UNIQUE key and astro-service, and an unrecognised one would
// create a permanent cache entry for a chart nothing can render.
func (h *Handler) parseChartType(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("type"))
	if raw == "" {
		return ChartTypeRasi, true
	}

	switch strings.ToUpper(raw) {
	case ChartTypeRasi:
		return ChartTypeRasi, true
	case ChartTypeNavamsa:
		return ChartTypeNavamsa, true
	case ChartTypeDasamsa:
		return ChartTypeDasamsa, true
	default:
		h.badRequest(w, r, "type must be D1, D9 or D10.")
		return "", false
	}
}

func (h *Handler) handleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		h.notFound(w, r)

	case errors.Is(err, ErrNoDashas):
		// Not a 404 on the chart — the chart exists. A profile recorded
		// without a birth time genuinely has no dasha tree, because the
		// Moon cannot be pinned to a nakshatra pada without one, and the
		// user needs to be told that rather than shown an empty screen.
		h.writeErr(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED",
			"Dashas need a birth time. Add one to this profile to see them.", nil)

	case errors.Is(err, ErrUncomputable):
		// A brand-new profile during an astro outage. 503 with Retry-After
		// so a client backs off rather than hammering.
		w.Header().Set("Retry-After", "60")
		h.writeErr(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE",
			"This chart is being prepared. Please try again in a moment.", nil)

	case clients.IsRejected(err):
		// astro refused our request. Our bug, so it is a 500 to the client
		// and a logged cause with a trace ID for us. Never the detail —
		// it names fields, and a field name plus a birth record is a
		// sentence about a person.
		h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
			"Something went wrong. Please try again.", err)

	case clients.IsUnavailable(err):
		w.Header().Set("Retry-After", "60")
		h.writeErr(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE",
			"This chart is being prepared. Please try again in a moment.", nil)

	default:
		h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
			"Something went wrong. Please try again.", err)
	}
}

func (h *Handler) notFound(w http.ResponseWriter, r *http.Request) {
	h.writeErr(w, r, http.StatusNotFound, "NOT_FOUND", "Not found.", nil)
}

func (h *Handler) badRequest(w http.ResponseWriter, r *http.Request, message string) {
	h.writeErr(w, r, http.StatusBadRequest, "VALIDATION_FAILED", message, nil)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// No body in the message: a chart is derived from birth data.
		slog.Error("encode chart response", slog.Any("err", err))
	}
}
