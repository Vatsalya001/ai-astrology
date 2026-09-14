package users

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"
)

// Handler exposes the users endpoints.
type Handler struct {
	svc      *Service
	sessions SessionReader
	writeErr HTTPErrorWriter
}

// HTTPErrorWriter is httpapi.WriteError, injected so this package does
// not import httpapi.
type HTTPErrorWriter func(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error)

// SessionReader is what the sessions endpoints need.
//
// Declared by the consumer, as with auth.UserCreator: the concrete
// implementation lives next to the session storage, and this package
// names only the shape it uses.
type SessionReader interface {
	ListActive(ctx context.Context, userID uuid.UUID) ([]SessionView, error)
	Revoke(ctx context.Context, sessionID, userID uuid.UUID) error
}

// SessionView is a device entry as the user sees it.
//
// Deliberately NOT the database row. `refresh_hash` must never leave the
// server — rendering the token's hash into a settings page would put a
// credential-shaped value in a browser, a screenshot and a support
// ticket.
type SessionView struct {
	ID        uuid.UUID `json:"id"`
	UserAgent string    `json:"user_agent"`
	CreatedAt string    `json:"created_at"`
	ExpiresAt string    `json:"expires_at"`
	// Current marks the session making this request, so the UI can label
	// it rather than inviting someone to revoke their own session and
	// wonder why they were logged out.
	Current bool `json:"current"`
}

func NewHandler(svc *Service, sessions SessionReader, writeErr HTTPErrorWriter) *Handler {
	return &Handler{svc: svc, sessions: sessions, writeErr: writeErr}
}

// ─── GET /users/me ───────────────────────────────────────────────────

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required.", nil)
		return
	}

	profile, err := h.svc.Profile(r.Context(), principal.UserID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

// ─── PATCH /users/me ─────────────────────────────────────────────────

type patchProfileBody struct {
	// Pointers so "absent" and "explicitly null" are distinguishable. A
	// PATCH omitting a field must leave it alone, not clear it.
	Name   *string `json:"name"`
	Gender *string `json:"gender"`
}

func (h *Handler) PatchMe(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required.", nil)
		return
	}

	var body patchProfileBody
	if !decode(w, r, &body, h.writeErr) {
		return
	}

	// Email and phone are deliberately NOT writable here. Changing a
	// contact method has to go through verification, or a stolen access
	// token becomes permanent account takeover by moving the address the
	// codes are sent to.
	profile, err := h.svc.UpdateProfile(r.Context(), principal.UserID, body.Name, body.Gender)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

// ─── preferences ─────────────────────────────────────────────────────

type patchPreferencesBody struct {
	PreferredLanguage *string `json:"preferred_language"`
	AstrologySystem   *string `json:"astrology_system"`
	ChartStyle        *string `json:"chart_style"`
	Theme             *string `json:"theme"`
}

func (h *Handler) Preferences(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required.", nil)
		return
	}

	prefs, err := h.svc.Preferences(r.Context(), principal.UserID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (h *Handler) PatchPreferences(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required.", nil)
		return
	}

	var body patchPreferencesBody
	if !decode(w, r, &body, h.writeErr) {
		return
	}

	prefs, err := h.svc.UpdatePreferences(r.Context(), principal.UserID, PreferenceUpdate{
		PreferredLanguage: body.PreferredLanguage,
		AstrologySystem:   body.AstrologySystem,
		ChartStyle:        body.ChartStyle,
		Theme:             body.Theme,
	})
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

// ─── sessions ────────────────────────────────────────────────────────

func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required.", nil)
		return
	}

	sessions, err := h.sessions.ListActive(r.Context(), principal.UserID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	// Always an array, never null: a client iterating the response should
	// not have to special-case "no other devices".
	if sessions == nil {
		sessions = []SessionView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (h *Handler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required.", nil)
		return
	}

	sessionID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		// 404, not 400. A malformed id and someone else's id must be
		// indistinguishable, or the endpoint reports which UUIDs are
		// well-formed session ids.
		h.writeErr(w, r, http.StatusNotFound, "NOT_FOUND", "Session not found.", nil)
		return
	}

	// The query scopes by user_id, so revoking another user's session
	// simply affects no rows. 404 rather than 403: a 403 would confirm
	// the session exists and belongs to someone else.
	if err := h.sessions.Revoke(r.Context(), sessionID, principal.UserID); err != nil {
		h.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── helpers ─────────────────────────────────────────────────────────

func (h *Handler) handleError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrNotFound) {
		h.writeErr(w, r, http.StatusNotFound, "NOT_FOUND", "Not found.", nil)
		return
	}
	if errors.Is(err, ErrInvalidPreference) {
		h.writeErr(w, r, http.StatusBadRequest, "VALIDATION_FAILED",
			"One of those values is not supported.", nil)
		return
	}
	h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
		"Something went wrong. Please try again.", err)
}

func decode(w http.ResponseWriter, r *http.Request, v any, writeErr HTTPErrorWriter) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()

	if err := dec.Decode(v); err != nil {
		writeErr(w, r, http.StatusBadRequest, "BAD_REQUEST",
			"The request body could not be read.", nil)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
