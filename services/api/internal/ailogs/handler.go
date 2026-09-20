package ailogs

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients/aiclient"
)

// Handler serves the admin AI views.
//
// SUPER_ADMIN only, mounted behind auth.RequireRole — enforced at the
// router rather than per-handler, because a check each handler remembers
// is a check one handler forgets. PHASE-04 §10.
type Handler struct {
	svc      *Service
	ai       *clients.AI
	writeErr HTTPErrorWriter
	now      func() time.Time
}

// HTTPErrorWriter is httpapi.WriteError, injected so this package does
// not import httpapi.
type HTTPErrorWriter func(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error)

func NewHandler(svc *Service, ai *clients.AI, writeErr HTTPErrorWriter) *Handler {
	return &Handler{svc: svc, ai: ai, writeErr: writeErr, now: time.Now}
}

// WithClock replaces the clock, so a test can ask about a fixed window.
func (h *Handler) WithClock(now func() time.Time) *Handler {
	if now != nil {
		h.now = now
	}
	return h
}

// ─── GET /api/v1/admin/ai/config ─────────────────────────────────────

// ConfigResponse is what ai-service reports about itself.
//
// Read from the live service rather than from this process's env: the
// question an operator is asking is "what is ai-service actually using",
// and reproducing its configuration here would answer a different
// question that happens to look the same.
type ConfigResponse struct {
	Provider     string `json:"provider"`
	ProviderTier string `json:"provider_tier"`
	Status       string `json:"status"`

	// Local, and therefore known here rather than there.
	MaxConcurrentCompletions int `json:"max_concurrent_completions"`
	InFlightCompletions      int `json:"in_flight_completions"`
}

func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	detail, err := h.ai.Health(r.Context())
	if err != nil {
		// 503 rather than 500. The admin panel is not broken; the
		// service it reports on is unreachable, and those want different
		// responses from an operator.
		h.writeErr(w, r, http.StatusServiceUnavailable, "ai_unavailable",
			"ai-service is not reachable.", err)
		return
	}

	provider, tier := splitDetail(detail)
	writeJSON(w, http.StatusOK, ConfigResponse{
		Provider:                 provider,
		ProviderTier:             tier,
		Status:                   "ok",
		MaxConcurrentCompletions: clients.MaxConcurrentCompletions(),
		InFlightCompletions:      h.ai.InFlight(),
	})
}

// ─── GET /api/v1/admin/ai/usage ──────────────────────────────────────

type UsageResponse struct {
	From    time.Time  `json:"from"`
	To      time.Time  `json:"to"`
	Summary Usage      `json:"summary"`
	ByJob   []JobUsage `json:"by_job"`
}

func (h *Handler) GetUsage(w http.ResponseWriter, r *http.Request) {
	from, to, err := h.window(r)
	if err != nil {
		h.writeErr(w, r, http.StatusBadRequest, "invalid_window", err.Error(), err)
		return
	}

	summary, err := h.svc.UsageSummary(r.Context(), from, to)
	if err != nil {
		h.writeErr(w, r, http.StatusInternalServerError, "usage_failed",
			"Could not read usage.", err)
		return
	}

	byJob, err := h.svc.UsageByJob(r.Context(), from, to)
	if err != nil {
		h.writeErr(w, r, http.StatusInternalServerError, "usage_failed",
			"Could not read usage.", err)
		return
	}

	writeJSON(w, http.StatusOK, UsageResponse{
		From: from, To: to, Summary: summary, ByJob: byJob,
	})
}

// ─── GET /api/v1/admin/ai/incidents ──────────────────────────────────

type IncidentsResponse struct {
	Incidents []Incident `json:"incidents"`
	Limit     int32      `json:"limit"`
	Offset    int32      `json:"offset"`
}

func (h *Handler) GetIncidents(w http.ResponseWriter, r *http.Request) {
	limit := clampQueryInt(r, "limit", 50, 1, 200)
	offset := clampQueryInt(r, "offset", 0, 0, 100_000)

	incidents, err := h.svc.Incidents(r.Context(), limit, offset)
	if err != nil {
		h.writeErr(w, r, http.StatusInternalServerError, "incidents_failed",
			"Could not read incidents.", err)
		return
	}

	writeJSON(w, http.StatusOK, IncidentsResponse{
		Incidents: incidents, Limit: limit, Offset: offset,
	})
}

// ─── POST /api/v1/admin/ai/test — the playground ─────────────────────

// PlaygroundRequest is a prompt to run against the configured provider.
//
// PHASE-04 §14: "Playground cannot be pointed at real user data." There
// is deliberately no `user_id` field. The completion runs with no chart
// context at all, which means the output validator's empty-index rule
// treats any personal placement in the answer as a fabrication — so the
// playground cannot even accidentally produce a reading about a real
// person.
type PlaygroundRequest struct {
	Message string `json:"message"`
	Job     string `json:"job,omitempty"`
}

type PlaygroundResponse struct {
	Result    aiclient.CompleteResult `json:"result"`
	Telemetry aiclient.Telemetry      `json:"telemetry"`
}

func (h *Handler) Playground(w http.ResponseWriter, r *http.Request) {
	var body PlaygroundRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		h.writeErr(w, r, http.StatusBadRequest, "invalid_body", "Could not read the request.", err)
		return
	}
	if body.Message == "" {
		h.writeErr(w, r, http.StatusBadRequest, "invalid_body", "A message is required.", nil)
		return
	}

	req := aiclient.CompleteRequest{Message: body.Message}
	if body.Job != "" {
		job := aiclient.JobType(body.Job)
		req.Job = &job
	}

	envelope, err := h.ai.Complete(r.Context(), req)
	if err != nil {
		h.writeErr(w, r, aiStatus(err), "ai_failed", "The completion failed.", err)
		return
	}

	// NOT recorded in ai_request_logs. A playground run is an operator
	// experimenting, and mixing it into the usage table would corrupt
	// the cost-per-request figure that table exists to produce — and
	// would do it invisibly, because the rows look identical.
	writeJSON(w, http.StatusOK, PlaygroundResponse{
		Result:    envelope.Result,
		Telemetry: envelope.Telemetry,
	})
}
