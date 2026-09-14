package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis/ratelimit"
)

// refreshCookieName is where the web client keeps its refresh token.
//
// httpOnly so JavaScript cannot read it, which is what makes an XSS bug
// survivable rather than a full account takeover. Mobile clients send it
// in the body instead and never see this cookie.
const refreshCookieName = "ayana_refresh"

// Handler exposes the auth service over HTTP.
type Handler struct {
	svc        *Service
	limiter    *ratelimit.Limiter
	ipSalt     string
	trustProxy bool
	secure     bool
	refreshTTL time.Duration
	writeErr   HTTPErrorWriter
}

// HTTPErrorWriter is httpapi.WriteError, injected to avoid importing it.
type HTTPErrorWriter func(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error)

type HandlerConfig struct {
	Service    *Service
	Limiter    *ratelimit.Limiter
	IPSalt     string
	TrustProxy bool
	// Secure marks the refresh cookie secure. False only for plain-HTTP
	// local development; anything reachable over the network must set it.
	Secure     bool
	RefreshTTL time.Duration
	WriteError HTTPErrorWriter
}

func NewHandler(cfg HandlerConfig) *Handler {
	return &Handler{
		svc:        cfg.Service,
		limiter:    cfg.Limiter,
		ipSalt:     cfg.IPSalt,
		trustProxy: cfg.TrustProxy,
		secure:     cfg.Secure,
		refreshTTL: cfg.RefreshTTL,
		writeErr:   cfg.WriteError,
	}
}

// ─── POST /auth/otp/request ──────────────────────────────────────────

type otpRequestBody struct {
	Channel    string `json:"channel"`
	Identifier string `json:"identifier"`
	Locale     string `json:"locale"`
}

// otpRequestResponse is returned for EVERY outcome that is not a client
// error — known identifier, unknown identifier, delivery succeeded or
// silently did not. Varying it would re-open the enumeration oracle that
// the timing floor in the service closes.
type otpRequestResponse struct {
	Sent      bool `json:"sent"`
	ExpiresIn int  `json:"expires_in_seconds"`
}

func (h *Handler) RequestOTP(w http.ResponseWriter, r *http.Request) {
	var body otpRequestBody
	if !h.decode(w, r, &body) {
		return
	}

	normalised, err := NormaliseIdentifier(body.Channel, body.Identifier)
	if err != nil {
		// A malformed identifier is a client bug, not an enumeration
		// signal — no real identifier can look like this — so reporting
		// it costs nothing and saves a confusing silent failure.
		h.writeErr(w, r, http.StatusBadRequest, "VALIDATION_FAILED",
			"Enter a valid email address or phone number.", nil)
		return
	}

	ip := ClientIP(r, h.trustProxy)
	ipKey := string(HashIP(ip, h.ipSalt))

	// Per-identifier first, so Retry-After reflects the limit a user
	// actually hits rather than the broader per-IP one.
	res, err := h.limiter.AllowAll(r.Context(), normalised,
		ratelimit.OTPRequestPerIdentifier,
		ratelimit.OTPRequestPerIdentifierDaily,
	)
	if err != nil {
		h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
			"Something went wrong. Please try again.", err)
		return
	}
	if res.Allowed {
		res, err = h.limiter.Allow(r.Context(), ratelimit.OTPRequestPerIP, ipKey)
		if err != nil {
			h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
				"Something went wrong. Please try again.", err)
			return
		}
	}
	if !res.Allowed {
		h.writeRateLimited(w, r, res)
		return
	}

	if err := h.svc.RequestOTP(r.Context(), body.Channel, body.Identifier, body.Locale, HashIP(ip, h.ipSalt)); err != nil {
		if errors.Is(err, ErrInvalidIdentifier) || errors.Is(err, ErrUnknownChannel) {
			h.writeErr(w, r, http.StatusBadRequest, "VALIDATION_FAILED",
				"Enter a valid email address or phone number.", nil)
			return
		}
		// A delivery failure is logged but NOT surfaced. Telling the
		// caller that sending failed for this identifier and not that one
		// is itself an enumeration signal.
		h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
			"Something went wrong. Please try again.", err)
		return
	}

	h.writeJSON(w, http.StatusOK, otpRequestResponse{
		Sent:      true,
		ExpiresIn: int((5 * time.Minute).Seconds()),
	})
}

// ─── POST /auth/otp/verify ───────────────────────────────────────────

type otpVerifyBody struct {
	Channel    string `json:"channel"`
	Identifier string `json:"identifier"`
	Code       string `json:"code"`
}

type tokenResponse struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
	// RefreshToken is omitted when it was set as a cookie, so a web
	// client cannot accidentally persist it somewhere readable.
	RefreshToken string       `json:"refresh_token,omitempty"`
	User         userResponse `json:"user"`
	IsNewUser    bool         `json:"is_new_user"`
}

// userResponse is what the auth endpoints return about a user.
//
// ID and role only. The profile endpoint returns the rest, deliberately:
// a login response is logged and screenshotted far more often than a
// profile fetch.
type userResponse struct {
	ID   uuid.UUID `json:"id"`
	Role string    `json:"role"`
}

func (h *Handler) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	var body otpVerifyBody
	if !h.decode(w, r, &body) {
		return
	}

	normalised, err := NormaliseIdentifier(body.Channel, body.Identifier)
	if err != nil {
		h.writeErr(w, r, http.StatusBadRequest, "VALIDATION_FAILED",
			"Enter a valid email address or phone number.", nil)
		return
	}

	res, err := h.limiter.Allow(r.Context(), ratelimit.OTPVerifyPerIdentifier, normalised)
	if err != nil {
		h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
			"Something went wrong. Please try again.", err)
		return
	}
	if !res.Allowed {
		h.writeRateLimited(w, r, res)
		return
	}

	ip := ClientIP(r, h.trustProxy)
	pair, user, isNew, err := h.svc.VerifyOTP(
		r.Context(), body.Channel, body.Identifier, body.Code,
		r.UserAgent(), HashIP(ip, h.ipSalt),
	)
	if err != nil {
		h.writeVerifyError(w, r, err)
		return
	}

	// A successful verification clears the request window, so someone who
	// fumbled a code and asked for another is not still throttled.
	_ = h.limiter.Reset(r.Context(), ratelimit.OTPVerifyPerIdentifier, normalised)

	h.setRefreshCookie(w, pair.RefreshToken)
	h.writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken: pair.AccessToken,
		ExpiresAt:   pair.ExpiresAt,
		User:        userResponse{ID: user.ID, Role: user.Role},
		IsNewUser:   isNew,
	})
}

func (h *Handler) writeVerifyError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrTooManyAttempts):
		h.writeErr(w, r, http.StatusTooManyRequests, "RATE_LIMITED",
			"Too many incorrect attempts. Request a new code.", nil)
	case errors.Is(err, ErrCodeIncorrect), errors.Is(err, ErrCodeNotFound):
		// One message for both. "No active code" versus "wrong code"
		// tells an attacker whether a code is currently in flight for
		// this identifier.
		h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED",
			"That code is not valid. Check it, or request a new one.", nil)
	case errors.Is(err, ErrUserSuspended):
		h.writeErr(w, r, http.StatusForbidden, "FORBIDDEN",
			"This account is not available. Contact support.", nil)
	case errors.Is(err, ErrInvalidIdentifier), errors.Is(err, ErrUnknownChannel):
		h.writeErr(w, r, http.StatusBadRequest, "VALIDATION_FAILED",
			"Enter a valid email address or phone number.", nil)
	default:
		h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
			"Something went wrong. Please try again.", err)
	}
}

// ─── POST /auth/refresh ──────────────────────────────────────────────

type refreshBody struct {
	// Optional. Web clients send the cookie; mobile sends this.
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var body refreshBody
	// A missing or malformed body is fine — the cookie may carry it.
	_ = json.NewDecoder(r.Body).Decode(&body)

	presented := body.RefreshToken
	fromCookie := false
	if presented == "" {
		if c, err := r.Cookie(refreshCookieName); err == nil {
			presented = c.Value
			fromCookie = true
		}
	}
	if presented == "" {
		h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED",
			"Sign in again to continue.", nil)
		return
	}

	res, err := h.limiter.Allow(r.Context(), ratelimit.RefreshPerSession, presented)
	if err != nil {
		h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
			"Something went wrong. Please try again.", err)
		return
	}
	if !res.Allowed {
		h.writeRateLimited(w, r, res)
		return
	}

	ip := ClientIP(r, h.trustProxy)
	pair, err := h.svc.Refresh(r.Context(), presented, r.UserAgent(), HashIP(ip, h.ipSalt))
	if err != nil {
		// Reuse and invalid look identical to the client. The difference
		// matters operationally, not to the caller — and saying "that
		// token was already used" confirms it was once real.
		h.clearRefreshCookie(w)
		if errors.Is(err, ErrRefreshReused) || errors.Is(err, ErrRefreshInvalid) {
			h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED",
				"Sign in again to continue.", nil)
			return
		}
		h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
			"Something went wrong. Please try again.", err)
		return
	}

	resp := tokenResponse{AccessToken: pair.AccessToken, ExpiresAt: pair.ExpiresAt}
	if fromCookie {
		h.setRefreshCookie(w, pair.RefreshToken)
	} else {
		resp.RefreshToken = pair.RefreshToken
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// ─── cookies ─────────────────────────────────────────────────────────

func (h *Handler) setRefreshCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:  refreshCookieName,
		Value: token,
		Path:  "/api/v1/auth",
		// httpOnly: JavaScript cannot read it, so an XSS bug is survivable
		// rather than a full account takeover.
		HttpOnly: true,
		Secure:   h.secure,
		// Strict, not Lax. There is no legitimate cross-site navigation
		// that should carry a refresh token, and Lax would send it on a
		// top-level GET from anywhere.
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(h.refreshTTL.Seconds()),
	})
}

func (h *Handler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/api/v1/auth",
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// ─── helpers ─────────────────────────────────────────────────────────

func (h *Handler) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	// Reject unknown fields: a client sending `{"identifer": ...}` gets
	// told rather than silently authenticating nobody.
	dec.DisallowUnknownFields()

	if err := dec.Decode(v); err != nil {
		h.writeErr(w, r, http.StatusBadRequest, "BAD_REQUEST",
			"The request body could not be read.", nil)
		return false
	}
	return true
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *Handler) writeRateLimited(w http.ResponseWriter, r *http.Request, res ratelimit.Result) {
	// Retry-After in seconds, rounded up: rounding down tells a client to
	// retry while still blocked, which produces a second 429 and looks
	// like the limiter is broken.
	seconds := int(res.RetryAfter.Seconds())
	if res.RetryAfter > 0 && seconds == 0 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	h.writeErr(w, r, http.StatusTooManyRequests, "RATE_LIMITED",
		"Too many requests. Try again shortly.", nil)
}

// ─── POST /auth/logout ───────────────────────────────────────────────

// SessionRevoker is what logout needs. Declared by the consumer; the
// implementation lives with the session storage.
type SessionRevoker interface {
	RevokeAll(ctx context.Context, userID uuid.UUID) error
}

// Logout ends the current session.
//
// Revokes by the presented refresh token rather than by user, so logging
// out on a phone does not sign the same person out on their laptop.
// Clearing the cookie alone would not do: the token would still be
// valid, and anyone who had copied it could keep refreshing.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	presented := h.presentedRefresh(r)
	h.clearRefreshCookie(w)

	if presented != "" {
		if err := h.svc.RevokeByToken(r.Context(), presented); err != nil {
			// Best-effort: the cookie is already cleared, so the client is
			// logged out from its own point of view. Failing the request
			// would leave the user staring at an error on a screen that
			// has, for them, already worked.
			h.svc.logger.WarnContext(r.Context(), "revoke session on logout", slog.Any("err", err))
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// LogoutAll ends every session for the authenticated user.
//
// Requires an access token, not just the refresh cookie: this is the
// "I think someone has my account" button, and it should not be
// triggerable by whoever holds a single stolen refresh token.
func (h *Handler) LogoutAll(revoker SessionRevoker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFrom(r.Context())
		if !ok {
			h.writeErr(w, r, http.StatusUnauthorized, "UNAUTHORIZED",
				"Authentication required.", nil)
			return
		}

		if err := revoker.RevokeAll(r.Context(), principal.UserID); err != nil {
			h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
				"Something went wrong. Please try again.", err)
			return
		}

		h.clearRefreshCookie(w)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) presentedRefresh(r *http.Request) string {
	var body refreshBody
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.RefreshToken != "" {
		return body.RefreshToken
	}
	if c, err := r.Cookie(refreshCookieName); err == nil {
		return c.Value
	}
	return ""
}
