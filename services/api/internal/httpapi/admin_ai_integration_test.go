//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// The admin AI routes, against the real router.
//
// PHASE-04 §14: "Admin AI routes SUPER_ADMIN only, audit-logged in Go."
// The first half is what this file pins. Everything here is a property
// of router.go rather than of the handlers — whether the guard is
// mounted, and whether it admits anyone it should not.

// adminRoutes is every path in the subtree.
//
// Listed explicitly rather than walked, because the point is that each
// one is behind the SAME guard. A walk would pass if a new route were
// mounted outside the group, which is precisely the mistake
// `r.Use`-on-the-group exists to prevent — and the mistake that a
// per-route check invites.
var adminRoutes = []struct {
	method string
	path   string
}{
	{http.MethodGet, "/api/v1/admin/ai/config"},
	{http.MethodGet, "/api/v1/admin/ai/usage"},
	{http.MethodGet, "/api/v1/admin/ai/incidents"},
	{http.MethodPost, "/api/v1/admin/ai/test"},
}

func TestAdminAIRoutesRefuseEveryRoleBelowSuperAdmin(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	// `auth.RequireRole` compares roles exactly with no hierarchy, so
	// ADMIN is genuinely excluded rather than admitted by an ordering
	// that does not exist. The playground spends real money against the
	// production provider, which is why the line is drawn above admin.
	for _, role := range []string{auth.RoleUser, auth.RoleAstrologer, auth.RoleAdmin} {
		token := h.tokenAs(t, h.alice, role)

		for _, route := range adminRoutes {
			t.Run(role+" "+route.path, func(t *testing.T) {
				rec := h.do(t, route.method, route.path, token, "{}")

				if rec.Code != http.StatusForbidden {
					t.Errorf("%s %s with role %q: want 403, got %d",
						route.method, route.path, role, rec.Code)
				}
			})
		}
	}
}

func TestAdminAIRoutesRefuseAnUnauthenticatedCaller(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	for _, route := range adminRoutes {
		t.Run(route.path, func(t *testing.T) {
			rec := h.do(t, route.method, route.path, "", "{}")

			// 401 and not 403: without a Principal there is nobody to
			// deny, and reporting 403 would imply an identity that does
			// not exist.
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s %s with no token: want 401, got %d",
					route.method, route.path, rec.Code)
			}
		})
	}
}

// TestSuperAdminReachesTheAdminRoutes is the negative case, and without
// it every test above would pass against a subtree that was never
// mounted at all.
//
// The assertion is "not 401 and not 403" rather than 200: ai-service is
// a dead stub in this harness, so `/config` and `/test` answer 503 and
// that is correct. What is being checked is that the request got PAST
// the guard.
func TestSuperAdminReachesTheAdminRoutes(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	token := h.tokenAs(t, h.alice, auth.RoleSuperAdmin)

	for _, route := range adminRoutes {
		t.Run(route.path, func(t *testing.T) {
			rec := h.do(t, route.method, route.path, token, `{"message":"hello"}`)

			if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
				t.Errorf("%s %s: super_admin was refused with %d — the route is either "+
					"not mounted or behind the wrong role",
					route.method, route.path, rec.Code)
			}
		})
	}
}

// TestTheUsageRouteRefusesAnUnboundedWindow.
//
// The table grows by a row per model call, so an unbounded window is a
// full scan triggered by a query parameter — and it gets slower every
// day the product runs.
func TestTheUsageRouteRefusesAnUnboundedWindow(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	token := h.tokenAs(t, h.alice, auth.RoleSuperAdmin)

	rec := h.do(t, http.MethodGet,
		"/api/v1/admin/ai/usage?from=1990-01-01T00:00:00Z&to=2030-01-01T00:00:00Z",
		token, "")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("a forty-year window was accepted with %d", rec.Code)
	}
}

// TestAdminUsageAndIncidentsReturnRealRows closes the gap between
// "reachable" and "working".
//
// TestSuperAdminReachesTheAdminRoutes asserts only that the guard let
// the request through — it would pass against handlers that returned an
// empty object forever. These two endpoints read the database, so this
// writes a row and reads it back through the HTTP layer.
func TestAdminUsageAndIncidentsReturnRealRows(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	ctx := context.Background()
	q := dbgen.New(h.pool)

	// One clean call and one that failed validation, so the summary has
	// something to total and the incident feed has something to select.
	for _, row := range []struct {
		trace  string
		cost   int64
		passed bool
		flags  string
	}{
		{"admin-ok", 1_500, true, `[]`},
		{"admin-blocked", 2_500, false, `[{"type":"fabricated_chart_fact","severity":"block"}]`},
	} {
		_, err := q.InsertAIRequestLog(ctx, dbgen.InsertAIRequestLogParams{
			TraceID:        row.trace,
			JobType:        "chat_response",
			ProviderID:     "mock",
			Model:          "mock-chat",
			Tier:           "chat",
			PromptVersion:  "v1",
			ContextVersion: "none",
			InputTokens:    100,
			OutputTokens:   50,
			CachedTokens:   900,
			LatencyMs:      1200,
			CostMicros:     row.cost,
			FinishReason:   "stop",
			SafetyFlags:    []byte(row.flags),
			ModelCalls:     2,
			// ValidationPassed defaults TRUE in the column, so the clean
			// row needs no value and the failed one must be explicit.
			ValidationPassed: row.passed,
		})
		if err != nil {
			t.Fatalf("seed %s: %v", row.trace, err)
		}
	}

	token := h.tokenAs(t, h.alice, auth.RoleSuperAdmin)

	t.Run("usage totals the window", func(t *testing.T) {
		rec := h.do(t, http.MethodGet, "/api/v1/admin/ai/usage", token, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body struct {
			Summary struct {
				RequestCount         int64   `json:"request_count"`
				CostMicros           int64   `json:"cost_micros"`
				FailedCount          int64   `json:"failed_count"`
				CacheHitRate         float64 `json:"cache_hit_rate"`
				CostPerRequestMicros int64   `json:"cost_per_request_micros"`
			} `json:"summary"`
			ByJob []struct {
				JobType    string `json:"job_type"`
				CostMicros int64  `json:"cost_micros"`
			} `json:"by_job"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if body.Summary.RequestCount != 2 {
			t.Errorf("want 2 requests, got %d", body.Summary.RequestCount)
		}
		if body.Summary.CostMicros != 4_000 {
			t.Errorf("want 4000 micros, got %d", body.Summary.CostMicros)
		}
		if body.Summary.CostPerRequestMicros != 2_000 {
			t.Errorf("want 2000 per request, got %d", body.Summary.CostPerRequestMicros)
		}
		if body.Summary.FailedCount != 1 {
			t.Errorf("want 1 failure, got %d", body.Summary.FailedCount)
		}
		// 1800 cached against 200 fresh across the two rows.
		if body.Summary.CacheHitRate < 0.89 || body.Summary.CacheHitRate > 0.91 {
			t.Errorf("want a cache hit rate near 0.9, got %v", body.Summary.CacheHitRate)
		}
		if len(body.ByJob) != 1 || body.ByJob[0].JobType != "chat_response" {
			t.Errorf("by_job did not group: %+v", body.ByJob)
		}
	})

	t.Run("incidents select only the failures", func(t *testing.T) {
		rec := h.do(t, http.MethodGet, "/api/v1/admin/ai/incidents", token, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body struct {
			Incidents []struct {
				TraceID     string `json:"trace_id"`
				SafetyFlags []struct {
					Type     string `json:"type"`
					Severity string `json:"severity"`
				} `json:"safety_flags"`
			} `json:"incidents"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if len(body.Incidents) != 1 {
			t.Fatalf("want only the failed row, got %d incidents", len(body.Incidents))
		}
		if body.Incidents[0].TraceID != "admin-blocked" {
			t.Errorf("the wrong row was selected: %q", body.Incidents[0].TraceID)
		}
		if len(body.Incidents[0].SafetyFlags) != 1 ||
			body.Incidents[0].SafetyFlags[0].Type != "fabricated_chart_fact" {
			t.Errorf("the flags were lost: %+v", body.Incidents[0].SafetyFlags)
		}

		// The rule this whole subtree exists under: no message content,
		// anywhere in the response.
		if strings.Contains(rec.Body.String(), "excerpt") {
			t.Errorf("an incident carried an excerpt: %s", rec.Body.String())
		}
	})
}
