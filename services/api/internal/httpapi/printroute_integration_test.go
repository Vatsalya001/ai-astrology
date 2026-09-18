//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/charts"
)

// The print route, against the real router.
//
// Everything here is a property of router.go rather than of the handler:
// whether the route is mounted where it claims to be, and whether the
// credential it accepts is the only way in. A test that mounted the
// handler itself would prove neither.

// ─── the mount ───────────────────────────────────────────────────────

/*
Every route that is NOT deliberately public must 401 without a token.

`TestNoPhase2RouteAnswersWithoutAToken` already checks this, but it
selects routes by a hand-written prefix list — `/birth-profiles`,
`/places`, `/charts`, `/astrology`. A route mounted at a path nobody
added to that list is not checked, and the test stays green while the
new endpoint serves birth data to the internet.

This one inverts it: walk EVERY route, and require 401 unless the
route is on an allowlist that says, in words, why it is public. The
allowlist is the thing a reviewer reads; forgetting to add a route to
it fails the test rather than skipping it.

This is not hypothetical. `/print/chart` was first written inside the
`/charts` subtree, under a comment claiming it sat outside
`authenticate` — which `r.Use` on that subtree made false. No existing
test noticed, because the prefix list put it in the "must be
authenticated" bucket where it happened to pass.
*/
func TestEveryRouteIsAuthenticatedUnlessItIsOnThePublicAllowlist(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	// method+" "+pattern → the reason it is reachable without a token.
	public := map[string]string{
		"GET /health": "liveness, called by the orchestrator before any user exists",
		"GET /ready":  "readiness, same caller",

		"GET /api/v1/meta": "build version and feature flags; the web app reads it " +
			"before anybody has signed in",

		"POST /api/v1/auth/otp/request": "how someone with no token asks for one",
		"POST /api/v1/auth/otp/verify":  "how that request becomes a token",
		"POST /api/v1/auth/refresh":     "takes a refresh token, not an access token",
		"POST /api/v1/auth/logout":      "an EXPIRED session must still be closable",

		"GET /api/v1/print/chart": "the single-use print token IS the credential; " +
			"headless Chrome on the PDF worker has no session to authenticate with",
	}

	var checked int
	var unexpected []string
	seen := make(map[string]bool, len(public))

	for _, route := range walk(t, h.routes) {
		key := route.method + " " + route.pattern
		path := strings.NewReplacer(
			"{id}", h.profile.String(),
			"{birthProfileId}", h.profile.String(),
		).Replace(route.pattern)

		rec := h.do(t, route.method, path, "", bodyFor(route.method, route.pattern, h.placeID))

		if _, ok := public[key]; ok {
			seen[key] = true
			/*
			   An allowlisted route must actually BE public.

			   Without this half the allowlist is a place to park a route
			   and stop thinking about it, and a route that is in fact
			   authenticated sits there forever implying it is not. It is
			   also what pins the property this file exists for:
			   /print/chart answers with no Authorization header.
			*/
			if refusedByAuthMiddleware(rec) {
				t.Errorf("%s is on the public allowlist but the auth middleware refused it.\n"+
					"Either it is authenticated after all — in which case remove it from the "+
					"allowlist — or it was mounted inside a subtree that applies "+
					"`authenticate` with r.Use, which no comment above the route can undo.\n"+
					"Reason recorded for it being public: %s\nBody: %s",
					key, public[key], strings.TrimSpace(rec.Body.String()))
			}
			continue
		}

		checked++
		if !refusedByAuthMiddleware(rec) {
			unexpected = append(unexpected, key+" → "+http.StatusText(rec.Code))
		}
	}

	sort.Strings(unexpected)
	if len(unexpected) > 0 {
		t.Fatalf("these routes were not refused by the auth middleware and are not on "+
			"the public allowlist:\n  %s\n\nAdd them to the allowlist WITH a reason, or "+
			"mount them behind `authenticate`.", strings.Join(unexpected, "\n  "))
	}

	/*
	   A stale allowlist entry is worse than none: it reads like coverage
	   of a route that is not mounted, so the reviewer stops looking. The
	   first draft of this test listed /health/live, /healthz and
	   /metrics, none of which exist here — and without this check that
	   draft would have passed while silently exercising three fewer
	   routes than it claimed.
	*/
	for key := range public {
		if !seen[key] {
			t.Errorf("the public allowlist names %q, which the route walk never saw. "+
				"Either the route was removed or the pattern is wrong; either way this "+
				"entry is checking nothing", key)
		}
	}

	if checked == 0 {
		t.Fatal("no authenticated routes were walked; the walk is matching nothing")
	}
	t.Logf("%d routes require a token, %d are deliberately public", checked, len(seen))
}

/*
Whether the AUTH MIDDLEWARE refused this request — not merely whether
the status was 401.

The distinction is load-bearing. `POST /api/v1/auth/refresh` is public
by design and still answers 401 when called with no refresh token,
because that is the correct answer to "refresh this absent session".
Treating any 401 as "authenticated" marks it as a contradiction and
fails the test for the wrong reason.

The two codes below are the only ones auth.Authenticate emits (see
internal/auth/middleware.go). Handlers write their own 401s with the
upper-case "UNAUTHORIZED", which is a different thing arriving from a
different place.
*/
func refusedByAuthMiddleware(rec *httptest.ResponseRecorder) bool {
	if rec.Code != http.StatusUnauthorized {
		return false
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		return false
	}
	return body.Error.Code == "unauthorized" || body.Error.Code == "token_expired"
}

// ─── the credential ──────────────────────────────────────────────────

// mintPrint issues a token the way the PDF worker does.
func (h *routerHarness) mintPrint(t *testing.T, userID, profileID uuid.UUID) string {
	t.Helper()
	token, err := h.printTokens.Mint(context.Background(), charts.PrintScope{
		UserID:    userID,
		ProfileID: profileID,
	})
	if err != nil {
		t.Fatalf("mint print token: %v", err)
	}
	return token
}

// printBundle is the shape the print page reads. Decoded loosely — this
// asserts on what the document needs, not on every field.
type printBundle struct {
	Profile struct {
		ID    uuid.UUID `json:"id"`
		Label string    `json:"label"`
	} `json:"birth_profile"`
	Charts map[string]struct {
		ID        uuid.UUID `json:"id"`
		ProfileID uuid.UUID `json:"birth_profile_id"`
		ChartType string    `json:"chart_type"`
	} `json:"charts"`
	Mahadashas []struct {
		Planet string `json:"planet"`
		Level  int    `json:"level"`
	} `json:"mahadashas"`
	Current *struct {
		Maha *struct {
			Planet string `json:"planet"`
		} `json:"mahadasha"`
	} `json:"current_dasha"`
}

func decodeBundle(t *testing.T, body string) printBundle {
	t.Helper()
	var out printBundle
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode print bundle: %v\nbody: %s", err, body)
	}
	return out
}

// A valid token serves the chart it names, with no session at all.
func TestThePrintRouteServesTheChartItsTokenNames(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	token := h.mintPrint(t, h.alice, h.profile)

	// Deliberately no Authorization header — the fourth argument is "".
	rec := h.do(t, http.MethodGet, "/api/v1/print/chart?token="+token, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("a valid print token returned %d, want 200: %s",
			rec.Code, strings.TrimSpace(rec.Body.String()))
	}

	bundle := decodeBundle(t, rec.Body.String())
	if bundle.Profile.ID != h.profile {
		t.Fatalf("served profile %s, want the one the token named (%s)",
			bundle.Profile.ID, h.profile)
	}
}

/*
One token, one render.

The token lives in a URL that is passed to a browser subprocess, so it
reaches a command line, an access log and a browser profile on disk.
Single use is what makes all three survivable: by the time anyone
reads it there, it has already been spent.
*/
func TestAPrintTokenWorksExactlyOnce(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	token := h.mintPrint(t, h.alice, h.profile)
	path := "/api/v1/print/chart?token=" + token

	if first := h.do(t, http.MethodGet, path, "", ""); first.Code != http.StatusOK {
		t.Fatalf("first use returned %d, want 200: %s",
			first.Code, strings.TrimSpace(first.Body.String()))
	}

	second := h.do(t, http.MethodGet, path, "", "")
	if second.Code != http.StatusNotFound {
		t.Fatalf("the SAME token worked a second time and returned %d, want 404. "+
			"A print token is single use; if this passes twice the URL in the worker's "+
			"process list is a live credential for as long as the TTL lasts", second.Code)
	}
}

/*
The bypass this route is shaped to make impossible.

The mistake is reading a profile id from the request and merely checking
that the token is valid. There is deliberately no profile id in this
route's path or query, so the mistake is unavailable rather than avoided
— and this test is what keeps it that way.

The target is ANOTHER PROFILE OF ALICE'S OWN, not a stranger's. That
matters. Every read in PrintBundle is scoped by user as well as profile,
so naming Bob's profile in Alice's token 404s on the user scope whether
or not the handler honoured the parameter — which is how the first draft
of this test "caught" the bypass for a reason unrelated to the property
it names. It would have kept passing with the bypass reintroduced
against any profile the token's own user happens to own.

Alice with two profiles is the product's multi-profile feature, not a
contrivance, and it is the case where honouring `?id=` actually changes
the answer.
*/
func TestAPrintTokenCannotBeAimedAtAnotherProfile(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	// A second profile for the same user, created the way the app does.
	second := h.createProfileOverHTTP(t, h.alice)
	if second == h.profile {
		t.Fatal("the second profile is the first one; this test would prove nothing")
	}

	for _, param := range []string{"id", "birthProfileId", "profile_id", "birth_profile_id"} {
		t.Run(param, func(t *testing.T) {
			// The token names Alice's FIRST profile throughout.
			token := h.mintPrint(t, h.alice, h.profile)

			rec := h.do(t, http.MethodGet,
				"/api/v1/print/chart?token="+token+"&"+param+"="+second.String(), "", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("returned %d, want 200: %s",
					rec.Code, strings.TrimSpace(rec.Body.String()))
			}

			bundle := decodeBundle(t, rec.Body.String())
			if bundle.Profile.ID == second {
				t.Fatalf("?%s= steered the response to a different profile of the same "+
					"user. The handler is reading the profile from the REQUEST; it must "+
					"come only from the redeemed token", param)
			}
			if bundle.Profile.ID != h.profile {
				t.Fatalf("served profile %s, want the one the token named (%s)",
					bundle.Profile.ID, h.profile)
			}
		})
	}
}

// createProfileOverHTTP adds a birth profile for a user through the real
// endpoint, so the row is built exactly as production builds it.
func (h *routerHarness) createProfileOverHTTP(t *testing.T, userID uuid.UUID) uuid.UUID {
	t.Helper()

	body := fmt.Sprintf(
		`{"label":"mother","birth_date":"1965-11-02","birth_time":"09:15",`+
			`"time_accuracy":"exact","place_id":%d}`, h.placeID)

	rec := h.do(t, http.MethodPost, "/api/v1/birth-profiles", h.token(t, userID), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create second profile returned %d: %s",
			rec.Code, strings.TrimSpace(rec.Body.String()))
	}

	var created struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created profile: %v\nbody: %s", err, rec.Body.String())
	}
	return created.ID
}

// A token minted for a user who does not own the profile reads nothing.
//
// Mint does not check ownership — it is called by the worker, which has
// already resolved both. This asserts the SECOND line of defence: the
// bundle's every read is scoped by user as well as profile, so a token
// carrying a mismatched pair is inert rather than powerful.
func TestAPrintTokenPairingTheWrongUserWithAProfileReadsNothing(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	token := h.mintPrint(t, h.bob, h.profile) // Bob does not own it.

	rec := h.do(t, http.MethodGet, "/api/v1/print/chart?token="+token, "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a token naming Bob and Alice's profile returned %d, want 404: %s",
			rec.Code, strings.TrimSpace(rec.Body.String()))
	}
}

// Garbage in the token parameter is a 404, and so is an absent one.
//
// 404 rather than 401 or 400: the rest of the product returns 404 for
// cross-user access so a status code cannot confirm a resource exists.
// Here it also refuses to confirm that a token was ever real, which is
// the difference between guessing and probing.
func TestARefusedPrintTokenLooksLikeAMissingPage(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	spent := h.mintPrint(t, h.alice, h.profile)
	if rec := h.do(t, http.MethodGet, "/api/v1/print/chart?token="+spent, "", ""); rec.Code != http.StatusOK {
		t.Fatalf("setup: spending the token returned %d", rec.Code)
	}

	cases := map[string]string{
		"absent":       "/api/v1/print/chart",
		"empty":        "/api/v1/print/chart?token=",
		"never minted": "/api/v1/print/chart?token=AAAAAAAAAAAAAAAAAAAAAA",
		"not base64":   "/api/v1/print/chart?token=%20%20%20",
		"already used": "/api/v1/print/chart?token=" + spent,
	}

	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			rec := h.do(t, http.MethodGet, path, "", "")
			if rec.Code != http.StatusNotFound {
				t.Fatalf("returned %d, want 404", rec.Code)
			}
			body := strings.TrimSpace(rec.Body.String())
			for _, leak := range []string{"expired", "spent", "redeem", "used", "token"} {
				if strings.Contains(strings.ToLower(body), leak) {
					t.Fatalf("the refusal says %q, which tells the caller WHICH kind of "+
						"invalid it was. All five cases must be indistinguishable. "+
						"Body: %s", leak, body)
				}
			}
		})
	}
}

// ─── the payload ─────────────────────────────────────────────────────

// One request has to carry the whole document, because there is only
// ever one request. A bundle missing a section is a PDF missing a page.
func TestThePrintBundleCarriesEverythingTheDocumentRenders(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	token := h.mintPrint(t, h.alice, h.profile)
	rec := h.do(t, http.MethodGet, "/api/v1/print/chart?token="+token, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("returned %d: %s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}

	bundle := decodeBundle(t, rec.Body.String())

	for _, chartType := range []string{"D1", "D9", "D10"} {
		chart, ok := bundle.Charts[chartType]
		if !ok {
			t.Errorf("the bundle has no %s. The print page cannot fetch it separately — "+
				"the token it arrived with has already been spent", chartType)
			continue
		}
		if chart.ChartType != chartType {
			t.Errorf("bundle.charts[%q] is labelled %q", chartType, chart.ChartType)
		}
		if chart.ProfileID != h.profile {
			t.Errorf("bundle.charts[%q] belongs to profile %s, want %s",
				chartType, chart.ProfileID, h.profile)
		}
	}

	if len(bundle.Mahadashas) == 0 {
		t.Error("the bundle carries no mahadashas, so the printed timeline is empty")
	}
	for _, period := range bundle.Mahadashas {
		if period.Level != 1 {
			t.Errorf("a level-%d period is in the mahadasha list; the printed timeline "+
				"is the top level only", period.Level)
		}
	}
}

/*
The response must never be cached.

It is birth data, reached with a credential in a query string, by a
browser whose cache is on the worker's disk. A shared cache keyed on
that URL would serve it again after the token had been redeemed and
deleted — defeating single use somewhere Redis cannot see.
*/
func TestThePrintResponseIsNotCacheable(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	token := h.mintPrint(t, h.alice, h.profile)
	rec := h.do(t, http.MethodGet, "/api/v1/print/chart?token="+token, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("returned %d", rec.Code)
	}

	cacheControl := rec.Header().Get("Cache-Control")
	if !strings.Contains(cacheControl, "no-store") {
		t.Fatalf("Cache-Control is %q, want it to contain no-store", cacheControl)
	}
}
