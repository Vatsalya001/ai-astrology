//go:build integration

package charts_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/birthprofiles"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/charts"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/testsupport"
)

// Birth profiles and charts against real Postgres and a stubbed astro.
//
// Postgres is real because the properties under test live in the schema:
// the UNIQUE constraint that makes a chart identified by its inputs, and
// the cascade that makes versioning work. astro is stubbed because what
// matters here is how this service behaves when astro is SLOW, DOWN or
// REJECTING — states a real service will not enter on request.

// The rasi and the navamsa are DELIBERATELY different here — Scorpio
// rising with the Sun in Leo against Capricorn rising with the Sun in
// Aries. That is what a real D9 looks like relative to its D1 (checked
// against astro-service for 1994-08-17), and it is the only way a test
// can tell a stored D9 from a mislabelled copy of the D1.
const chartJSON = `{
	"meta": {"schema_version":1,"calculation_system":"vedic","ayanamsa":"lahiri",
	         "ayanamsa_value":23.8,"house_system":"whole_sign",
	         "engine_version":"skyfield-1.55+de421+schema1",
	         "computed_at":"2026-01-01T00:00:00Z","time_accuracy":"exact"},
	"ascendant": {"longitude":215.5,"sign":"Scorpio","sign_index":7,"degree":5.5,
	              "nakshatra":"Anuradha","pada":1},
	"houses": null,
	"dashas": [{"planet":"Ketu","start":"1994-01-01T00:00:00Z","end":"2001-01-01T00:00:00Z",
	            "level":1,
	            "children":[{"planet":"Ketu","start":"1994-01-01T00:00:00Z",
	                         "end":"1994-06-01T00:00:00Z","level":2}]}],
	"navamsa": {
	  "ascendant": {"longitude":279.5,"sign":"Capricorn","sign_index":9,"degree":9.5,
	                "nakshatra":"Uttara Ashadha","pada":3},
	  "houses": null,
	  "planets": [{"planet":"Sun","longitude":10.0,"sign":"Aries","sign_index":0,
	               "degree":10.0,"house":4,"nakshatra":"Ashwini","nakshatra_index":0,
	               "pada":3,"is_retrograde":false,"is_combust":false,
	               "dignity":"exalted","speed":0.98}]
	},
	"planets": [{"planet":"Sun","longitude":125.0,"sign":"Leo","sign_index":4,
	             "degree":5.0,"house":10,"nakshatra":"Magha","nakshatra_index":9,
	             "pada":2,"is_retrograde":false,"is_combust":false,
	             "dignity":"own","speed":0.98}],
	"dasamsa": {
	  "ascendant": {"longitude":100.5,"sign":"Cancer","sign_index":3,"degree":10.5,
	                "nakshatra":"Pushya","pada":1},
	  "houses": null,
	  "planets": [{"planet":"Sun","longitude":190.0,"sign":"Libra","sign_index":6,
	               "degree":10.0,"house":4,"nakshatra":"Swati","nakshatra_index":14,
	               "pada":1,"is_retrograde":false,"is_combust":false,
	               "dignity":"neutral","speed":0.98}]
	},
	"yogas": [],
	"summary": {"sun_sign":"Leo","moon_sign":"Sagittarius","ascendant_sign":"Scorpio",
	            "moon_nakshatra":"Mula","moon_nakshatra_pada":4}
}`

type harness struct {
	pool     *pgxpool.Pool
	profiles *birthprofiles.Service
	charts   *charts.Service
	// astroCalls counts requests that actually reached astro-service.
	// This is how "the cache did not call astro" is asserted as a fact
	// rather than an assumption.
	astroCalls *atomic.Int32
	setDown    func(bool)
	userID     uuid.UUID
}

func newHarness(t *testing.T) (*harness, func()) {
	t.Helper()
	ctx := context.Background()

	pool, stopDB := startPostgres(ctx, t)

	var calls atomic.Int32
	var down atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chartJSON))
	}))

	astro, err := clients.NewAstro(server.URL, "token", 2*time.Second)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}

	q := dbgen.New(pool)
	profiles := birthprofiles.NewService(q)

	h := &harness{
		pool:       pool,
		profiles:   profiles,
		charts:     charts.NewService(q, pool, astro, profiles, nil),
		astroCalls: &calls,
		setDown:    func(v bool) { down.Store(v) },
		userID:     seedUser(ctx, t, pool),
	}

	return h, func() {
		server.Close()
		stopDB()
	}
}

func startPostgres(ctx context.Context, t *testing.T) (*pgxpool.Pool, func()) {
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
			// astro_dev by name: the init script grants on that database,
			// and a different name makes the container exit during init —
			// which SKIPS the suite rather than failing it.
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

func seedUser(ctx context.Context, t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(ctx,
		`INSERT INTO users (email, email_verified) VALUES ($1, TRUE) RETURNING id`,
		"charts-"+uuid.NewString()+"@example.com").Scan(&id)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

// createProfile makes one profile for the tests that only need "a
// profile that exists".
func (h *harness) createProfile(t *testing.T) birthprofiles.Profile {
	t.Helper()
	profile, err := h.profiles.Create(context.Background(), jaipurInput(h.userID))
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	return profile
}

func jaipurInput(userID uuid.UUID) birthprofiles.CreateInput {
	return birthprofiles.CreateInput{
		UserID:       userID,
		Label:        "self",
		BirthDate:    time.Date(1994, 8, 17, 0, 0, 0, 0, time.UTC),
		BirthTime:    14*time.Hour + 35*time.Minute,
		HasBirthTime: true,
		TimeAccuracy: birthprofiles.AccuracyExact,
		PlaceName:    "Jaipur",
		Latitude:     26.9124,
		Longitude:    75.7873,
		Timezone:     "Asia/Kolkata",
	}
}

// ─── versioning ──────────────────────────────────────────────────────

// Editing birth details creates v2 and leaves v1 readable.
//
// A reading given last month was based on a specific chart. "Which chart
// was that?" has to stay answerable, so a correction is a new version
// rather than an UPDATE — and the old chart stays attached to the old
// version as the correct explanation for the old reading.
func TestEditingCreatesANewVersionAndPreservesTheOld(t *testing.T) {
	ctx := context.Background()
	h, stop := newHarness(t)
	defer stop()

	v1, err := h.profiles.Create(ctx, jaipurInput(h.userID))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if v1.Version != 1 || !v1.IsActive {
		t.Fatalf("v1 is version %d, active %v", v1.Version, v1.IsActive)
	}

	corrected := jaipurInput(h.userID)
	corrected.BirthTime = 15*time.Hour + 5*time.Minute // a half-hour correction

	v2, err := h.profiles.Update(ctx, h.userID, v1.ID, corrected)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if v2.Version != 2 {
		t.Errorf("the new profile is version %d, want 2", v2.Version)
	}
	if v2.ID == v1.ID {
		t.Error("the update mutated v1 in place instead of creating a version")
	}

	// v1 must still be readable, and must point at its replacement.
	reloaded, err := h.profiles.Get(ctx, h.userID, v1.ID)
	if err != nil {
		t.Fatalf("v1 is no longer readable: %v", err)
	}
	if reloaded.IsActive {
		t.Error("v1 is still active after being superseded")
	}
	if reloaded.SupersededBy == nil || *reloaded.SupersededBy != v2.ID {
		t.Errorf("v1.superseded_by = %v, want %v", reloaded.SupersededBy, v2.ID)
	}

	// The correction actually changed the instant — otherwise this test
	// would pass for an update that silently did nothing.
	if reloaded.UTCInstant.Equal(v2.UTCInstant) {
		t.Error("the corrected birth time produced the same UTC instant")
	}

	// Only the new version is active.
	active, err := h.profiles.List(ctx, h.userID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(active) != 1 || active[0].ID != v2.ID {
		t.Errorf("%d active profiles; expected only v2", len(active))
	}
}

func TestVersionHistoryReadsBackwards(t *testing.T) {
	ctx := context.Background()
	h, stop := newHarness(t)
	defer stop()

	v1, _ := h.profiles.Create(ctx, jaipurInput(h.userID))

	second := jaipurInput(h.userID)
	second.BirthTime = 15 * time.Hour
	v2, err := h.profiles.Update(ctx, h.userID, v1.ID, second)
	if err != nil {
		t.Fatalf("second version: %v", err)
	}

	third := jaipurInput(h.userID)
	third.BirthTime = 16 * time.Hour
	v3, err := h.profiles.Update(ctx, h.userID, v2.ID, third)
	if err != nil {
		t.Fatalf("third version: %v", err)
	}

	history, err := h.profiles.Versions(ctx, h.userID, v3.ID)
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}

	if len(history) != 3 {
		t.Fatalf("history has %d entries, want 3", len(history))
	}
	for i, want := range []int32{3, 2, 1} {
		if history[i].Version != want {
			t.Errorf("history[%d] is version %d, want %d", i, history[i].Version, want)
		}
	}
}

// Cross-user access is a 404, never a 403.
//
// A 403 confirms the row exists, which for birth data means confirming
// that a particular person has an account here.
func TestAnotherUsersProfileIsNotFound(t *testing.T) {
	ctx := context.Background()
	h, stop := newHarness(t)
	defer stop()

	mine, _ := h.profiles.Create(ctx, jaipurInput(h.userID))
	stranger := seedUser(ctx, t, h.pool)

	if _, err := h.profiles.Get(ctx, stranger, mine.ID); err == nil {
		t.Fatal("another user read my birth profile")
	} else if !errorIs(err, birthprofiles.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound — a 403 would confirm the profile exists", err)
	}

	if _, err := h.profiles.Update(ctx, stranger, mine.ID, jaipurInput(stranger)); err == nil {
		t.Fatal("another user edited my birth profile")
	}
	if err := h.profiles.Deactivate(ctx, stranger, mine.ID); err == nil {
		t.Fatal("another user deleted my birth profile")
	}
}

// ─── charts and the cache ────────────────────────────────────────────

// The spec's assertion, with a counter rather than an assumption.
func TestACacheHitDoesNotCallAstro(t *testing.T) {
	ctx := context.Background()
	h, stop := newHarness(t)
	defer stop()

	profile, _ := h.profiles.Create(ctx, jaipurInput(h.userID))
	key := charts.Key{ProfileID: profile.ID}

	first, err := h.charts.Get(ctx, h.userID, key)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if first.FromCache {
		t.Error("the first request reported a cache hit")
	}

	after := h.astroCalls.Load()
	if after == 0 {
		t.Fatal("the first request did not call astro at all")
	}

	for range 5 {
		chart, err := h.charts.Get(ctx, h.userID, key)
		if err != nil {
			t.Fatalf("cached Get: %v", err)
		}
		if !chart.FromCache {
			t.Error("a stored chart did not report FromCache")
		}
	}

	if h.astroCalls.Load() != after {
		t.Errorf("%d extra astro calls after the chart was stored; expected 0",
			h.astroCalls.Load()-after)
	}
}

// THE property of this phase.
//
// A user must be able to view their existing Kundli when astro-service is
// down. Only creating a NEW profile fails, and it fails clearly.
func TestAStoredChartIsServedWhileAstroIsDown(t *testing.T) {
	ctx := context.Background()
	h, stop := newHarness(t)
	defer stop()

	profile, _ := h.profiles.Create(ctx, jaipurInput(h.userID))
	key := charts.Key{ProfileID: profile.ID}

	if _, err := h.charts.Get(ctx, h.userID, key); err != nil {
		t.Fatalf("initial compute: %v", err)
	}

	h.setDown(true)

	chart, err := h.charts.GetOrStale(ctx, h.userID, key)
	if err != nil {
		t.Fatalf("a stored chart was not served with astro down: %v", err)
	}
	if !chart.FromCache {
		t.Error("the chart did not come from storage")
	}
	if len(chart.Data) == 0 {
		t.Error("the stored chart has no data")
	}
}

// The other half, and the one that must NOT pretend.
//
// A brand-new profile during an outage has nothing stored. Inventing a
// chart, or returning an empty one, would be worse than failing.
func TestANewProfileFailsCleanlyWhileAstroIsDown(t *testing.T) {
	ctx := context.Background()
	h, stop := newHarness(t)
	defer stop()

	profile, _ := h.profiles.Create(ctx, jaipurInput(h.userID))
	h.setDown(true)

	_, err := h.charts.GetOrStale(ctx, h.userID, charts.Key{ProfileID: profile.ID})
	if err == nil {
		t.Fatal("a chart was produced with astro down and nothing stored")
	}
	if !errorIs(err, charts.ErrUncomputable) {
		t.Errorf("got %v, want ErrUncomputable", err)
	}
}

// Changing the ayanamsa must produce a different chart, not the cached one.
//
// The database rules name this as the classic cache bug: omit an input
// from the key and a user who switches preference is served somebody
// else's chart. Both the UNIQUE constraint and the cache key carry every
// input, and this proves the pair actually behave that way.
func TestADifferentAyanamsaIsADifferentChart(t *testing.T) {
	ctx := context.Background()
	h, stop := newHarness(t)
	defer stop()

	profile, _ := h.profiles.Create(ctx, jaipurInput(h.userID))

	lahiri := charts.Key{ProfileID: profile.ID, Ayanamsa: "lahiri"}
	raman := charts.Key{ProfileID: profile.ID, Ayanamsa: "raman"}

	first, err := h.charts.Get(ctx, h.userID, lahiri)
	if err != nil {
		t.Fatalf("lahiri: %v", err)
	}
	callsAfterFirst := h.astroCalls.Load()

	second, err := h.charts.Get(ctx, h.userID, raman)
	if err != nil {
		t.Fatalf("raman: %v", err)
	}

	if second.FromCache {
		t.Error("a different ayanamsa was served from the lahiri cache")
	}
	if h.astroCalls.Load() == callsAfterFirst {
		t.Error("a different ayanamsa did not reach astro; the key is missing an input")
	}
	if first.ID == second.ID {
		t.Error("both ayanamsas wrote to the same row")
	}

	// The cache keys must differ too, not just the database rows.
	if lahiri.CacheKey() == raman.CacheKey() {
		t.Errorf("both keys are %q", lahiri.CacheKey())
	}
}

func TestTheCacheKeyNamesEveryInput(t *testing.T) {
	profileID := uuid.New()
	base := charts.Key{ProfileID: profileID}

	variants := map[string]charts.Key{
		"chart type":   {ProfileID: profileID, ChartType: "D9"},
		"system":       {ProfileID: profileID, System: "western"},
		"ayanamsa":     {ProfileID: profileID, Ayanamsa: "kp"},
		"house system": {ProfileID: profileID, HouseSystem: "placidus"},
		"profile":      {ProfileID: uuid.New()},
	}

	for name, variant := range variants {
		if variant.CacheKey() == base.CacheKey() {
			t.Errorf("changing the %s did not change the cache key: %q", name, variant.CacheKey())
		}
	}
}

// A chart belongs to a profile, and a profile to a user. Reading somebody
// else's chart must find nothing — the ownership predicate is in the SQL,
// so this proves the join rather than a handler check.
func TestAnotherUsersChartIsNotFound(t *testing.T) {
	ctx := context.Background()
	h, stop := newHarness(t)
	defer stop()

	profile, _ := h.profiles.Create(ctx, jaipurInput(h.userID))
	if _, err := h.charts.Get(ctx, h.userID, charts.Key{ProfileID: profile.ID}); err != nil {
		t.Fatalf("seed chart: %v", err)
	}

	stranger := seedUser(ctx, t, h.pool)
	if _, err := h.charts.Get(ctx, stranger, charts.Key{ProfileID: profile.ID}); err == nil {
		t.Fatal("another user read my chart")
	}
}

// An unknown birth time still produces a chart, with nulls where the
// ascendant would be.
func TestAnUnknownBirthTimeStillProducesAChart(t *testing.T) {
	ctx := context.Background()
	h, stop := newHarness(t)
	defer stop()

	in := jaipurInput(h.userID)
	in.TimeAccuracy = birthprofiles.AccuracyUnknown
	in.HasBirthTime = false

	profile, err := h.profiles.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if profile.BirthTime != nil {
		t.Error("an unknown-time profile stored a birth time")
	}

	chart, err := h.charts.Get(ctx, h.userID, charts.Key{ProfileID: profile.ID})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(chart.Data) == 0 {
		t.Error("no chart data for an unknown birth time")
	}
}

// An exact profile without a time is refused before it reaches the
// database, so the error names the field rather than surfacing a
// constraint violation.
func TestAnExactProfileWithoutATimeIsRefused(t *testing.T) {
	ctx := context.Background()
	h, stop := newHarness(t)
	defer stop()

	in := jaipurInput(h.userID)
	in.HasBirthTime = false

	if _, err := h.profiles.Create(ctx, in); err == nil {
		t.Fatal("an exact profile with no birth time was accepted")
	} else if !errorIs(err, birthprofiles.ErrInvalidInput) {
		t.Errorf("got %v, want ErrInvalidInput", err)
	}
}

func errorIs(err, target error) bool { return errors.Is(err, target) }

// ─── D9 ──────────────────────────────────────────────────────────────

// The gate asks for "D9 computed and stored". Storing the D1 payload
// under a D9 label satisfies the words and nothing else: a client asking
// for the navamsa gets the rasi, with the real navamsa buried in a field
// it was not looking at.
//
// The two charts genuinely differ — a navamsa ascendant is a ninth-part
// division of the rasi one — so asserting they differ is the whole test.
func TestTheStoredD9IsTheNavamsaAndNotACopyOfTheD1(t *testing.T) {
	h, cleanup := newHarness(t)
	defer cleanup()
	ctx := context.Background()

	profile := h.createProfile(t)

	rasi, err := h.charts.Get(ctx, h.userID, charts.Key{
		ProfileID: profile.ID, ChartType: charts.ChartTypeRasi,
	})
	if err != nil {
		t.Fatalf("D1: %v", err)
	}

	navamsa, err := h.charts.Get(ctx, h.userID, charts.Key{
		ProfileID: profile.ID, ChartType: charts.ChartTypeNavamsa,
	})
	if err != nil {
		t.Fatalf("D9: %v", err)
	}

	if string(rasi.Data) == string(navamsa.Data) {
		t.Fatal("the D1 and D9 payloads are byte-identical; the D9 row is a " +
			"relabelled D1, and a client asking for the navamsa is served the rasi")
	}

	ascendantOf := func(raw []byte, label string) string {
		var chart struct {
			Ascendant *struct {
				Sign string `json:"sign"`
			} `json:"ascendant"`
		}
		if err := json.Unmarshal(raw, &chart); err != nil {
			t.Fatalf("decode %s: %v", label, err)
		}
		if chart.Ascendant == nil {
			t.Fatalf("%s has no ascendant", label)
		}
		return chart.Ascendant.Sign
	}

	if got := ascendantOf(rasi.Data, "D1"); got != "Scorpio" {
		t.Fatalf("D1 ascendant is %s, want Scorpio", got)
	}
	if got := ascendantOf(navamsa.Data, "D9"); got != "Capricorn" {
		t.Fatalf("D9 ascendant is %s, want Capricorn — the navamsa ascendant, "+
			"not the rasi's", got)
	}
}

// One astro call, two charts. The navamsa arrives inside the same
// response, so fetching a D9 after a D1 must not call out again — and
// more importantly must not be able to disagree with the D1 it was
// derived from.
func TestFetchingBothChartsCostsOneAstroCall(t *testing.T) {
	h, cleanup := newHarness(t)
	defer cleanup()
	ctx := context.Background()

	profile := h.createProfile(t)
	before := h.astroCalls.Load()

	for _, chartType := range []string{charts.ChartTypeRasi, charts.ChartTypeNavamsa} {
		if _, err := h.charts.Get(ctx, h.userID, charts.Key{
			ProfileID: profile.ID, ChartType: chartType,
		}); err != nil {
			t.Fatalf("%s: %v", chartType, err)
		}
	}

	if calls := h.astroCalls.Load() - before; calls != 1 {
		t.Fatalf("fetching D1 and D9 made %d calls to astro-service, want 1 — "+
			"the navamsa is in the same response, and a second call could "+
			"return a chart computed from a different ephemeris state", calls)
	}
}

// Dashas belong to the rasi. A dasha tree hanging off a D9 row is a
// second, independently-stored copy of something that has one correct
// value, and FindDashaAt would have two rows per level to choose from.
func TestOnlyTheRasiCarriesADashaTree(t *testing.T) {
	h, cleanup := newHarness(t)
	defer cleanup()
	ctx := context.Background()

	profile := h.createProfile(t)

	// D9 FIRST, deliberately. The first Get is the one that computes and
	// stores; the second is a cache hit and runs none of this code. Asking
	// for the rasi first made an earlier version of this test vacuous —
	// hanging the tree off the requested chart instead of the rasi still
	// passed, because the requested chart WAS the rasi.
	for _, chartType := range []string{charts.ChartTypeNavamsa, charts.ChartTypeRasi} {
		if _, err := h.charts.Get(ctx, h.userID, charts.Key{
			ProfileID: profile.ID, ChartType: chartType,
		}); err != nil {
			t.Fatalf("%s: %v", chartType, err)
		}
	}

	// And the rasi must have one, or "zero on the D9" is trivially true.
	var onD1 int
	if err := h.pool.QueryRow(ctx,
		`SELECT count(*) FROM dashas d
		 JOIN charts c ON c.id = d.chart_id
		 WHERE c.birth_profile_id = $1 AND c.chart_type = 'D1'`,
		profile.ID).Scan(&onD1); err != nil {
		t.Fatalf("count D1: %v", err)
	}
	if onD1 == 0 {
		t.Fatal("no dasha rows on the rasi; the D9 having none proves nothing")
	}

	var onD9 int
	if err := h.pool.QueryRow(ctx,
		`SELECT count(*) FROM dashas d
		 JOIN charts c ON c.id = d.chart_id
		 WHERE c.birth_profile_id = $1 AND c.chart_type = 'D9'`,
		profile.ID).Scan(&onD9); err != nil {
		t.Fatalf("count: %v", err)
	}
	if onD9 != 0 {
		t.Fatalf("%d dasha rows hang off the D9 chart; dashas are a property of "+
			"the birth moment, not of a divisional chart", onD9)
	}
}

// ─── D10 ─────────────────────────────────────────────────────────────

// The Phase 3 gate requires D1, D9 and D10 all viewable, so the same
// property that caught the D9 being a relabelled D1 has to hold for the
// dasamsa: three requests, three genuinely different charts, one call to
// astro-service.
func TestTheStoredD10IsTheDasamsaAndDiffersFromBothOthers(t *testing.T) {
	h, cleanup := newHarness(t)
	defer cleanup()
	ctx := context.Background()

	profile := h.createProfile(t)
	before := h.astroCalls.Load()

	payloads := map[string]string{}
	for _, chartType := range []string{
		charts.ChartTypeRasi, charts.ChartTypeNavamsa, charts.ChartTypeDasamsa,
	} {
		chart, err := h.charts.Get(ctx, h.userID, charts.Key{
			ProfileID: profile.ID, ChartType: chartType,
		})
		if err != nil {
			t.Fatalf("%s: %v", chartType, err)
		}
		payloads[chartType] = string(chart.Data)
	}

	// Three distinct payloads. Any two being equal means one chart type
	// is a relabelled copy of another — the Phase 2 bug, in a new place.
	seen := map[string]string{}
	for chartType, payload := range payloads {
		if other, clash := seen[payload]; clash {
			t.Fatalf("%s and %s have byte-identical payloads; one is a relabelled "+
				"copy of the other", chartType, other)
		}
		seen[payload] = chartType
	}

	if calls := h.astroCalls.Load() - before; calls != 1 {
		t.Fatalf("fetching D1, D9 and D10 made %d calls to astro-service, want 1 — "+
			"all three come out of one response, and separate calls could land on "+
			"different ephemeris states", calls)
	}
}

// The dasamsa is a divisional chart, so like the navamsa it carries no
// dasha tree of its own.
func TestTheDasamsaCarriesNoDashaTree(t *testing.T) {
	h, cleanup := newHarness(t)
	defer cleanup()
	ctx := context.Background()

	profile := h.createProfile(t)
	// D10 first: the first Get is the one that computes and stores.
	for _, chartType := range []string{charts.ChartTypeDasamsa, charts.ChartTypeRasi} {
		if _, err := h.charts.Get(ctx, h.userID, charts.Key{
			ProfileID: profile.ID, ChartType: chartType,
		}); err != nil {
			t.Fatalf("%s: %v", chartType, err)
		}
	}

	var onD10, onD1 int
	count := func(chartType string, into *int) {
		if err := h.pool.QueryRow(ctx,
			`SELECT count(*) FROM dashas d
			 JOIN charts c ON c.id = d.chart_id
			 WHERE c.birth_profile_id = $1 AND c.chart_type = $2`,
			profile.ID, chartType).Scan(into); err != nil {
			t.Fatalf("count %s: %v", chartType, err)
		}
	}
	count("D10", &onD10)
	count("D1", &onD1)

	if onD1 == 0 {
		t.Fatal("no dasha rows on the rasi; the D10 having none proves nothing")
	}
	if onD10 != 0 {
		t.Fatalf("%d dasha rows hang off the D10", onD10)
	}
}
