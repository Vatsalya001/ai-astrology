//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/birthprofiles"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/charts"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/config"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/places"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis/ratelimit"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/testsupport"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/shares"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/transits"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/users"
)

// The real router, the real handlers, real Postgres, real Redis.
//
// A stub router would prove that a middleware I mounted in the test
// blocks requests. What has to be proved is that the middleware is
// mounted on the routes this service actually serves — which is a
// property of router.go, not of the middleware.

type routerHarness struct {
	handler http.Handler
	// routes is the same route table the handler serves, unwrapped so it
	// can be walked. Built from one Deps value, so the two cannot drift.
	routes chi.Router
	pool   *pgxpool.Pool
	issuer *auth.Issuer

	alice   uuid.UUID
	bob     uuid.UUID
	profile uuid.UUID
	placeID int32

	/*
	   A share link Alice owns, so the route walk can address
	   /shares/{shareId} with an id that exists.

	   Without it the walk substitutes nothing for {shareId}, the handler
	   fails to parse the literal "{shareId}" and answers 404 — and
	   TestEveryProfileScopedRouteRefusesAStranger fails on its OWNER
	   assertion, reporting the route as broken when it is the URL that
	   was malformed. Which is the assertion doing its job: a stranger
	   getting 404 from a route the owner cannot use either proves
	   nothing at all.
	*/
	shareID uuid.UUID

	// printTokens is the SAME store the mounted handler uses, so a test
	// can mint a token the way the PDF worker does. Built from one value
	// and handed to both, so "the token the test made" and "the token the
	// route accepts" cannot be two different things.
	printTokens *charts.PrintTokens
}

func newRouterHarness(t *testing.T) (*routerHarness, func()) {
	t.Helper()
	ctx := context.Background()

	pool, stopDB := startRouterPostgres(ctx, t)
	redisClient, stopRedis := startRedis(ctx, t)

	issuer, err := auth.NewIssuer(
		"example-not-a-real-router-test-key-32b", time.Minute, time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	queries := dbgen.New(pool)
	profileService := birthprofiles.NewService(queries)
	placeService := places.NewService(queries)

	// A minimal but REAL auth handler, because mountAstrology is gated on
	// Deps.Auth being non-nil — the same condition production uses.
	authHandler := auth.NewHandler(auth.HandlerConfig{
		Limiter:    ratelimit.New(redisClient),
		IPSalt:     testSalt,
		RefreshTTL: time.Hour,
		WriteError: AuthErrorWriter,
	})

	cfg := &config.Config{WebURL: "http://localhost:3000", IPHashSalt: testSalt}

	astroStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(stubChartJSON))
	}))

	astro, err := clients.NewAstro(astroStub.URL, "token", 2*time.Second)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}
	ai, err := clients.NewAI("http://127.0.0.1:1", "token", time.Second)
	if err != nil {
		t.Fatalf("NewAI: %v", err)
	}

	chartService := charts.NewService(queries, pool, astro, profileService, nil)
	printTokens := charts.NewPrintTokens(charts.NewRedisPrintTokens(redisClient))
	shareService := shares.NewService(queries, profileService, nil)
	// Correcting birth details revokes the old version's links. Wired
	// here as well as in cmd/api, because the test that proves it runs
	// against this router.
	profileService.WithShareRevoker(shareService)

	deps := Deps{
		Config:     cfg,
		DB:         &db.DB{Pool: pool},
		Redis:      &redis.Client{Client: redisClient},
		Astro:      astro,
		AI:         ai,
		Auth:       authHandler,
		AuthIssuer: issuer,
		Users:      users.NewHandler(users.NewService(queries), nil, AuthErrorWriter),
		Limiter:    ratelimit.New(redisClient),

		BirthProfiles: birthprofiles.NewHandler(
			profileService, placeShim{placeService}, AuthErrorWriter),
		Places:   places.NewHandler(placeService, AuthErrorWriter),
		Charts:   charts.NewHandler(chartService, AuthErrorWriter).WithPrintTokens(printTokens),
		Transits: transits.NewHandler(transits.NewReader(queries), chartService, AuthErrorWriter),
		Shares: shares.NewHandler(
			shareService, sharedChartAdapter{chartService}, AuthErrorWriter, nil),
		ProfileOwner: profileService,
	}

	h := &routerHarness{
		handler:     NewRouter(deps),
		routes:      newChiRouter(deps),
		pool:        pool,
		issuer:      issuer,
		printTokens: printTokens,
	}
	h.alice = seedRouterUser(ctx, t, pool, "alice")
	h.bob = seedRouterUser(ctx, t, pool, "bob")
	h.placeID = seedPlace(ctx, t, pool)
	h.profile = seedProfile(ctx, t, profileService, h.alice, h.placeID, placeService)
	seedTransits(ctx, t, pool)
	h.shareID = seedShare(ctx, t, shareService, h.alice, h.profile)

	return h, func() {
		astroStub.Close()
		stopRedis()
		stopDB()
	}
}

// placeShim is the composition-root adapter, duplicated here because
// cmd/api is a main package and cannot be imported.
type placeShim struct{ svc *places.Service }

func (a placeShim) Get(ctx context.Context, id int32) (birthprofiles.Place, error) {
	place, err := a.svc.Get(ctx, id)
	if err != nil {
		return birthprofiles.Place{}, err
	}
	return birthprofiles.Place{
		Name: place.Name, Latitude: place.Latitude,
		Longitude: place.Longitude, Timezone: place.Timezone,
	}, nil
}

func (h *routerHarness) token(t *testing.T, userID uuid.UUID) string {
	t.Helper()
	token, err := h.issuer.IssueAccessToken(userID, uuid.New(), "user")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	return token
}

func (h *routerHarness) do(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.RemoteAddr = "203.0.113.9:40000"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

// ─── the specification's headline assertion ──────────────────────────

// "An integration test asserts that user B gets a 404 (not 403) on user
// A's chart."
//
// Written as a walk over the REAL route table rather than as a list of
// paths, so a route added later under a profile ID is covered the day it
// is added rather than the day somebody remembers to extend this test.
func TestEveryProfileScopedRouteRefusesAStranger(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	bob := h.token(t, h.bob)
	alice := h.token(t, h.alice)

	routes := profileScopedRoutes(t, h.routes, h.placeID)
	if len(routes) == 0 {
		t.Fatal("no profile-scoped routes were discovered; the walk is matching nothing " +
			"and this test is proving nothing")
	}
	t.Logf("checking %d profile-scoped routes", len(routes))

	for _, route := range routes {
		path := strings.NewReplacer(
			"{id}", h.profile.String(),
			"{birthProfileId}", h.profile.String(),
			"{shareId}", h.shareID.String(),
		).Replace(route.pattern)

		t.Run(route.method+" "+route.pattern, func(t *testing.T) {
			// Alice owns it, so she must get something other than 404 —
			// otherwise "everyone gets 404" would pass this test while the
			// product was entirely broken.
			owner := h.do(t, route.method, path, alice, route.body)
			if owner.Code == http.StatusNotFound {
				t.Fatalf("the OWNER got 404 on her own profile; this route is broken, "+
					"and a stranger getting 404 proves nothing (body: %s)",
					strings.TrimSpace(owner.Body.String()))
			}

			stranger := h.do(t, route.method, path, bob, route.body)
			if stranger.Code != http.StatusNotFound {
				t.Fatalf("a stranger got %d on somebody else's birth profile, want 404. "+
					"403 confirms the profile exists and turns this route into an "+
					"enumeration oracle; anything 2xx is a data leak. Body: %s",
					stranger.Code, strings.TrimSpace(stranger.Body.String()))
			}
		})
	}
}

// Ownership is enforced on the row, not merely in the response. A
// stranger's PATCH must not have created a version before the 404 was
// written.
func TestAStrangersWriteChangesNothing(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	before := h.countVersions(t)

	body := fmt.Sprintf(
		`{"birth_date":"1991-02-03","birth_time":"04:05","time_accuracy":"exact","place_id":%d}`,
		h.placeID)
	rec := h.do(t, http.MethodPatch, "/api/v1/birth-profiles/"+h.profile.String(),
		h.token(t, h.bob), body)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("a stranger's PATCH returned %d, want 404", rec.Code)
	}
	if after := h.countVersions(t); after != before {
		t.Fatalf("a refused PATCH still wrote a row: %d versions before, %d after", before, after)
	}

	// And the same for DELETE.
	rec = h.do(t, http.MethodDelete, "/api/v1/birth-profiles/"+h.profile.String(),
		h.token(t, h.bob), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a stranger's DELETE returned %d, want 404", rec.Code)
	}

	var active bool
	if err := h.pool.QueryRow(context.Background(),
		`SELECT is_active FROM birth_profiles WHERE id = $1`, h.profile).Scan(&active); err != nil {
		t.Fatalf("read is_active: %v", err)
	}
	if !active {
		t.Fatal("a stranger's DELETE deactivated the profile despite returning 404")
	}
}

// Every Phase 2 route must require a token. A GET that answered without
// one would expose birth data to the internet, and the place search would
// expose a 200k-row gazetteer.
func TestNoPhase2RouteAnswersWithoutAToken(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	for _, route := range phase2Routes(t, h.routes, h.placeID) {
		path := strings.NewReplacer(
			"{id}", h.profile.String(),
			"{birthProfileId}", h.profile.String(),
			"{shareId}", h.shareID.String(),
		).Replace(route.pattern)

		rec := h.do(t, route.method, path, "", route.body)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s answered %d with no Authorization header, want 401",
				route.method, route.pattern, rec.Code)
		}
	}
}

// ─── the happy path, so the guards above are not vacuous ─────────────

func TestAnOwnerCanCreateReadVersionAndDeleteAProfile(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()
	alice := h.token(t, h.alice)

	created := h.do(t, http.MethodPost, "/api/v1/birth-profiles", alice, fmt.Sprintf(
		`{"label":"self","birth_date":"1990-03-15","birth_time":"06:30",`+
			`"time_accuracy":"exact","place_id":%d}`, h.placeID))
	if created.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", created.Code, created.Body.String())
	}

	var profile struct {
		ID         string  `json:"id"`
		Version    int     `json:"version"`
		BirthPlace string  `json:"birth_place"`
		UTCOffset  int     `json:"utc_offset_min"`
		BirthTime  *string `json:"birth_time"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &profile); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if profile.Version != 1 {
		t.Fatalf("a new profile is version %d, want 1", profile.Version)
	}
	// The server resolved the zone; the client sent only a place ID.
	if profile.UTCOffset != 330 {
		t.Fatalf("utc_offset_min is %d for an Indian place, want 330 — "+
			"the historical zone was not resolved server-side", profile.UTCOffset)
	}

	// PATCH creates a version rather than mutating.
	patched := h.do(t, http.MethodPatch, "/api/v1/birth-profiles/"+profile.ID, alice, fmt.Sprintf(
		`{"label":"self","birth_date":"1990-03-15","birth_time":"07:45",`+
			`"time_accuracy":"exact","place_id":%d}`, h.placeID))
	if patched.Code != http.StatusOK {
		t.Fatalf("patch returned %d: %s", patched.Code, patched.Body.String())
	}

	var updated struct {
		ID        string  `json:"id"`
		Version   int     `json:"version"`
		BirthTime *string `json:"birth_time"`
	}
	if err := json.Unmarshal(patched.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("a corrected profile is version %d, want 2", updated.Version)
	}
	if updated.ID == profile.ID {
		t.Fatal("PATCH returned the same id — it mutated the row instead of versioning it, " +
			"and the chart behind the old reading is now unexplainable")
	}
	if updated.BirthTime == nil || *updated.BirthTime != "07:45" {
		t.Fatalf("the corrected birth time is %v, want 07:45 — the update did nothing",
			updated.BirthTime)
	}

	// The version history is reachable from the NEW id.
	versions := h.do(t, http.MethodGet, "/api/v1/birth-profiles/"+updated.ID+"/versions", alice, "")
	if versions.Code != http.StatusOK {
		t.Fatalf("versions returned %d: %s", versions.Code, versions.Body.String())
	}
	var history struct {
		Versions []struct {
			Version int `json:"version"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(versions.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode versions: %v", err)
	}
	if len(history.Versions) != 2 {
		t.Fatalf("history has %d entries, want 2 — the superseded version is not reachable",
			len(history.Versions))
	}

	// DELETE is soft, and the row survives so old readings stay explicable.
	deleted := h.do(t, http.MethodDelete, "/api/v1/birth-profiles/"+updated.ID, alice, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete returned %d: %s", deleted.Code, deleted.Body.String())
	}

	var stillThere bool
	if err := h.pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM birth_profiles WHERE id = $1)`,
		updated.ID).Scan(&stillThere); err != nil {
		t.Fatalf("check row: %v", err)
	}
	if !stillThere {
		t.Fatal("DELETE removed the row; it must be a soft delete or the charts and " +
			"readings referencing it become orphans")
	}
}

// A birth time is required unless the caller explicitly says it is
// unknown. The escape hatch has to be asked for: "unknown" means no
// ascendant and no houses, which is a materially poorer chart to fall
// into by omitting a field.
func TestAMissingBirthTimeIsRefusedUnlessDeclaredUnknown(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()
	alice := h.token(t, h.alice)

	refused := h.do(t, http.MethodPost, "/api/v1/birth-profiles", alice, fmt.Sprintf(
		`{"birth_date":"1990-03-15","time_accuracy":"exact","place_id":%d}`, h.placeID))
	if refused.Code != http.StatusBadRequest {
		t.Fatalf("a missing birth time with accuracy \"exact\" returned %d, want 400",
			refused.Code)
	}

	accepted := h.do(t, http.MethodPost, "/api/v1/birth-profiles", alice, fmt.Sprintf(
		`{"birth_date":"1990-03-15","time_accuracy":"unknown","place_id":%d}`, h.placeID))
	if accepted.Code != http.StatusCreated {
		t.Fatalf("an explicitly unknown birth time returned %d: %s",
			accepted.Code, accepted.Body.String())
	}
}

// Coordinates come from our own gazetteer, never from the client. A
// browser that sent its own latitude would compute a chart for wherever
// that is, and nothing downstream could tell.
func TestAnUnknownPlaceIsRefused(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	rec := h.do(t, http.MethodPost, "/api/v1/birth-profiles", h.token(t, h.alice),
		`{"birth_date":"1990-03-15","birth_time":"06:30","time_accuracy":"exact","place_id":999999}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown place_id returned %d, want 400", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "999999") {
		t.Fatalf("the error echoes the id back (%s); it confirms which ids are real",
			rec.Body.String())
	}
}

// A request body carrying an unexpected field is rejected rather than
// silently ignored — an allowlist, per the security rules.
func TestAnUnknownFieldIsRejected(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	rec := h.do(t, http.MethodPost, "/api/v1/birth-profiles", h.token(t, h.alice), fmt.Sprintf(
		`{"birth_date":"1990-03-15","birth_time":"06:30","time_accuracy":"exact",`+
			`"place_id":%d,"latitude":0.0}`, h.placeID))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a body carrying its own latitude returned %d, want 400 — "+
			"a client must not be able to smuggle coordinates past the gazetteer", rec.Code)
	}
}

// ─── the place search ────────────────────────────────────────────────

func TestPlaceSearchRanksByPopulationAndToleratesAShortQuery(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()
	alice := h.token(t, h.alice)

	rec := h.do(t, http.MethodGet, "/api/v1/places/search?q=jaip", alice, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("search returned %d: %s", rec.Code, rec.Body.String())
	}
	var found struct {
		Places []struct {
			Name       string `json:"name"`
			Population int32  `json:"population"`
		} `json:"places"`
		Query string `json:"query"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &found); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(found.Places) < 2 {
		t.Fatalf("expected both Jaipurs, got %d", len(found.Places))
	}
	if found.Places[0].Population < found.Places[1].Population {
		t.Fatalf("results are not ranked by population: %v then %v — "+
			"the village would outrank the city about half the time",
			found.Places[0], found.Places[1])
	}
	if found.Query != "jaip" {
		t.Fatalf("query echoed as %q; a client cannot discard an overtaken response without it",
			found.Query)
	}

	// One character is mid-word, not an error.
	short := h.do(t, http.MethodGet, "/api/v1/places/search?q=j", alice, "")
	if short.Code != http.StatusOK {
		t.Fatalf("a one-character query returned %d, want 200 with an empty list — "+
			"a red error under a box the user is still typing into is a bug", short.Code)
	}
}

// ─── route discovery ─────────────────────────────────────────────────

type discovered struct {
	method  string
	pattern string
	body    string
}

// bodyFor supplies a minimally valid payload for the methods that need
// one, so a 400 cannot be mistaken for the 404 under test.
func bodyFor(method, pattern string, placeID int32) string {
	if method != http.MethodPost && method != http.MethodPatch {
		return ""
	}
	return fmt.Sprintf(
		`{"birth_date":"1990-03-15","birth_time":"06:30","time_accuracy":"exact","place_id":%d}`,
		placeID)
}

// profileScopedRoutes walks the real router for every route addressing a
// specific birth profile.
func profileScopedRoutes(t *testing.T, router chi.Router, placeID int32) []discovered {
	t.Helper()

	var out []discovered
	for _, route := range walk(t, router) {
		if !strings.Contains(route.pattern, "{id}") &&
			!strings.Contains(route.pattern, "{birthProfileId}") {
			continue
		}
		// Session revocation also takes an {id}, but it is a Phase 1 route
		// scoped by the session table rather than by a birth profile.
		if strings.Contains(route.pattern, "/sessions/") {
			continue
		}
		route.body = bodyFor(route.method, route.pattern, placeID)
		out = append(out, route)
	}
	return out
}

// phase2Routes is everything this phase mounted, scoped or not.
func phase2Routes(t *testing.T, router chi.Router, placeID int32) []discovered {
	t.Helper()

	var out []discovered
	for _, route := range walk(t, router) {
		if strings.HasPrefix(route.pattern, "/api/v1/birth-profiles") ||
			strings.HasPrefix(route.pattern, "/api/v1/places") ||
			strings.HasPrefix(route.pattern, "/api/v1/charts") ||
			strings.HasPrefix(route.pattern, "/api/v1/astrology") {
			route.body = bodyFor(route.method, route.pattern, placeID)
			out = append(out, route)
		}
	}
	return out
}

// walk enumerates the real route table with chi.Walk.
//
// The patterns are never written out by hand here. That is the whole
// point: a hand-written list goes stale the first time somebody adds an
// endpoint, and the test then passes while the new route is unguarded.
func walk(t *testing.T, router chi.Router) []discovered {
	t.Helper()

	var out []discovered
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// chi renders a subtree root as "/api/v1/birth-profiles/", which
		// is the same route as the collection.
		route = strings.TrimSuffix(route, "/*")
		if route != "/" {
			route = strings.TrimSuffix(route, "/")
		}
		out = append(out, discovered{method: method, pattern: route})
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].pattern != out[j].pattern {
			return out[i].pattern < out[j].pattern
		}
		return out[i].method < out[j].method
	})
	return out
}

func seedRouterUser(ctx context.Context, t *testing.T, pool *pgxpool.Pool, label string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(ctx,
		`INSERT INTO users (email, email_verified) VALUES ($1, TRUE) RETURNING id`,
		label+"-"+uuid.NewString()+"@example.com").Scan(&id)
	if err != nil {
		t.Fatalf("seed user %s: %v", label, err)
	}
	return id
}

// seedPlace inserts two Jaipurs, so population ranking has something to
// rank. A single row would pass an ordering assertion trivially.
func seedPlace(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int32 {
	t.Helper()

	rows := []struct {
		id         int32
		name       string
		admin      string
		population int32
	}{
		{1269515, "Jaipur", "Rajasthan", 2711758},
		{1269516, "Jaipur", "Odisha", 612},
	}
	for _, row := range rows {
		if _, err := pool.Exec(ctx,
			`INSERT INTO places (id, name, ascii_name, admin1, country_code,
			                     latitude, longitude, timezone, population)
			 VALUES ($1, $2, $2, $3, 'IN', 26.9124, 75.7873, 'Asia/Kolkata', $4)`,
			row.id, row.name, row.admin, row.population); err != nil {
			t.Fatalf("seed place %s: %v", row.admin, err)
		}
	}
	return rows[0].id
}

func seedProfile(
	ctx context.Context, t *testing.T,
	svc *birthprofiles.Service, userID uuid.UUID, placeID int32, placeSvc *places.Service,
) uuid.UUID {
	t.Helper()

	place, err := placeSvc.Get(ctx, placeID)
	if err != nil {
		t.Fatalf("resolve seed place: %v", err)
	}

	profile, err := svc.Create(ctx, birthprofiles.CreateInput{
		UserID:       userID,
		Label:        "self",
		BirthDate:    time.Date(1990, 3, 15, 0, 0, 0, 0, time.UTC),
		BirthTime:    6*time.Hour + 30*time.Minute,
		HasBirthTime: true,
		TimeAccuracy: birthprofiles.AccuracyExact,
		PlaceName:    place.Name,
		Latitude:     place.Latitude,
		Longitude:    place.Longitude,
		Timezone:     place.Timezone,
	})
	if err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	return profile.ID
}

func (h *routerHarness) countVersions(t *testing.T) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM birth_profiles WHERE user_id = $1`, h.alice).Scan(&n); err != nil {
		t.Fatalf("count profiles: %v", err)
	}
	return n
}

func startRouterPostgres(ctx context.Context, t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()

	initSQL, err := filepath.Abs(filepath.Join(
		"..", "..", "..", "..", "infrastructure", "docker", "init", "01-init.sql"))
	if err != nil {
		t.Fatalf("resolve init script: %v", err)
	}
	if _, statErr := os.Stat(initSQL); statErr != nil {
		t.Fatalf("init script missing at %s: %v", initSQL, statErr)
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "pgvector/pgvector:pg16",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_USER": "astro", "POSTGRES_PASSWORD": "astro", "POSTGRES_DB": "astro_dev",
			},
			Files: []testcontainers.ContainerFile{{
				HostFilePath:      initSQL,
				ContainerFilePath: "/docker-entrypoint-initdb.d/01-init.sql",
				FileMode:          0o644,
			}},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		testsupport.ContainerUnavailable(t, "Postgres", err)
	}

	host, _ := container.Host(ctx)
	port, _ := container.MappedPort(ctx, "5432/tcp")

	pool, err := pgxpool.New(ctx,
		"postgres://astro:astro@"+host+":"+port.Port()+"/astro_dev?sslmode=disable")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	dir, _ := filepath.Abs(filepath.Join("..", "..", "db", "migrations"))
	files, globErr := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if globErr != nil || len(files) == 0 {
		t.Fatalf("no migrations found in %s", dir)
	}
	sort.Strings(files)
	for _, file := range files {
		body, _ := os.ReadFile(file)
		if _, execErr := pool.Exec(ctx, string(body)); execErr != nil {
			t.Fatalf("apply %s: %v", filepath.Base(file), execErr)
		}
	}

	return pool, func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	}
}

// stubChartJSON is what the fake astro-service returns.
//
// The dasha tree is three real levels with a second Mahadasha after the
// first, so ordering and "which is running now" have something to be
// wrong about. A single period would satisfy every assertion trivially.
//
// The Moon is in Capricorn, which is also where seedTransits puts Saturn
// — so the natal transit endpoint has an active Sade Sati at peak, and a
// rotation that silently returned astro's own house number would produce
// a different answer.
const stubChartJSON = `{
  "meta": {"schema_version":1,"calculation_system":"vedic","ayanamsa":"lahiri",
           "ayanamsa_value":24.21,"house_system":"whole_sign",
           "engine_version":"skyfield-1.55+de421+schema1",
           "computed_at":"2026-01-01T00:00:00Z","time_accuracy":"exact"},
  "ascendant": null, "houses": null,
  "planets": [], "yogas": [],
  "navamsa": {
    "ascendant": null, "houses": null,
    "planets": [{"planet":"Sun","longitude":10.0,"sign":"Aries","sign_index":0,
                 "degree":10.0,"house":1,"nakshatra":"Ashwini","nakshatra_index":0,
                 "pada":3,"is_retrograde":false,"is_combust":false,
                 "dignity":"exalted","speed":0.98}]
  },
  "dasamsa": {
    "ascendant": null, "houses": null,
    "planets": [{"planet":"Sun","longitude":190.0,"sign":"Libra","sign_index":6,
                 "degree":10.0,"house":1,"nakshatra":"Swati","nakshatra_index":14,
                 "pada":1,"is_retrograde":false,"is_combust":false,
                 "dignity":"neutral","speed":0.98}]
  },
  "summary": {"sun_sign":"Pisces","moon_sign":"Capricorn","ascendant_sign":"Aries",
              "moon_nakshatra":"Shravana","moon_nakshatra_pada":2},
  "dashas": [
    {"planet":"Sun","start":"2020-01-01T00:00:00Z","end":"2026-12-31T00:00:00Z","level":1,
     "children":[
       {"planet":"Sun","start":"2020-01-01T00:00:00Z","end":"2021-01-01T00:00:00Z","level":2},
       {"planet":"Moon","start":"2021-01-01T00:00:00Z","end":"2026-12-31T00:00:00Z","level":2,
        "children":[
          {"planet":"Rahu","start":"2021-01-01T00:00:00Z","end":"2026-01-01T00:00:00Z","level":3},
          {"planet":"Mars","start":"2026-01-01T00:00:00Z","end":"2026-12-31T00:00:00Z","level":3}
        ]}
     ]},
    {"planet":"Moon","start":"2026-12-31T00:00:00Z","end":"2036-12-31T00:00:00Z","level":1,
     "children":[
       {"planet":"Moon","start":"2026-12-31T00:00:00Z","end":"2027-11-01T00:00:00Z","level":2}
     ]}
  ]
}`

// dashaProbe is an instant inside the first Mahadasha, the Moon
// Antardasha and the Mars Pratyantardasha — one value that lands in all
// three, which is the point of the tree above.
const dashaProbe = "2026-06-01T00:00:00Z"

// seedTransits fills the global table directly, standing in for the
// six-hourly worker. Saturn sits in Capricorn, over the stub chart's
// natal Moon.
func seedTransits(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	rows := []struct {
		planet    string
		sign      string
		signIndex int
	}{
		{"Sun", "Taurus", 1},
		{"Moon", "Leo", 4},
		{"Saturn", "Capricorn", 9},
	}
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	for _, row := range rows {
		if _, err := pool.Exec(ctx,
			`INSERT INTO transits (planet, sign, degree, is_retrograde, timestamp,
			                       calculation_system, ayanamsa, metadata)
			 VALUES ($1, $2, 14.5, FALSE, $3, 'vedic', 'lahiri',
			         jsonb_build_object('longitude', $4::double precision,
			                            'sign_index', $5::int))`,
			row.planet, row.sign, at, float64(row.signIndex)*30+14.5, row.signIndex,
		); err != nil {
			t.Fatalf("seed transit %s: %v", row.planet, err)
		}
	}
}

// ─── charts, dashas and transits ─────────────────────────────────────

// The dasha tree is stored as ROWS, not just as JSON inside the chart,
// and this is the query that justifies it: one instant in, three levels
// out, one round trip.
func TestCurrentDashasReturnAllThreeLevels(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()
	alice := h.token(t, h.alice)

	rec := h.do(t, http.MethodGet,
		"/api/v1/charts/"+h.profile.String()+"/dashas/current?at="+dashaProbe, alice, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("current dashas returned %d: %s", rec.Code, rec.Body.String())
	}

	var current struct {
		Maha *struct {
			Planet    string  `json:"planet"`
			Level     int     `json:"level"`
			ElapsedPc float64 `json:"elapsed_percent"`
		} `json:"mahadasha"`
		Antar *struct {
			Planet string `json:"planet"`
			Level  int    `json:"level"`
		} `json:"antardasha"`
		Pratyantar *struct {
			Planet string `json:"planet"`
			Level  int    `json:"level"`
		} `json:"pratyantardasha"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &current); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if current.Maha == nil || current.Antar == nil || current.Pratyantar == nil {
		t.Fatalf("expected all three levels at %s, got maha=%v antar=%v pratyantar=%v — "+
			"a missing level means the tree was written without its children, and the "+
			"screen renders that as fact",
			dashaProbe, current.Maha, current.Antar, current.Pratyantar)
	}

	// The instant sits inside exactly one period per level, and these are
	// the three. Getting the parent-child links wrong would return a
	// plausible but different trio.
	if current.Maha.Planet != "Sun" {
		t.Fatalf("Mahadasha at %s is %s, want Sun", dashaProbe, current.Maha.Planet)
	}
	if current.Antar.Planet != "Moon" {
		t.Fatalf("Antardasha is %s, want Moon", current.Antar.Planet)
	}
	if current.Pratyantar.Planet != "Mars" {
		t.Fatalf("Pratyantardasha is %s, want Mars", current.Pratyantar.Planet)
	}

	// Sun runs 2020-01-01 to 2026-12-31 and the probe is 2026-06-01, so
	// it is most of the way through. An elapsed of 0 or 100 would mean the
	// progress bar is computed from the wrong pair of timestamps.
	if current.Maha.ElapsedPc <= 80 || current.Maha.ElapsedPc >= 100 {
		t.Fatalf("Mahadasha is %.1f%% elapsed at %s; the probe is six years into a "+
			"seven-year period", current.Maha.ElapsedPc, dashaProbe)
	}
}

// Levels come back ordered and complete, which is what ListDashasByLevel
// is for. Level 3 having more rows than level 1 is the shape of a tree.
func TestDashaLevelsAreOrderedAndNested(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()
	alice := h.token(t, h.alice)

	counts := map[int]int{}
	for level := 1; level <= 3; level++ {
		rec := h.do(t, http.MethodGet, fmt.Sprintf(
			"/api/v1/charts/%s/dashas?level=%d", h.profile.String(), level), alice, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("level %d returned %d: %s", level, rec.Code, rec.Body.String())
		}

		var body struct {
			Level   int `json:"level"`
			Periods []struct {
				Planet   string    `json:"planet"`
				Start    time.Time `json:"start"`
				End      time.Time `json:"end"`
				Level    int       `json:"level"`
				ParentID *string   `json:"parent_id"`
			} `json:"periods"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode level %d: %v", level, err)
		}
		counts[level] = len(body.Periods)

		for i, period := range body.Periods {
			if period.Level != level {
				t.Fatalf("level %d query returned a level-%d period", level, period.Level)
			}
			// The parent_id / level pair is a CHECK constraint in the
			// schema; this asserts the API surfaces it consistently.
			if level == 1 && period.ParentID != nil {
				t.Fatalf("a Mahadasha has parent %s; level 1 is the root", *period.ParentID)
			}
			if level > 1 && period.ParentID == nil {
				t.Fatalf("a level-%d period has no parent — it is queryable as though "+
					"it were a Mahadasha", level)
			}
			if i > 0 && period.Start.Before(body.Periods[i-1].Start) {
				t.Fatalf("level %d is not ordered by start date", level)
			}
		}
	}

	if counts[1] != 2 {
		t.Fatalf("level 1 has %d periods, want the 2 in the fixture", counts[1])
	}
	if counts[3] == 0 {
		t.Fatal("level 3 is empty; the recursion never reached the third level, " +
			"and the deepest dasha the product shows does not exist")
	}
}

func TestAnOutOfRangeDashaLevelIsRefused(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()
	alice := h.token(t, h.alice)

	for _, level := range []string{"0", "4", "-1", "three"} {
		rec := h.do(t, http.MethodGet,
			"/api/v1/charts/"+h.profile.String()+"/dashas?level="+level, alice, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("level=%s returned %d, want 400", level, rec.Code)
		}
	}
}

// The chart type reaches both a database UNIQUE key and astro-service, so
// it is an allowlist rather than a pass-through: an unrecognised value
// would create a permanent cache entry for a chart nothing can render.
func TestTheChartTypeIsAnAllowlist(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()
	alice := h.token(t, h.alice)

	// D10 is a real divisional chart as of Phase 3 — the gate requires
	// "D1, D9 and D10 all viewable".
	for _, chartType := range []string{"D1", "D9", "D10", "d1", "d10"} {
		rec := h.do(t, http.MethodGet,
			"/api/v1/charts/"+h.profile.String()+"?type="+url.QueryEscape(chartType), alice, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("type=%s returned %d: %s", chartType, rec.Code, rec.Body.String())
		}
	}
	// D60 is a real divisional chart this project does not compute, so it
	// must be refused rather than cached as an empty D1. The last one is
	// not expected to reach SQL — sqlc parameterises everything — but a
	// value that would be catastrophic if it did belongs in the allowlist
	// test rather than in a comment.
	for _, chartType := range []string{"D60", "D2", "'; DROP TABLE charts; --"} {
		rec := h.do(t, http.MethodGet,
			"/api/v1/charts/"+h.profile.String()+"?type="+url.QueryEscape(chartType), alice, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("type=%q returned %d, want 400", chartType, rec.Code)
		}
	}

	// And the tables are still there.
	var tables int
	if err := h.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables
		 WHERE table_schema = 'public' AND table_name IN ('charts', 'dashas')`).Scan(&tables); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if tables != 2 {
		t.Fatalf("expected charts and dashas to still exist, found %d", tables)
	}
}

// The natal transit endpoint rotates the SHARED table onto this user's
// Moon. Saturn is in Capricorn and so is the stub chart's natal Moon, so
// Sade Sati is at peak — and the house must be 1, not the 7 that a
// pass-through of astro's own number would give.
func TestNatalTransitsRotateOntoTheUsersMoon(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()
	alice := h.token(t, h.alice)

	rec := h.do(t, http.MethodGet,
		"/api/v1/astrology/transits/"+h.profile.String()+"?at="+dashaProbe, alice, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("natal transits returned %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		NatalMoonSign string `json:"natal_moon_sign"`
		Transits      []struct {
			Planet        string `json:"planet"`
			Sign          string `json:"sign"`
			HouseFromMoon int    `json:"house_from_moon"`
		} `json:"transits"`
		SadeSati struct {
			IsActive bool    `json:"is_active"`
			Phase    *string `json:"phase"`
		} `json:"sade_sati"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body.NatalMoonSign != "Capricorn" {
		t.Fatalf("natal moon sign is %q, want Capricorn from the stored chart summary",
			body.NatalMoonSign)
	}

	var saturn, sun int
	for _, transit := range body.Transits {
		switch transit.Planet {
		case "Saturn":
			saturn = transit.HouseFromMoon
		case "Sun":
			sun = transit.HouseFromMoon
		}
	}
	if saturn != 1 {
		t.Fatalf("Saturn in Capricorn with a Capricorn Moon is house %d, want 1", saturn)
	}
	// Taurus is four signs on from Capricorn counting inclusively: 5.
	if sun != 5 {
		t.Fatalf("the Sun in Taurus with a Capricorn Moon is house %d, want 5 — "+
			"the rotation is applied per planet, not once for the whole set", sun)
	}

	if !body.SadeSati.IsActive || body.SadeSati.Phase == nil || *body.SadeSati.Phase != "peak" {
		t.Fatalf("Saturn over the natal Moon: active=%v phase=%v, want an active peak",
			body.SadeSati.IsActive, body.SadeSati.Phase)
	}
}

// The global endpoint must not leak a natal frame. A house number there
// would be somebody's — whoever's Moon happened to be used.
func TestGlobalTransitsCarryNoHouse(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()

	rec := h.do(t, http.MethodGet, "/api/v1/astrology/transits?at="+dashaProbe,
		h.token(t, h.alice), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("global transits returned %d: %s", rec.Code, rec.Body.String())
	}

	var raw struct {
		Transits []map[string]any `json:"transits"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(raw.Transits) == 0 {
		t.Fatal("the global endpoint returned nothing")
	}

	for _, transit := range raw.Transits {
		for _, field := range []string{"house_from_moon", "house_from_ascendant"} {
			if _, present := transit[field]; present {
				t.Fatalf("the global response carries %q for %v — a house is relative to "+
					"one person's natal chart and has no meaning in a shared response",
					field, transit["planet"])
			}
		}
	}
}

// A chart whose profile has no birth time has no dasha tree, because the
// Moon cannot be pinned to a nakshatra pada without one. That is a
// different thing from "we failed", and the user is told which.
func TestDashasForAnUnknownBirthTimeExplainWhy(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()
	alice := h.token(t, h.alice)

	created := h.do(t, http.MethodPost, "/api/v1/birth-profiles", alice, fmt.Sprintf(
		`{"label":"no-time","birth_date":"1990-03-15","time_accuracy":"unknown","place_id":%d}`,
		h.placeID))
	if created.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", created.Code, created.Body.String())
	}
	var profile struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &profile); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Compute the chart first, so there is a chart row to hang the
	// absence off. Otherwise this would test "no chart", which 404s for a
	// completely different reason.
	if chart := h.do(t, http.MethodGet, "/api/v1/charts/"+profile.ID, alice, ""); chart.Code != http.StatusOK {
		t.Fatalf("computing the chart returned %d: %s", chart.Code, chart.Body.String())
	}

	// The stub returns a dasha tree regardless of time accuracy, so the
	// real no-time case is reproduced by removing the rows a real
	// astro-service would never have sent.
	if _, err := h.pool.Exec(context.Background(),
		`DELETE FROM dashas d USING charts c, birth_profiles p
		 WHERE d.chart_id = c.id AND c.birth_profile_id = p.id AND p.id = $1`,
		profile.ID); err != nil {
		t.Fatalf("clear dashas: %v", err)
	}

	rec := h.do(t, http.MethodGet, "/api/v1/charts/"+profile.ID+"/dashas", alice, "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("dashas with no tree returned %d, want 422 — a 404 would say the chart "+
			"does not exist, and an empty list would say there are no dashas, which is "+
			"a claim about astrology rather than about this profile (body: %s)",
			rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	if !strings.Contains(rec.Body.String(), "birth time") {
		t.Fatalf("the message does not tell the user what to do about it: %s",
			strings.TrimSpace(rec.Body.String()))
	}
}

// Recompute is the one route that calls astro-service unconditionally,
// so the limit is the feature, not an afterthought: without it, one
// authenticated user is arbitrary load on the compute service and the
// rest of the product degrades with them.
func TestRecomputeIsRateLimitedPerUser(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()
	alice := h.token(t, h.alice)
	path := "/api/v1/charts/" + h.profile.String() + "/recompute"

	for attempt := 1; attempt <= charts.RecomputeLimit.Max; attempt++ {
		rec := h.do(t, http.MethodPost, path, alice, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("recompute %d of %d returned %d: %s",
				attempt, charts.RecomputeLimit.Max, rec.Code, rec.Body.String())
		}
	}

	rec := h.do(t, http.MethodPost, path, alice, "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("recompute %d returned %d, want 429 — the limit is what stops one "+
			"account becoming arbitrary load on astro-service",
			charts.RecomputeLimit.Max+1, rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("a 429 with no Retry-After leaves the client guessing, and guessing " +
			"means retrying immediately")
	}

	// Per user, not per IP: everyone behind one mobile carrier NAT shares
	// an address, and limiting on it would punish all of them for one.
	// Bob owns no profile, so he gets 404 — but from the OWNERSHIP check,
	// which means he was not stopped by Alice's exhausted limit.
	bob := h.do(t, http.MethodPost, path, h.token(t, h.bob), "")
	if bob.Code == http.StatusTooManyRequests {
		t.Fatal("a second user was rate-limited by the first user's recomputes; " +
			"the limiter is keyed on something they share")
	}
}

// Recompute replaces the stored chart rather than deleting and refilling,
// so a failure leaves the previous chart intact.
func TestRecomputeReplacesTheChartAndItsDashaTree(t *testing.T) {
	h, cleanup := newRouterHarness(t)
	defer cleanup()
	alice := h.token(t, h.alice)

	// Compute once, then count.
	if rec := h.do(t, http.MethodGet, "/api/v1/charts/"+h.profile.String(), alice, ""); rec.Code != http.StatusOK {
		t.Fatalf("initial chart returned %d: %s", rec.Code, rec.Body.String())
	}
	before := h.countDashas(t)
	if before == 0 {
		t.Fatal("no dasha rows after computing a chart")
	}

	if rec := h.do(t, http.MethodPost,
		"/api/v1/charts/"+h.profile.String()+"/recompute", alice, ""); rec.Code != http.StatusOK {
		t.Fatalf("recompute returned %d: %s", rec.Code, rec.Body.String())
	}

	if after := h.countDashas(t); after != before {
		t.Fatalf("the dasha tree has %d rows after a recompute and %d before — "+
			"the old tree was not replaced, it was added to, and FindDashaAt now "+
			"returns two overlapping generations", after, before)
	}
}

func (h *routerHarness) countDashas(t *testing.T) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM dashas d
		 JOIN charts c ON c.id = d.chart_id
		 WHERE c.birth_profile_id = $1`, h.profile).Scan(&n); err != nil {
		t.Fatalf("count dashas: %v", err)
	}
	return n
}

// sharedChartAdapter matches the composition root's adapter in
// cmd/api/main.go: shares.ChartReader returns `any`, and the charts
// service returns a concrete SharedView.
//
// Duplicated here rather than exported from somewhere shared, because
// cmd/api is a main package and cannot be imported — the same reason
// placeShim above is duplicated.
type sharedChartAdapter struct{ svc *charts.Service }

func (a sharedChartAdapter) SharedChart(
	ctx context.Context, userID, profileID uuid.UUID,
) (any, error) {
	return a.svc.SharedChart(ctx, userID, profileID)
}

// seedShare creates one share link, so a route addressing {shareId} has
// something real to address.
func seedShare(
	ctx context.Context, t *testing.T,
	svc *shares.Service, userID, profileID uuid.UUID,
) uuid.UUID {
	t.Helper()

	share, err := svc.Create(ctx, userID, profileID, 0)
	if err != nil {
		t.Fatalf("seed share: %v", err)
	}
	return share.ID
}
