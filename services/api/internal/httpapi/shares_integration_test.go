//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/shares"
)

/*
Share links, end to end through the real router.

The properties worth proving here are all authorisation properties, and
every one of them lives in a query predicate or a row rather than in Go
— which is exactly why they are tested against real Postgres. A mock
would be asserting that this file understands its own fake.
*/

type shareResponse struct {
	ID        string     `json:"id"`
	ProfileID string     `json:"birth_profile_id"`
	Scope     string     `json:"scope"`
	Token     string     `json:"token"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	ViewCount int64      `json:"view_count"`
}

func (h *routerHarness) createShare(t *testing.T, userID uuid.UUID, body string) shareResponse {
	t.Helper()

	rec := h.do(t, http.MethodPost,
		"/api/v1/charts/"+h.profile.String()+"/shares", h.token(t, userID), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create share returned %d: %s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}

	var share shareResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &share); err != nil {
		t.Fatalf("decode share: %v — body %s", err, rec.Body.String())
	}
	if share.Token == "" {
		t.Fatal("the create response carries no token, so there is no link to send")
	}
	return share
}

// ─── the happy path ──────────────────────────────────────────────────

func TestASharedLinkServesTheChartToSomebodyWithNoAccount(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	share := h.createShare(t, h.alice, "")

	rec := h.do(t, http.MethodGet, "/api/v1/shared/"+share.Token, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("a valid share link returned %d, want 200: %s",
			rec.Code, strings.TrimSpace(rec.Body.String()))
	}

	var body struct {
		Scope string `json:"scope"`
		Chart struct {
			Label     string          `json:"label"`
			ChartType string          `json:"chart_type"`
			Data      json.RawMessage `json:"chart_data"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v — body %s", err, rec.Body.String())
	}

	if body.Scope != shares.ScopeChart {
		t.Fatalf("scope %q, want %q", body.Scope, shares.ScopeChart)
	}
	if len(body.Chart.Data) == 0 {
		t.Fatal("the shared response carries no chart data; there is nothing to render")
	}
	if body.Chart.ChartType != "D1" {
		t.Fatalf("shared chart type %q, want D1", body.Chart.ChartType)
	}
}

/*
The response carries no birth details.

The specification's checklist: share links "do not embed birth details".
That is written about the URL, and the URL satisfies it by carrying
nothing but a token — but a payload handing over the date, time and place
would defeat the point entirely. Birth date plus time plus place is, in
combination, close to a unique identifier, and a share link ends up in
group chats.

── Why this compares against the owner's OWN response ──

The first version scanned the body for the substring "longitude" and
failed — on `planets[].longitude`, which is a planet's position along the
ecliptic and the entire content of a chart. A birthplace's longitude and
a planet's share a word and have nothing else in common.

So the check is by VALUE, taken from the profile the owner can actually
read, rather than by guessing at field names. It is also self-
calibrating: if the seed profile changes, this test changes with it
instead of silently checking for values nothing produces.
*/
func TestASharedResponseCarriesNoBirthDetails(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	// What the OWNER is allowed to see, straight from the API.
	ownerView := h.do(t, http.MethodGet,
		"/api/v1/birth-profiles/"+h.profile.String(), h.token(t, h.alice), "")
	if ownerView.Code != http.StatusOK {
		t.Fatalf("reading the owner's own profile returned %d", ownerView.Code)
	}
	var profile map[string]any
	if err := json.Unmarshal(ownerView.Body.Bytes(), &profile); err != nil {
		t.Fatalf("decode owner profile: %v", err)
	}

	share := h.createShare(t, h.alice, "")
	rec := h.do(t, http.MethodGet, "/api/v1/shared/"+share.Token, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("returned %d", rec.Code)
	}
	body := rec.Body.String()

	/*
	   The identifying fields, by the VALUE the owner's profile holds.

	   `label` is deliberately absent from this list: the owner chose it
	   and chose to share it, and a chart with no name on it is not much
	   of a thing to send anybody.
	*/
	for _, field := range []string{
		"birth_date", "birth_time", "birth_place",
		"latitude", "longitude", "timezone", "utc_instant",
	} {
		value, ok := profile[field]
		if !ok || value == nil {
			continue
		}
		rendered := fmt.Sprintf("%v", value)
		if rendered == "" || rendered == "0" {
			continue
		}
		if strings.Contains(body, rendered) {
			t.Errorf("the shared response contains the profile's %s (%q). Birth date "+
				"plus time plus place is close to a unique identifier, and this "+
				"link ends up in group chats.\nBody: %s", field, rendered, body)
		}
	}

	// And the shared chart object exposes only the keys it is meant to.
	// A field added later — by a struct change or an embedded type —
	// fails here rather than shipping.
	var envelope struct {
		Chart map[string]json.RawMessage `json:"chart"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	allowed := map[string]bool{
		"label": true, "chart_type": true, "chart_data": true,
		"ayanamsa": true, "house_system": true, "engine_version": true,
	}
	for key := range envelope.Chart {
		if !allowed[key] {
			t.Errorf("the shared chart exposes an unexpected field %q. Every field "+
				"here is visible to anyone holding the link; adding one is a "+
				"decision, not a refactor", key)
		}
	}
}

// The URL itself carries nothing but the token — no ids to edit.
func TestAShareLinkCarriesNothingButItsToken(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	share := h.createShare(t, h.alice, "")

	for name, value := range map[string]string{
		"the profile id": h.profile.String(),
		"the user id":    h.alice.String(),
		"the share id":   share.ID,
	} {
		if strings.Contains(share.Token, value) {
			t.Errorf("the token contains %s: %q", name, share.Token)
		}
	}
}

// ─── revocation, which is the only real remedy ───────────────────────

/*
Revoking kills the link immediately.

Once a link has left the owner's phone this is the ONLY thing they can
do about it. If revocation were eventually-consistent, or cached, or
checked in Go rather than in the query, it would not be a remedy.
*/
func TestRevokingAShareKillsItImmediately(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	share := h.createShare(t, h.alice, "")

	if rec := h.do(t, http.MethodGet, "/api/v1/shared/"+share.Token, "", ""); rec.Code != http.StatusOK {
		t.Fatalf("setup: the link did not work before revocation (%d)", rec.Code)
	}

	rec := h.do(t, http.MethodDelete,
		"/api/v1/charts/"+h.profile.String()+"/shares/"+share.ID, h.token(t, h.alice), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke returned %d: %s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}

	after := h.do(t, http.MethodGet, "/api/v1/shared/"+share.Token, "", "")
	if after.Code != http.StatusNotFound {
		t.Fatalf("a revoked link still returned %d, want 404. Revocation is the only "+
			"remedy an owner has once a link has left their phone", after.Code)
	}
}

// Revoking twice keeps the first timestamp — "when did I turn this off"
// must not be rewritten by a second click.
func TestRevokingIsIdempotent(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	share := h.createShare(t, h.alice, "")
	path := "/api/v1/charts/" + h.profile.String() + "/shares/" + share.ID

	first := h.do(t, http.MethodDelete, path, h.token(t, h.alice), "")
	if first.Code != http.StatusOK {
		t.Fatalf("first revoke returned %d", first.Code)
	}
	var one shareResponse
	_ = json.Unmarshal(first.Body.Bytes(), &one)

	second := h.do(t, http.MethodDelete, path, h.token(t, h.alice), "")
	if second.Code != http.StatusOK {
		t.Fatalf("second revoke returned %d, want 200 — revoking an already-revoked "+
			"link is not an error", second.Code)
	}
	var two shareResponse
	_ = json.Unmarshal(second.Body.Bytes(), &two)

	if one.RevokedAt == nil || two.RevokedAt == nil {
		t.Fatal("a revoked share reports no revoked_at")
	}
	if !one.RevokedAt.Equal(*two.RevokedAt) {
		t.Fatalf("the second revoke rewrote the timestamp: %s then %s. \"When did I "+
			"turn this off\" is a question the owner asks", one.RevokedAt, two.RevokedAt)
	}
}

// A stranger cannot revoke somebody else's link.
func TestAStrangerCannotRevokeAShare(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	share := h.createShare(t, h.alice, "")

	/*
	   Bob asks about ALICE'S profile, so the ownership middleware refuses
	   him first and this would pass without the query's own user
	   predicate. The second half below is what actually tests the
	   predicate: Bob revoking Alice's share id while naming his OWN
	   profile, which the middleware happily allows.
	*/
	rec := h.do(t, http.MethodDelete,
		"/api/v1/charts/"+h.profile.String()+"/shares/"+share.ID, h.token(t, h.bob), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a stranger revoking via the owner's profile returned %d, want 404", rec.Code)
	}

	bobProfile := h.createProfileOverHTTP(t, h.bob)
	rec = h.do(t, http.MethodDelete,
		"/api/v1/charts/"+bobProfile.String()+"/shares/"+share.ID, h.token(t, h.bob), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a stranger revoked Alice's share through his own profile and got %d, "+
			"want 404. The middleware cannot catch this — a share id is not a "+
			"profile — so the user predicate in the query is the only thing "+
			"stopping it", rec.Code)
	}

	// And the link still works, so the 404 was a refusal rather than a
	// silent success reported as an error.
	if still := h.do(t, http.MethodGet, "/api/v1/shared/"+share.Token, "", ""); still.Code != http.StatusOK {
		t.Fatalf("the stranger's refused revoke killed the link anyway (view returned %d)",
			still.Code)
	}
}

// ─── correcting birth details kills the links ────────────────────────

/*
A link stops working when the chart it points at is superseded.

Correcting birth details creates a NEW profile version. A link
pointing at the old one would keep serving a chart its owner has
already decided was wrong — to people they may have no way to reach.
*/
func TestCorrectingBirthDetailsRevokesTheProfilesLinks(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	share := h.createShare(t, h.alice, "")
	if rec := h.do(t, http.MethodGet, "/api/v1/shared/"+share.Token, "", ""); rec.Code != http.StatusOK {
		t.Fatalf("setup: the link did not work (%d)", rec.Code)
	}

	body := fmt.Sprintf(
		`{"birth_date":"1990-03-15","birth_time":"07:45","time_accuracy":"exact","place_id":%d}`,
		h.placeID)
	patch := h.do(t, http.MethodPatch,
		"/api/v1/birth-profiles/"+h.profile.String(), h.token(t, h.alice), body)
	if patch.Code != http.StatusOK {
		t.Fatalf("correcting birth details returned %d: %s",
			patch.Code, strings.TrimSpace(patch.Body.String()))
	}

	after := h.do(t, http.MethodGet, "/api/v1/shared/"+share.Token, "", "")
	if after.Code != http.StatusNotFound {
		t.Fatalf("a link to the OLD profile version still returned %d after the birth "+
			"time was corrected, want 404. It is serving a chart its owner has "+
			"already decided was wrong", after.Code)
	}
}

// ─── refusals ────────────────────────────────────────────────────────

// Every kind of dead or invented link is the same answer.
func TestADeadOrInventedLinkIsIndistinguishable(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	revoked := h.createShare(t, h.alice, "")
	if rec := h.do(t, http.MethodDelete,
		"/api/v1/charts/"+h.profile.String()+"/shares/"+revoked.ID,
		h.token(t, h.alice), ""); rec.Code != http.StatusOK {
		t.Fatalf("setup revoke returned %d", rec.Code)
	}

	cases := map[string]string{
		"revoked":       revoked.Token,
		"never existed": "AAAAAAAAAAAAAAAAAAAAAA",
		"too short":     "abc",
		"not base64":    "%20%20%20",
	}

	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			rec := h.do(t, http.MethodGet, "/api/v1/shared/"+token, "", "")
			if rec.Code != http.StatusNotFound {
				t.Fatalf("returned %d, want 404", rec.Code)
			}
			lower := strings.ToLower(strings.TrimSpace(rec.Body.String()))
			for _, leak := range []string{"revoked", "expired", "share", "token"} {
				if strings.Contains(lower, leak) {
					t.Fatalf("the refusal says %q, so a viewer can tell WHICH kind of "+
						"dead this link is — including that it was once real and its "+
						"owner turned it off. Body: %s", leak, rec.Body.String())
				}
			}
		})
	}
}

// A share cannot be created for somebody else's profile.
func TestAStrangerCannotCreateAShareForAnotherProfile(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	rec := h.do(t, http.MethodPost,
		"/api/v1/charts/"+h.profile.String()+"/shares", h.token(t, h.bob), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a stranger creating a share for somebody else's profile returned %d, "+
			"want 404", rec.Code)
	}
}

// ─── the listing ─────────────────────────────────────────────────────

/*
Listing links never returns a token, or anything shaped like one.

The plaintext exists exactly once, in the response to Create. If a
listing returned it, every subsequent read of the share screen would be
another chance for it to be cached, logged or screenshotted — and "we
cannot show you that link again" would stop being true.

── Why this checks more than the plaintext ──

The first version asserted only that the body did not contain the exact
token string, and that it did not contain the literal "token_hash". Both
passed while the listing handed back `row.TokenHash` under the `token`
key: the hash is not the plaintext, and the JSON key is `token`, not
`token_hash`.

So it now asserts the property directly — every listed share's `token`
field is empty — and additionally that no 64-character hex string
appears anywhere, which is what the stored hash looks like.
*/
func TestListingSharesNeverReturnsAToken(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	created := h.createShare(t, h.alice, "")

	rec := h.do(t, http.MethodGet,
		"/api/v1/charts/"+h.profile.String()+"/shares", h.token(t, h.alice), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list returned %d: %s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	body := rec.Body.String()

	var listed struct {
		Shares []map[string]json.RawMessage `json:"shares"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode listing: %v — body %s", err, body)
	}
	/*
	   The share this test created must be IN the listing — not the only
	   thing in it.

	   The harness seeds a share of its own so the route walk has a
	   {shareId} to address, so "want exactly 1" was an assumption about
	   the fixture rather than about the handler. Finding this test's own
	   row by id is the assertion that actually holds.
	*/
	var mine map[string]json.RawMessage
	for _, share := range listed.Shares {
		var id string
		if raw, ok := share["id"]; ok && json.Unmarshal(raw, &id) == nil && id == created.ID {
			mine = share
			break
		}
	}
	if mine == nil {
		t.Fatalf("the created share is not in the listing at all, so the assertions "+
			"below would pass vacuously.\nBody: %s", body)
	}

	// The property itself: no token field on ANY listed share, with any
	// value — the harness's seeded one included.
	for i, share := range listed.Shares {
		if raw, present := share["token"]; present {
			t.Errorf("share %d in the listing carries a token field: %s. It exists "+
				"once, in the create response, and every extra copy is another "+
				"chance for it to be cached or logged", i, raw)
		}
		if raw, present := share["token_hash"]; present {
			t.Errorf("share %d in the listing carries token_hash: %s", i, raw)
		}
	}

	// The plaintext specifically, in case a future field carries it under
	// some other name.
	if strings.Contains(body, created.Token) {
		t.Errorf("the listing contains the plaintext token.\nBody: %s", body)
	}

	// And nothing shaped like the stored hash: 64 hex characters.
	if hex := regexp.MustCompile(`[0-9a-f]{64}`).FindString(body); hex != "" {
		t.Errorf("the listing contains a 64-character hex string (%q), which is what "+
			"the stored token hash looks like.\nBody: %s", hex, body)
	}

}

// ─── caching ─────────────────────────────────────────────────────────

/*
Neither the create response nor the shared view may be cached.

Create carries the only copy of a working credential. The shared view
carries somebody's chart at a public URL — a CDN or corporate proxy
holding it would keep serving that chart after the owner revoked the
link, from infrastructure neither they nor we control.
*/
func TestShareResponsesAreNotCacheable(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	create := h.do(t, http.MethodPost,
		"/api/v1/charts/"+h.profile.String()+"/shares", h.token(t, h.alice), "")
	if create.Code != http.StatusCreated {
		t.Fatalf("create returned %d", create.Code)
	}
	if cc := create.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("the create response's Cache-Control is %q, want no-store — it holds "+
			"the only copy of a working share token", cc)
	}

	var share shareResponse
	_ = json.Unmarshal(create.Body.Bytes(), &share)

	view := h.do(t, http.MethodGet, "/api/v1/shared/"+share.Token, "", "")
	if cc := view.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("the shared view's Cache-Control is %q, want no-store", cc)
	}
	if robots := view.Header().Get("X-Robots-Tag"); !strings.Contains(robots, "noindex") {
		t.Errorf("the shared view's X-Robots-Tag is %q, want noindex — a share link "+
			"WILL end up somewhere a crawler can see it", robots)
	}
}

// ─── expiry ──────────────────────────────────────────────────────────

// A ttl outside the permitted range is refused rather than clamped.
//
// Clamping would mean a client asking for a ten-year link gets a
// ninety-day one and believes it got what it asked for.
func TestAnOutOfRangeExpiryIsRefused(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	for name, days := range map[string]int{
		"a decade": 3650,
		"negative": -1,
	} {
		t.Run(name, func(t *testing.T) {
			rec := h.do(t, http.MethodPost,
				"/api/v1/charts/"+h.profile.String()+"/shares",
				h.token(t, h.alice), fmt.Sprintf(`{"expires_in_days":%d}`, days))
			if rec.Code == http.StatusCreated {
				t.Fatalf("a %d-day link was created; out-of-range expiry must be "+
					"refused, not quietly clamped to something else", days)
			}
		})
	}
}

// The default is applied when the client asks for nothing.
func TestTheDefaultExpiryIsApplied(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	share := h.createShare(t, h.alice, "")

	want := time.Now().Add(shares.DefaultTTL)
	if diff := share.ExpiresAt.Sub(want); diff > time.Hour || diff < -time.Hour {
		t.Fatalf("expires_at is %s, want about %s (%s away)",
			share.ExpiresAt, want, diff)
	}
}

// ─── view counting ───────────────────────────────────────────────────

// Opening a link is counted, so an owner can see a link is in use and
// decide to revoke it. The count is all there is — never who looked.
func TestOpeningALinkIsCountedButTheViewerIsNot(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	share := h.createShare(t, h.alice, "")

	for range 3 {
		if rec := h.do(t, http.MethodGet, "/api/v1/shared/"+share.Token, "", ""); rec.Code != http.StatusOK {
			t.Fatalf("view returned %d", rec.Code)
		}
	}

	rec := h.do(t, http.MethodGet,
		"/api/v1/charts/"+h.profile.String()+"/shares", h.token(t, h.alice), "")
	var listed struct {
		Shares []shareResponse `json:"shares"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode listing: %v", err)
	}
	// By id, not by position: the harness seeds a share of its own, so
	// "the first one" is a fact about the fixture rather than about the
	// link this test opened.
	var opened *shareResponse
	for i := range listed.Shares {
		if listed.Shares[i].ID == share.ID {
			opened = &listed.Shares[i]
			break
		}
	}
	if opened == nil {
		t.Fatalf("the share this test opened is not in the listing")
	}
	if opened.ViewCount != 3 {
		t.Fatalf("view_count is %d after three opens, want 3", opened.ViewCount)
	}

	// And the counter is per link: the harness's own share, never
	// opened, must still read zero.
	for i := range listed.Shares {
		if listed.Shares[i].ID != share.ID && listed.Shares[i].ViewCount != 0 {
			t.Errorf("an unopened share reports %d views; the counter is not scoped "+
				"to the link that was opened", listed.Shares[i].ViewCount)
		}
	}

	// There is no column recording WHO looked, and nothing in the
	// response resembling one. A record of one person's interest in
	// another is not something this product has any business keeping.
	var columns []string
	rows, err := h.pool.Query(context.Background(),
		`SELECT column_name FROM information_schema.columns WHERE table_name = 'chart_shares'`)
	if err != nil {
		t.Fatalf("read columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		columns = append(columns, name)
	}
	for _, column := range columns {
		for _, forbidden := range []string{"viewer", "ip", "user_agent", "referer"} {
			if strings.Contains(column, forbidden) {
				t.Errorf("chart_shares has a %q column. The count is for the owner; "+
					"a log of viewers is a record of one person's interest in "+
					"another, which nobody consented to", column)
			}
		}
	}
}
