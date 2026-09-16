package birthprofiles

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

// Handler exposes the birth-profile endpoints.
//
// Nothing in this file logs a request body, an error detail derived from
// one, or a field name from one. Birth date plus time plus place is close
// enough to a unique identifier that the security rules say to treat it
// like an email address, and a validation message naming the value that
// failed puts it in a log line.
type Handler struct {
	svc      *Service
	places   PlaceLookup
	writeErr HTTPErrorWriter
}

// HTTPErrorWriter is httpapi.WriteError, injected so this package does
// not import httpapi.
type HTTPErrorWriter func(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error)

// PlaceLookup resolves the place a client selected.
//
// Declared by the consumer. The client sends a place ID from the search
// endpoint, never coordinates: latitude, longitude and IANA zone all have
// to agree, and a client that sends its own three values will eventually
// send three that do not — producing a chart that is wrong in a way
// nothing downstream can detect.
type PlaceLookup interface {
	Get(ctx context.Context, id int32) (Place, error)
}

// Place is the subset of a place this package needs. Declared here for
// the same reason as PlaceLookup.
type Place struct {
	Name      string
	Latitude  float64
	Longitude float64
	Timezone  string
}

func NewHandler(svc *Service, places PlaceLookup, writeErr HTTPErrorWriter) *Handler {
	return &Handler{svc: svc, places: places, writeErr: writeErr}
}

// profileBody is the create and update payload.
//
// The client sends LOCAL date and clock time plus a place ID. It does not
// send a UTC instant, and it must not: resolving a 1943 Indian wartime
// offset is the hardest part of this problem, and a browser that gets it
// wrong produces a chart that is wrong by an hour with nothing to show
// for it. Go resolves it once, server-side, and stores the answer.
type profileBody struct {
	Label        string `json:"label"`
	BirthDate    string `json:"birth_date"`
	BirthTime    string `json:"birth_time"`
	TimeAccuracy string `json:"time_accuracy"`
	PlaceID      int32  `json:"place_id"`
}

// ─── POST /birth-profiles ────────────────────────────────────────────

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		h.unauthorized(w, r)
		return
	}

	input, ok := h.parse(w, r, principal.UserID)
	if !ok {
		return
	}

	profile, err := h.svc.Create(r.Context(), input)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, profile)
}

// ─── GET /birth-profiles ─────────────────────────────────────────────

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		h.unauthorized(w, r)
		return
	}

	profiles, err := h.svc.List(r.Context(), principal.UserID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	// Always an array, never null — a client mapping over the response
	// should not have to special-case a brand-new account.
	if profiles == nil {
		profiles = []Profile{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"birth_profiles": profiles})
}

// ─── GET /birth-profiles/{id} ────────────────────────────────────────

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	principal, profileID, ok := h.scope(w, r)
	if !ok {
		return
	}

	profile, err := h.svc.Get(r.Context(), principal.UserID, profileID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

// ─── GET /birth-profiles/{id}/versions ───────────────────────────────

// Versions is what makes "which chart was that reading based on?"
// answerable. Correcting a birth time creates a new version rather than
// mutating the old one, and the old chart stays attached to the old
// version.
func (h *Handler) Versions(w http.ResponseWriter, r *http.Request) {
	principal, profileID, ok := h.scope(w, r)
	if !ok {
		return
	}

	versions, err := h.svc.Versions(r.Context(), principal.UserID, profileID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	if versions == nil {
		versions = []Profile{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

// ─── PATCH /birth-profiles/{id} ──────────────────────────────────────

// Update never mutates. It creates a new version and supersedes the old,
// so the response carries a different id from the one in the URL — which
// is unusual enough to be worth saying out loud in the API docs.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	principal, profileID, ok := h.scope(w, r)
	if !ok {
		return
	}

	input, ok := h.parse(w, r, principal.UserID)
	if !ok {
		return
	}

	profile, err := h.svc.Update(r.Context(), principal.UserID, profileID, input)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

// ─── DELETE /birth-profiles/{id} ─────────────────────────────────────

// Delete is a soft delete. The charts and readings that reference this
// profile have to stay explicable; hard deletion happens only when the
// whole account goes, where the cascade takes profiles, charts and
// dashas together.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	principal, profileID, ok := h.scope(w, r)
	if !ok {
		return
	}

	if err := h.svc.Deactivate(r.Context(), principal.UserID, profileID); err != nil {
		h.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── parsing ─────────────────────────────────────────────────────────

// parse turns a request body into a CreateInput, resolving the place.
func (h *Handler) parse(w http.ResponseWriter, r *http.Request, userID uuid.UUID) (CreateInput, bool) {
	var body profileBody
	if !decode(w, r, &body, h.writeErr) {
		return CreateInput{}, false
	}

	birthDate, err := time.Parse("2006-01-02", strings.TrimSpace(body.BirthDate))
	if err != nil {
		h.badRequest(w, r, "birth_date must be a calendar date, as YYYY-MM-DD.")
		return CreateInput{}, false
	}

	accuracy := strings.TrimSpace(body.TimeAccuracy)
	if accuracy == "" {
		accuracy = AccuracyExact
	}

	birthTime, hasTime, ok := h.parseClockTime(w, r, body.BirthTime, accuracy)
	if !ok {
		return CreateInput{}, false
	}

	place, err := h.places.Get(r.Context(), body.PlaceID)
	if err != nil {
		// Not "place 4711 does not exist": the ID came from our own search
		// endpoint, so a client sending an unknown one is either stale or
		// probing, and neither is helped by confirming which IDs are real.
		h.badRequest(w, r, "That birth place could not be resolved. Please search and select it again.")
		return CreateInput{}, false
	}

	return CreateInput{
		UserID:       userID,
		Label:        strings.TrimSpace(body.Label),
		BirthDate:    birthDate,
		BirthTime:    birthTime,
		HasBirthTime: hasTime,
		TimeAccuracy: accuracy,
		PlaceName:    place.Name,
		Latitude:     place.Latitude,
		Longitude:    place.Longitude,
		Timezone:     place.Timezone,
	}, true
}

// parseClockTime reads HH:MM, and enforces that it is present unless the
// accuracy explicitly says it is unknown.
//
// The escape hatch is narrow on purpose: "unknown" means no ascendant and
// no houses, which is a materially poorer chart, so a client must ask for
// it rather than fall into it by omitting a field.
func (h *Handler) parseClockTime(
	w http.ResponseWriter, r *http.Request, raw, accuracy string,
) (time.Duration, bool, bool) {
	raw = strings.TrimSpace(raw)

	if raw == "" {
		if accuracy != AccuracyUnknown {
			h.badRequest(w, r,
				`birth_time is required unless time_accuracy is "unknown".`)
			return 0, false, false
		}
		return 0, false, true
	}

	clock, err := time.Parse("15:04", raw)
	if err != nil {
		h.badRequest(w, r, "birth_time must be a 24-hour clock time, as HH:MM.")
		return 0, false, false
	}

	duration := time.Duration(clock.Hour())*time.Hour + time.Duration(clock.Minute())*time.Minute
	return duration, true, true
}

// ─── shared plumbing ─────────────────────────────────────────────────

// scope returns the caller and the profile ID the ownership middleware
// already verified.
//
// The ID comes from the request context, NEVER from chi.URLParam. That
// is the whole discipline: a handler written this way cannot be reached
// with an ID nobody checked, so moving or copying the route later cannot
// quietly drop the check. httpapi.RequireProfileOwnership is what puts
// the value there.
func (h *Handler) scope(w http.ResponseWriter, r *http.Request) (auth.Principal, uuid.UUID, bool) {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		h.unauthorized(w, r)
		return auth.Principal{}, uuid.Nil, false
	}

	profileID, ok := reqctx.ProfileIDFrom(r.Context())
	if !ok {
		// The route was mounted outside the ownership group. Refusing is
		// the only safe response: continuing would mean reading the ID
		// from the URL, which is precisely the thing nobody checked.
		h.notFound(w, r)
		return auth.Principal{}, uuid.Nil, false
	}

	return principal, profileID, true
}

// handleError maps a service error to a status.
//
// ErrInvalidInput carries its own message because it names a FIELD and a
// rule, never a value — "time_accuracy exact requires a birth time", not
// the time itself.
func (h *Handler) handleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		h.notFound(w, r)
	case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrUnknownPlace):
		h.badRequest(w, r, publicMessage(err))
	default:
		// The cause goes to the log keyed by trace ID; the client gets
		// nothing. A timezone-resolution failure mentions the zone, and a
		// zone plus a date is most of a birth record.
		h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
			"Something went wrong. Please try again.", err)
	}
}

// publicMessage strips the package prefix from a sentinel-wrapped error.
//
// "birthprofiles: invalid input: time_accuracy …" is an internal string;
// the client gets the part after the sentinel, which is written to be
// shown to a person.
func publicMessage(err error) string {
	message := err.Error()
	if _, after, found := strings.Cut(message, ": "); found {
		if _, rest, ok := strings.Cut(after, ": "); ok {
			return capitalise(rest)
		}
		return capitalise(after)
	}
	return "That request could not be accepted."
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:] + "."
}

func (h *Handler) unauthorized(w http.ResponseWriter, r *http.Request) {
	h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required.", nil)
}

func (h *Handler) notFound(w http.ResponseWriter, r *http.Request) {
	h.writeErr(w, r, http.StatusNotFound, "NOT_FOUND", "Not found.", nil)
}

func (h *Handler) badRequest(w http.ResponseWriter, r *http.Request, message string) {
	h.writeErr(w, r, http.StatusBadRequest, "VALIDATION_FAILED", message, nil)
}

func decode(w http.ResponseWriter, r *http.Request, v any, writeErr HTTPErrorWriter) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()

	if err := dec.Decode(v); err != nil {
		// Never the decoder's message. It quotes the offending JSON, which
		// here is somebody's date and time of birth.
		writeErr(w, r, http.StatusBadRequest, "BAD_REQUEST",
			"The request body could not be read.", nil)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status line is already out; there is nothing useful left to
		// tell the client. No body in the message — it is a birth record.
		slog.Error("encode birth-profile response", slog.Any("err", err))
	}
}
