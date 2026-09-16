package places

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
)

// The place search is the first interactive thing a new user touches and
// the single highest drop-off point in onboarding: somebody who cannot
// find their birth town does not get a chart, and does not come back.
//
// It is also the only endpoint here that reads nothing user-owned, which
// is what makes its answers safe to cache across everybody.

// DefaultLimit is what a client gets without asking.
//
// Ten fits a dropdown without scrolling. Beyond that a user is not
// choosing from a list, they are searching a second time.
const DefaultLimit = 10

// MaxLimit caps what a client may ask for.
//
// Not a correctness bound — a generous limit is still one indexed prefix
// scan — but an abuse bound. Without it, `?limit=100000` turns a search
// box into a bulk export of a 200k-row gazetteer.
const MaxLimit = 25

// HTTPErrorWriter is httpapi.WriteError, injected so this package does
// not import httpapi.
type HTTPErrorWriter func(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error)

type Handler struct {
	svc      *Service
	writeErr HTTPErrorWriter
}

func NewHandler(svc *Service, writeErr HTTPErrorWriter) *Handler {
	return &Handler{svc: svc, writeErr: writeErr}
}

// ─── GET /places/search?q=jaip&limit=10 ──────────────────────────────

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")

	places, err := h.svc.Search(r.Context(), query, parseLimit(r.URL.Query().Get("limit")))
	if err != nil {
		switch {
		case errors.Is(err, ErrQueryTooShort):
			// 200 with an empty list, not an error. A user typing "j" has
			// not made a mistake — they are mid-word, and a red error under
			// a search box they are still filling in is a bug that looks
			// like a feature.
			writeJSON(w, http.StatusOK, response{Places: []Place{}, Query: query})
		case errors.Is(err, ErrQueryTooLong):
			h.writeErr(w, r, http.StatusBadRequest, "VALIDATION_FAILED",
				"That search term is too long.", nil)
		default:
			h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
				"Something went wrong. Please try again.", err)
		}
		return
	}

	// Never null. A client mapping over the result should not have to
	// special-case "no matches".
	if places == nil {
		places = []Place{}
	}
	writeJSON(w, http.StatusOK, response{Places: places, Query: query})
}

type response struct {
	Places []Place `json:"places"`
	// Query is echoed so a client can discard a response that arrived
	// after the user typed another character. Debouncing on the client is
	// not enough — responses can overtake each other on the wire, and the
	// visible symptom is a dropdown that flickers back to stale results.
	Query string `json:"query"`
}

// parseLimit is forgiving on purpose.
//
// A malformed limit is a client bug, and failing the request would break
// the search box rather than fix the client. Clamp and serve.
func parseLimit(raw string) int32 {
	if raw == "" {
		return DefaultLimit
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return DefaultLimit
	}
	if value > MaxLimit {
		return MaxLimit
	}
	return int32(value)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode places response", slog.Any("err", err))
	}
}
