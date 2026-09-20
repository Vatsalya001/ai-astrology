package ailogs

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
)

// maxWindow bounds how much history one usage query may scan.
//
// The table grows by a row per model call, so an unbounded window is a
// full scan that gets slower every day and is triggered by a query
// parameter. Ninety days is longer than any billing period and short
// enough to stay on the index.
const maxWindow = 90 * 24 * time.Hour

// window parses ?from and ?to, defaulting to the last 24 hours.
//
// Defaults rather than requiring both, because the commonest use is
// "what is happening today" and making an operator construct two RFC3339
// timestamps for that is friction with no safety value.
func (h *Handler) window(r *http.Request) (time.Time, time.Time, error) {
	now := h.now().UTC()
	to := now
	from := now.Add(-24 * time.Hour)

	if raw := r.URL.Query().Get("from"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("from must be an RFC3339 timestamp")
		}
		from = parsed.UTC()
	}
	if raw := r.URL.Query().Get("to"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("to must be an RFC3339 timestamp")
		}
		to = parsed.UTC()
	}

	if !to.After(from) {
		// Refused rather than swapped. A reversed window is a caller bug
		// and silently fixing it returns data for a period nobody asked
		// about, which is worse than an error.
		return time.Time{}, time.Time{}, fmt.Errorf("to must be after from")
	}
	if to.Sub(from) > maxWindow {
		return time.Time{}, time.Time{}, fmt.Errorf(
			"window may not exceed %d days", int(maxWindow.Hours()/24))
	}

	return from, to, nil
}

// clampQueryInt reads a bounded integer query parameter.
//
// Clamps rather than rejecting: a pagination parameter slightly out of
// range is not worth a 400, and clamping `limit=99999` to 200 is what
// the caller would have done anyway. A NON-numeric value takes the
// default for the same reason.
func clampQueryInt(r *http.Request, key string, fallback, minimum, maximum int32) int32 {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return fallback
	}
	value := int32(parsed)
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

// splitDetail unpacks "provider · tier" from the health probe.
//
// Falls back to putting the whole string in `provider` rather than
// returning two empty fields: a shape change upstream should degrade the
// display, not blank it.
func splitDetail(detail string) (provider, tier string) {
	parts := strings.SplitN(detail, " · ", 2)
	if len(parts) != 2 {
		return detail, ""
	}
	return parts[0], parts[1]
}

// aiStatus maps a client error to the status the admin panel should see.
//
// PHASE-04 §2: exhaustion maps to a 503 with a retryable flag "so the UI
// shows a real message rather than a spinner that never resolves". The
// local rate limit is a 429, which is a different instruction to the
// caller: wait and retry, rather than something is down.
func aiStatus(err error) int {
	switch {
	case errors.Is(err, clients.ErrAIRateLimited):
		return http.StatusTooManyRequests
	case errors.Is(err, clients.ErrAIUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, clients.ErrAIRejected):
		// Our request was wrong, and the admin panel built it — so this
		// is a bug here, not a caller error, and a 500 is what says so.
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The error is deliberately unhandled: the status line is already
	// written, so there is nothing left to tell the client. The
	// connection failing mid-body is the transport's problem and is
	// already visible in access logs.
	_ = json.NewEncoder(w).Encode(body)
}

// jobLabel renders a job for the audit trail.
//
// "default" rather than "" for an absent job: an empty string in an
// audit row reads as data loss, and the absence genuinely means the
// service's own default was used.
func jobLabel(job string) string {
	if job == "" {
		return "default"
	}
	return job
}
