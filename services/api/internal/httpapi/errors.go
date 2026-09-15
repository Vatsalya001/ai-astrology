package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/logging"
)

// ErrorCode is a stable, client-facing identifier for a failure class.
// Clients branch on these; they must not branch on message text.
type ErrorCode string

const (
	CodeBadRequest       ErrorCode = "BAD_REQUEST"
	CodeValidationFailed ErrorCode = "VALIDATION_FAILED"
	CodeUnauthorized     ErrorCode = "UNAUTHORIZED"
	CodeForbidden        ErrorCode = "FORBIDDEN"
	CodeNotFound         ErrorCode = "NOT_FOUND"
	CodeRateLimited      ErrorCode = "RATE_LIMITED"
	CodeInternal         ErrorCode = "INTERNAL_ERROR"
	CodeUnavailable      ErrorCode = "SERVICE_UNAVAILABLE"
)

// ErrorBody is the uniform error envelope for every endpoint.
//
// The message is deliberately generic. Stack traces, SQL, field names and
// internal service errors go to the server log — keyed by the same
// trace_id the client receives — and never into the response.
type ErrorBody struct {
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message"`
	TraceID   string    `json:"trace_id,omitempty"`
	RetryAble bool      `json:"retryable,omitempty"`
}

type errorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// WriteJSON writes a successful JSON response.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status line is already sent, so there is nothing useful to
		// tell the client. Record it and move on.
		slog.Error("encode response body", slog.Any("err", err))
	}
}

// WriteError writes the uniform error envelope.
//
// cause is logged, never sent. That separation is the whole point of this
// function existing.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code ErrorCode, message string, cause error) {
	traceID := logging.TraceIDFrom(r.Context())

	if cause != nil {
		slog.ErrorContext(r.Context(), "request failed",
			slog.String("code", string(code)),
			slog.Int("status", status),
			slog.String("path", r.URL.Path),
			slog.Any("err", cause),
		)
	}

	WriteJSON(w, status, errorEnvelope{Error: ErrorBody{
		Code:    code,
		Message: message,
		TraceID: traceID,
		// 501 is the exception among 5xx: "this server does not implement
		// this" is a permanent property of the deployment, not a blip.
		// Telling a client to retry a feature that is switched off is a
		// loop that never terminates.
		RetryAble: (status >= 500 && status != http.StatusNotImplemented) ||
			status == http.StatusTooManyRequests,
	}})
}
