//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	"github.com/Vatsalya001/ai-astrology/services/api/internal/config"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/places"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis/ratelimit"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/testsupport"
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

	astro, err := clients.NewAstro("http://127.0.0.1:1", "token", time.Second)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}
	ai, err := clients.NewAI("http://127.0.0.1:1", "token", time.Second)
	if err != nil {
		t.Fatalf("NewAI: %v", err)
	}

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
		Places:       places.NewHandler(placeService, AuthErrorWriter),
		ProfileOwner: profileService,
	}

	h := &routerHarness{
		handler: NewRouter(deps),
		routes:  newChiRouter(deps),
		pool:    pool,
		issuer:  issuer,
	}
	h.alice = seedRouterUser(ctx, t, pool, "alice")
	h.bob = seedRouterUser(ctx, t, pool, "bob")
	h.placeID = seedPlace(ctx, t, pool)
	h.profile = seedProfile(ctx, t, profileService, h.alice, h.placeID, placeService)

	return h, func() {
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
			strings.HasPrefix(route.pattern, "/api/v1/places") {
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
