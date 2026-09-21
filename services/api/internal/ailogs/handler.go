package ailogs

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients/aiclient"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis/ratelimit"
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

	// Audit is nil only in tests that do not assert on the trail.
	// §14 pairs SUPER_ADMIN with "audit-logged in Go", and only the
	// first half was enforced — so the endpoint that spends real money
	// against the production provider left no record of who ran it.
	audit AuditRecorder
	ipOf  func(*http.Request) []byte

	// Nil in tests that do not exercise the playground's cost limit.
	// §14: "Rate limiting on the internal completion path (a runaway
	// loop is a real cost event)" — this is that path.
	limiter Limiter
}

// Limiter is the slice of the rate limiter this package uses.
//
// Declared by the CONSUMER, per .claude/rules/go.md, so a test double
// is one method.
type Limiter interface {
	Allow(ctx context.Context, rule ratelimit.Rule, subject string) (ratelimit.Result, error)
}

// WithLimiter attaches the playground's cost limit.
func (h *Handler) WithLimiter(l Limiter) *Handler {
	h.limiter = l
	return h
}

// AuditRecorder is the slice of platform/audit this package uses.
//
// Declared by the CONSUMER, per .claude/rules/go.md, so a test double
// is one method and this package does not depend on the recorder's
// other surface.
type AuditRecorder interface {
	Record(ctx context.Context, userID *uuid.UUID, action string, metadata map[string]any, ipHash []byte)
}

// HTTPErrorWriter is httpapi.WriteError, injected so this package does
// not import httpapi.
type HTTPErrorWriter func(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error)

func NewHandler(svc *Service, ai *clients.AI, writeErr HTTPErrorWriter) *Handler {
	return &Handler{svc: svc, ai: ai, writeErr: writeErr, now: time.Now}
}

// WithAudit wires the trail. Separate from the constructor so the
// existing call sites and tests keep working, and so a handler built
// without one is obviously un-audited rather than silently so.
func (h *Handler) WithAudit(recorder AuditRecorder, ipOf func(*http.Request) []byte) *Handler {
	h.audit = recorder
	h.ipOf = ipOf
	return h
}

// record writes one admin action.
//
// Every caller passes IDs, enums and counts. Never a message, never a
// model answer — an audit row outlives everything else in the system
// and is read by people who never saw this file. `audit.Record` has its
// own allowlist as a second line; this is the first.
func (h *Handler) record(r *http.Request, action string, metadata map[string]any) {
	if h.audit == nil {
		return
	}

	var userID *uuid.UUID
	if principal, ok := auth.PrincipalFrom(r.Context()); ok {
		id := principal.UserID
		userID = &id
	}

	var ipHash []byte
	if h.ipOf != nil {
		ipHash = h.ipOf(r)
	}

	h.audit.Record(r.Context(), userID, action, metadata, ipHash)
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

	h.record(r, "admin.ai.config_read", map[string]any{"outcome": "ok"})

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

	h.record(r, "admin.ai.usage_read", map[string]any{
		// The WINDOW, not the rows. How much history somebody pulled is
		// the auditable fact; what was in it is already in the table
		// they read.
		"window_days": int(to.Sub(from).Hours() / 24),
	})

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

	h.record(r, "admin.ai.incidents_read", map[string]any{"limit": int(limit)})

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

	// BEFORE the audit write and before the provider call: the point of
	// this limit is that the money is never spent, and an audit row for a
	// run that was refused would misreport what the operator actually
	// did.
	//
	// Keyed on the SUPER_ADMIN's id, not the IP — the cost is per
	// operator, and two admins behind one office NAT must not share a
	// budget.
	if h.limiter != nil {
		subject := "unknown"
		if principal, ok := auth.PrincipalFrom(r.Context()); ok {
			subject = principal.UserID.String()
		}
		result, err := h.limiter.Allow(r.Context(), ratelimit.AIPlaygroundPerAdmin, subject)
		if err != nil {
			// Fails CLOSED, unlike the global throttle and unlike
			// /recompute. Those protect availability; this protects a
			// bill. If Redis cannot tell us whether this operator has
			// already run twenty completions, the safe assumption on a
			// route that spends real money is that they have.
			h.writeErr(w, r, http.StatusServiceUnavailable, "RATE_LIMITER_DOWN",
				"The playground is unavailable while rate limiting is degraded.", err)
			return
		}
		if !result.Allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(result.RetryAfter.Seconds())+1))
			h.writeErr(w, r, http.StatusTooManyRequests, "RATE_LIMITED",
				"The playground has been run several times recently. Please wait.", nil)
			return
		}
	}

	req := aiclient.CompleteRequest{Message: body.Message}
	if body.Job != "" {
		job := aiclient.JobType(body.Job)
		req.Job = &job
	}

	// Recorded BEFORE the call, not after. This is the one admin action
	// that spends money, and a run that times out or crashes the process
	// is exactly the one an auditor needs to find — an after-the-fact
	// write would miss it.
	//
	// `message_chars` is a LENGTH. The prompt itself never goes in: an
	// audit row outlives everything else in the system, and an operator
	// pasting a real user's question into the playground must not make
	// that question permanent.
	h.record(r, "admin.ai.playground_run", map[string]any{
		"job":           jobLabel(body.Job),
		"message_chars": len(body.Message),
	})

	envelope, err := h.ai.Complete(r.Context(), req)
	if err != nil {
		h.record(r, "admin.ai.playground_failed", map[string]any{"outcome": "error"})
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

// ─── GET / PATCH /api/v1/admin/ai/routing ────────────────────────────
//
// §17: "Model router maps all 10 job types; overridable from admin
// without deploy." The mapping shipped; the override did not.
// `ModelRouter` took overrides only in its CONSTRUCTOR — which means a
// deploy — and the `PATCH /admin/ai/config` §10 lists never existed. The
// whole mechanism was a constructor argument that only tests passed.

func (h *Handler) GetRouting(w http.ResponseWriter, r *http.Request) {
	table, err := h.ai.Routing(r.Context())
	if err != nil {
		h.writeErr(w, r, aiStatus(err), "ai_unavailable", "Could not read routing.", err)
		return
	}

	h.record(r, "admin.ai.routing_read", map[string]any{"outcome": "ok"})
	writeJSON(w, http.StatusOK, table)
}

// PatchRouting changes a job's tier at runtime.
//
// The most consequential admin action after the playground: routing
// `premium_report` to `fast` makes a paid product cheap and bad, and
// routing `intent_classification` to `deep` makes a cheap product
// expensive. So the audit row carries the COUNT of jobs changed and
// whether it was a reset — enough to find the change, without copying a
// table that is already readable through GET.
func (h *Handler) PatchRouting(w http.ResponseWriter, r *http.Request) {
	var body aiclient.RoutingPatch
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		h.writeErr(w, r, http.StatusBadRequest, "invalid_body", "Could not read the request.", err)
		return
	}

	changed := 0
	if body.Overrides != nil {
		changed = len(*body.Overrides)
	}
	reset := body.Reset != nil && *body.Reset

	if changed == 0 && !reset {
		// Refused rather than treated as a no-op. A PATCH that changes
		// nothing and returns 200 is indistinguishable from one that
		// was silently dropped, and this is a control somebody reaches
		// for during an incident.
		h.writeErr(w, r, http.StatusBadRequest, "invalid_body",
			"Send at least one override, or reset=true.", nil)
		return
	}

	// Before the call, like the playground: a change that crashed
	// mid-flight is exactly the one an auditor needs to find.
	h.record(r, "admin.ai.routing_changed", map[string]any{
		"overrides": changed,
		"reset":     reset,
	})

	table, err := h.ai.PatchRouting(r.Context(), body)
	if err != nil {
		h.record(r, "admin.ai.routing_change_failed", map[string]any{"outcome": "error"})
		h.writeErr(w, r, aiStatus(err), "ai_failed", "Could not change routing.", err)
		return
	}

	writeJSON(w, http.StatusOK, table)
}
