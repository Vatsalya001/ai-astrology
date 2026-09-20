//go:build integration

package httpapi

import (
	"net/http"
	"testing"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"
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
