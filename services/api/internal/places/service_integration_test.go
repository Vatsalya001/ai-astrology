//go:build integration

package places_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/places"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/testsupport"
)

// Place search against real Postgres.
//
// Real, because the feature IS the SQL: a trigram index, an ILIKE prefix
// match and a population ordering. A mocked repository would assert that
// I remembered to call a method, not that "jaip" returns Jaipur.

func startPostgres(ctx context.Context, t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()

	initSQL, err := filepath.Abs(filepath.Join(
		"..", "..", "..", "..", "infrastructure", "docker", "init", "01-init.sql"))
	if err != nil {
		t.Fatalf("resolve init script: %v", err)
	}
	if _, statErr := os.Stat(initSQL); statErr != nil {
		// Fatal rather than skip. A wrong path here would leave the
		// container without pg_trgm and every assertion below would be
		// testing nothing.
		t.Fatalf("init script missing at %s: %v", initSQL, statErr)
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "pgvector/pgvector:pg16",
			ExposedPorts: []string{"5432/tcp"},
			// astro_dev, not a test-specific name: the init script
			// grants on that database by name, so a different one makes
			// the container exit during init — code 3, and every test
			// then SKIPS rather than fails.
			Env: map[string]string{
				"POSTGRES_USER":     "astro",
				"POSTGRES_PASSWORD": "astro",
				"POSTGRES_DB":       "astro_dev",
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

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("mapped port: %v", err)
	}

	pool, err := pgxpool.New(ctx,
		"postgres://astro:astro@"+host+":"+port.Port()+"/astro_dev?sslmode=disable")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	applyMigrations(ctx, t, pool)
	seedPlaces(ctx, t, pool)

	return pool, func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	}
}

func applyMigrations(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join("..", "..", "db", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no migrations found in %s (err=%v)", dir, err)
	}
	sort.Strings(files)

	for _, file := range files {
		body, readErr := os.ReadFile(file)
		if readErr != nil {
			t.Fatalf("read %s: %v", file, readErr)
		}
		if _, execErr := pool.Exec(ctx, string(body)); execErr != nil {
			t.Fatalf("apply %s: %v", filepath.Base(file), execErr)
		}
	}
}

// A fixture chosen to make the ranking assertion meaningful.
//
// Two places called Jaipur: the Rajasthan city of three million and the
// Odisha village of a few hundred. That collision is real, and it is the
// reason population ranking exists.
func seedPlaces(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	q := dbgen.New(pool)

	rajasthan, odisha, up := "Rajasthan", "Odisha", "Uttar Pradesh"
	england, maharashtra := "England", "Maharashtra"

	for _, p := range []dbgen.UpsertPlaceParams{
		{ID: 1269515, Name: "Jaipur", AsciiName: "Jaipur", Admin1: &rajasthan,
			CountryCode: "IN", Latitude: 26.9124, Longitude: 75.7873,
			Timezone: "Asia/Kolkata", Population: 2711758},
		{ID: 1269516, Name: "Jaipur", AsciiName: "Jaipur", Admin1: &odisha,
			CountryCode: "IN", Latitude: 20.8500, Longitude: 86.3300,
			Timezone: "Asia/Kolkata", Population: 632},
		{ID: 1269517, Name: "Jaipurhat", AsciiName: "Jaipurhat", Admin1: &up,
			CountryCode: "IN", Latitude: 25.1000, Longitude: 89.0000,
			Timezone: "Asia/Kolkata", Population: 87000},
		{ID: 1275339, Name: "Mumbai", AsciiName: "Mumbai", Admin1: &maharashtra,
			CountryCode: "IN", Latitude: 19.0760, Longitude: 72.8777,
			Timezone: "Asia/Kolkata", Population: 12691836},
		{ID: 2643743, Name: "London", AsciiName: "London", Admin1: &england,
			CountryCode: "GB", Latitude: 51.5085, Longitude: -0.1257,
			Timezone: "Europe/London", Population: 8961989},
	} {
		if err := q.UpsertPlace(ctx, p); err != nil {
			t.Fatalf("seed %s: %v", p.Name, err)
		}
	}
}

// THE test for this feature.
//
// A user typing "jaip" means the city, essentially always. Returning the
// village of 632 people first is the difference between a completed
// signup and an abandoned one — and this is the highest drop-off point in
// the entire product.
func TestPopulationRankingPutsTheCityFirst(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	results, err := places.NewService(dbgen.New(pool)).Search(ctx, "jaip", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) < 2 {
		t.Fatalf("got %d results for \"jaip\"; the fixture has three", len(results))
	}

	if results[0].Admin1 != "Rajasthan" {
		t.Errorf("first result is Jaipur, %s — expected Rajasthan, the city of 2.7 million",
			results[0].Admin1)
	}

	// And the ordering is genuinely by population, not an accident of
	// insertion order.
	for i := 1; i < len(results); i++ {
		if results[i].Population > results[i-1].Population {
			t.Errorf("results are not population-ordered: %s (%d) after %s (%d)",
				results[i].Name, results[i].Population,
				results[i-1].Name, results[i-1].Population)
		}
	}
}

func TestSearchIsCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	service := places.NewService(dbgen.New(pool))

	var counts []int
	for _, query := range []string{"jaipur", "JAIPUR", "JaIpUr", "  jaipur  "} {
		results, err := service.Search(ctx, query, 10)
		if err != nil {
			t.Fatalf("Search(%q): %v", query, err)
		}
		counts = append(counts, len(results))
	}

	for i := 1; i < len(counts); i++ {
		if counts[i] != counts[0] {
			t.Fatalf("case and whitespace changed the result count: %v", counts)
		}
	}
}

func TestSearchIsAPrefixMatch(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	service := places.NewService(dbgen.New(pool))

	// "jaipur" matches Jaipur AND Jaipurhat, because it is a prefix.
	prefixed, err := service.Search(ctx, "jaipur", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(prefixed) != 3 {
		t.Errorf("got %d results, expected 3 (two Jaipurs and Jaipurhat)", len(prefixed))
	}

	// A substring that is not a prefix must not match. Infix matching
	// would return unrelated towns for common fragments and bury the real
	// answer.
	infix, err := service.Search(ctx, "aipur", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(infix) != 0 {
		t.Errorf("an infix matched %d places; search is prefix-only", len(infix))
	}
}

func TestTheLimitIsHonoured(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	results, err := places.NewService(dbgen.New(pool)).Search(ctx, "jaip", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("limit 1 returned %d results", len(results))
	}
	// And it is still the best one, not an arbitrary one.
	if results[0].Admin1 != "Rajasthan" {
		t.Errorf("limit 1 returned Jaipur, %s — the limit is applied before the ranking",
			results[0].Admin1)
	}
}

// Every place carries the timezone birth-time resolution needs.
//
// Resolved at import so selecting a place needs no lookup at request
// time — and so a place can never reach the chart pipeline without one,
// which would resolve to UTC and silently shift the chart.
func TestEveryPlaceCarriesAnIANATimezone(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	results, err := places.NewService(dbgen.New(pool)).Search(ctx, "ja", 50)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	for _, place := range results {
		if place.Timezone == "" {
			t.Errorf("%s has no timezone", place.Name)
			continue
		}
		if _, err := time.LoadLocation(place.Timezone); err != nil {
			t.Errorf("%s has timezone %q, which tzdata does not recognise: %v",
				place.Name, place.Timezone, err)
		}
	}
}

// Selection resolves by id, not by client-supplied coordinates.
//
// A browser could send any latitude it liked and the chart would be
// computed for wherever that is. Going through the id means the
// coordinates come from the dataset.
func TestGetResolvesByID(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	place, err := places.NewService(dbgen.New(pool)).Get(ctx, 1269515)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if place.Name != "Jaipur" || place.Admin1 != "Rajasthan" {
		t.Errorf("got %s, %s", place.Name, place.Admin1)
	}
	if place.Timezone != "Asia/Kolkata" {
		t.Errorf("timezone = %q", place.Timezone)
	}
}

// The spec's budget: p95 under 50 ms.
//
// Place search runs on every keystroke behind a 250 ms debounce, so it
// has to be comfortably faster than the typing it responds to.
func TestSearchMeetsItsLatencyBudget(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	service := places.NewService(dbgen.New(pool))

	// Warm the connection and the plan cache, so the measurement is of
	// steady-state search rather than first-query overhead.
	for range 5 {
		if _, err := service.Search(ctx, "jai", 10); err != nil {
			t.Fatalf("warmup: %v", err)
		}
	}

	const samples = 50
	durations := make([]time.Duration, 0, samples)

	for range samples {
		started := time.Now()
		if _, err := service.Search(ctx, "jai", 10); err != nil {
			t.Fatalf("Search: %v", err)
		}
		durations = append(durations, time.Since(started))
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[samples*95/100]

	t.Logf("place search p50=%v p95=%v", durations[samples/2], p95)

	// Generous against the spec's 50 ms: this runs against a container on
	// a shared CI runner, so the budget here is for catching a missing
	// index — which would be milliseconds versus seconds — not for
	// measuring production latency.
	if p95 > 250*time.Millisecond {
		t.Errorf("p95 is %v; the spec budgets 50ms and this is far beyond any "+
			"plausible container overhead — is the trigram index present?", p95)
	}
}
