package transits

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/reqctx"
)

// Handler exposes the gochara endpoints.
//
// Neither of them calls astro-service. Both read the table the worker
// fills, which is what makes Sade Sati answerable during an outage.
type Handler struct {
	reader   *Reader
	natal    NatalMoon
	writeErr HTTPErrorWriter
	now      func() time.Time
}

// HTTPErrorWriter is httpapi.WriteError, injected so this package does
// not import httpapi.
type HTTPErrorWriter func(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error)

// NatalMoon supplies the one fact a natal-relative transit needs.
//
// Declared by the consumer, per the Go rules: the chart service
// implements it, and this package does not import that one. It returns
// the sign NAME rather than an index deliberately — the chart package
// knows the shape of chart JSON, this package owns the zodiac ordering,
// and neither has to learn the other's job.
type NatalMoon interface {
	MoonSign(ctx context.Context, userID, profileID uuid.UUID) (string, error)
}

func NewHandler(reader *Reader, natal NatalMoon, writeErr HTTPErrorWriter) *Handler {
	return &Handler{reader: reader, natal: natal, writeErr: writeErr, now: time.Now}
}

// WithClock replaces the clock, so a test can ask about a fixed instant.
func (h *Handler) WithClock(now func() time.Time) *Handler {
	if now != nil {
		h.now = now
	}
	return h
}

// ─── GET /astrology/transits ─────────────────────────────────────────

// Global serves the shared table as-is.
//
// No user scoping, because there is nothing to scope: these positions are
// identical for every person alive at that instant. That is also what
// makes the response safe to cache at the edge.
func (h *Handler) Global(w http.ResponseWriter, r *http.Request) {
	at, ok := h.parseAt(w, r)
	if !ok {
		return
	}

	positions, err := h.reader.GlobalAt(r.Context(), at)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"at": at, "transits": positions})
}

// ─── GET /astrology/transits/{birthProfileId} ────────────────────────

// Natal serves the same positions rotated onto the caller's Moon, with
// Sade Sati.
func (h *Handler) Natal(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required.", nil)
		return
	}

	// From the context, never the URL — the ownership middleware put it
	// there, and a value the URL supplies is a value nobody checked.
	profileID, ok := reqctx.ProfileIDFrom(r.Context())
	if !ok {
		h.writeErr(w, r, http.StatusNotFound, "NOT_FOUND", "Not found.", nil)
		return
	}

	at, ok := h.parseAt(w, r)
	if !ok {
		return
	}

	moonSignName, err := h.natal.MoonSign(r.Context(), principal.UserID, profileID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}

	moonSign, err := SignIndex(moonSignName)
	if err != nil {
		// A stored chart naming a sign this service does not recognise is
		// a drift between Go and astro-service, not a client mistake.
		// TestSignNamesMatchAstroService exists so this branch stays
		// theoretical, and the log line is how we would learn it did not.
		h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
			"Something went wrong. Please try again.", err)
		return
	}

	positions, err := h.reader.At(r.Context(), at, moonSign)
	if err != nil {
		h.handleError(w, r, err)
		return
	}

	sadeSati, err := h.reader.SadeSatiAt(r.Context(), at, moonSign)
	if err != nil {
		h.handleError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"at": at,
		// The sign, not the birth details it was derived from. A Moon sign
		// is one of twelve values and identifies nobody.
		"natal_moon_sign": moonSignName,
		"transits":        positions,
		"sade_sati":       sadeSati,
	})
}

// ─── shared plumbing ─────────────────────────────────────────────────

// parseAt reads an optional ?at, defaulting to now.
func (h *Handler) parseAt(w http.ResponseWriter, r *http.Request) (time.Time, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("at"))
	if raw == "" {
		return h.now(), true
	}

	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		h.writeErr(w, r, http.StatusBadRequest, "VALIDATION_FAILED",
			"at must be an RFC 3339 timestamp.", nil)
		return time.Time{}, false
	}
	return at, true
}

func (h *Handler) handleError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrNoTransits) {
		// The worker has not run yet, or has not run since the retention
		// window. 503 rather than 404 or an empty list: there is nothing
		// wrong with the request, and "no planets are transiting" is never
		// true.
		w.Header().Set("Retry-After", "300")
		h.writeErr(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE",
			"Transits are being prepared. Please try again shortly.", nil)
		return
	}
	h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
		"Something went wrong. Please try again.", err)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode transit response", slog.Any("err", err))
	}
}
