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

// ─── deletion and export ─────────────────────────────────────────────

// FreshOTPVerifier re-checks a code at the moment of a dangerous action.
//
// Declared by the consumer. Deletion and export both require it on top
// of a valid access token: an access token lives 15 minutes and an
// unlocked laptop is enough to use one, which is not the bar for "erase
// everything" or "hand me a file containing this person's whole
// history".
type FreshOTPVerifier interface {
	VerifyFresh(ctx context.Context, userID uuid.UUID, code string) error
}

type dangerousActionBody struct {
	// Code is a currently-valid OTP sent to the account's own verified
	// contact. It proves possession now, not fifteen minutes ago.
	Code string `json:"code"`
	// Confirm must be the literal word, typed by the user. A stray
	// DELETE from a mis-scoped client is otherwise indistinguishable
	// from an intentional one.
	Confirm string `json:"confirm"`
}

func (h *Handler) RequestDeletion(deleter *Deleter, fresh FreshOTPVerifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.PrincipalFrom(r.Context())
		if !ok {
			h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required.", nil)
			return
		}

		var body dangerousActionBody
		if !decode(w, r, &body, h.writeErr) {
			return
		}
		if body.Confirm != "DELETE" {
			h.writeErr(w, r, http.StatusBadRequest, "VALIDATION_FAILED",
				`Type DELETE to confirm.`, nil)
			return
		}
		if err := fresh.VerifyFresh(r.Context(), principal.UserID, body.Code); err != nil {
			h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED",
				"That code is not valid. Request a new one.", nil)
			return
		}

		deleteAt, err := deleter.Request(r.Context(), principal.UserID)
		if err != nil {
			if errors.Is(err, ErrDeletionAlreadyRequested) {
				// Idempotent from the caller's point of view: they asked to
				// be deleted and they are being deleted.
				w.WriteHeader(http.StatusNoContent)
				return
			}
			h.handleError(w, r, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"deletion_scheduled_at": deleteAt.UTC().Format("2006-01-02T15:04:05Z"),
			// Stated explicitly so the grace window is a promise the user
			// can act on rather than a detail in a help page.
			"cancellable_until": deleteAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
}

func (h *Handler) CancelDeletion(deleter *Deleter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.PrincipalFrom(r.Context())
		if !ok {
			h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required.", nil)
			return
		}

		if err := deleter.Cancel(r.Context(), principal.UserID); err != nil {
			if errors.Is(err, ErrNoDeletionPending) {
				h.writeErr(w, r, http.StatusNotFound, "NOT_FOUND", "No deletion is pending.", nil)
				return
			}
			h.handleError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) Export(exporter *Exporter, fresh FreshOTPVerifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.PrincipalFrom(r.Context())
		if !ok {
			h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required.", nil)
			return
		}

		// The code arrives as a query parameter because this is a GET the
		// browser downloads. It is single-use and five minutes old at
		// most, so the usual "never put a secret in a URL" objection —
		// that it persists in history and access logs — costs little here,
		// and the alternative is a POST that cannot be a download link.
		code := r.URL.Query().Get("code")
		if err := fresh.VerifyFresh(r.Context(), principal.UserID, code); err != nil {
			h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED",
				"That code is not valid. Request a new one.", nil)
			return
		}

		export, err := exporter.Export(r.Context(), principal.UserID)
		if err != nil {
			h.handleError(w, r, err)
			return
		}

		w.Header().Set("Content-Disposition", `attachment; filename="ayana-export.json"`)
		writeJSON(w, http.StatusOK, export)
	}
}

// FreshOTPChallenger sends a re-verification code. Declared here for the
// same reason as FreshOTPVerifier.
type FreshOTPChallenger interface {
	Challenge(ctx context.Context, userID uuid.UUID, locale string) error
}

// Challenge sends a code to the account's own verified contact.
//
// Separate from /auth/otp/request because that one takes an identifier
// from the caller. This one takes none — the destination comes from the
// account, so a stolen access token cannot redirect the code.
func (h *Handler) Challenge(challenger FreshOTPChallenger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.PrincipalFrom(r.Context())
		if !ok {
			h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required.", nil)
			return
		}

		if err := challenger.Challenge(r.Context(), principal.UserID, r.Header.Get("Accept-Language")); err != nil {
			h.handleError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"sent": true})
	}
}
