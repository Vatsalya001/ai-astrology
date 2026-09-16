//go:build integration

package transits_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/jobs"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/testsupport"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/transits"
)

// Real Postgres, stubbed astro — the same split as the charts suite. The
// properties being tested are the UNIQUE constraint that makes a slot
// idempotent, and the behaviour of this package when astro is DOWN,
// which a real astro-service will not do on request.

// Sign indices used throughout: 0 Aries … 8 Sagittarius, 9 Capricorn,
// 10 Aquarius, 11 Pisces.
const (
	aries       = 0
	sagittarius = 8
	capricorn   = 9
	aquarius    = 10
)

var signNames = map[int]string{
	0: "Aries", 1: "Taurus", 2: "Gemini", 3: "Cancer", 4: "Leo", 5: "Virgo",
	6: "Libra", 7: "Scorpio", 8: "Sagittarius", 9: "Capricorn",
	10: "Aquarius", 11: "Pisces",
}

// wrongHouse is what the stub reports for house_from_moon on every
// planet. It is a fixed, obviously-wrong number so that if it ever
// reaches the database or a response, the test says so instead of the
// value happening to be right by coincidence.
const wrongHouse = 7

type stubAstro struct {
	mu sync.Mutex
	// requests holds the decoded body of every call that arrived, which
	// is how "astro was asked for the slot boundary" becomes a
	// measurement rather than a belief.
	requests []map[string]any
	down     bool
	// saturnSign can be moved between tests to drive the Sade Sati cases.
	saturnSign int
}

type harness struct {
	pool      *pgxpool.Pool
	astro     *stubAstro
	refresher *transits.Refresher
	reader    *transits.Reader
	handler   *transits.RefreshHandler
	// now is the instant the handler believes it is running at.
	now time.Time
}

// slot is the aligned six-hour boundary every assertion is written
// against. `now` sits five minutes inside it on purpose: a worker never
// wakes exactly on the boundary, and the whole point of Slot is that the
// stored row does not record the moment the worker happened to wake.
var (
	slotStart = time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	runAt     = slotStart.Add(5 * time.Minute)
)

func newHarness(t *testing.T) (*harness, func()) {
	t.Helper()
	ctx := context.Background()

	pool, stopDB := startPostgres(ctx, t)

	stub := &stubAstro{saturnSign: capricorn}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		defer stub.mu.Unlock()

		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		stub.requests = append(stub.requests, body)

		if stub.down {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(stub.response())
	}))

	astro, err := clients.NewAstro(server.URL, "token", 2*time.Second)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}

	q := dbgen.New(pool)
	refresher := transits.NewRefresher(q, astro, jobs.TransitRefreshInterval, testLogger())

	h := &harness{
		pool:      pool,
		astro:     stub,
		refresher: refresher,
		reader:    transits.NewReader(q),
		now:       runAt,
	}
	h.handler = transits.NewRefreshHandler(
		refresher, jobs.TransitRetention, func() time.Time { return h.now }, testLogger())

	return h, func() {
		server.Close()
		stopDB()
	}
}

// response builds a TransitResponse whose house_from_moon fields are all
// deliberately wrong.
func (s *stubAstro) response() []byte {
	positions := []map[string]any{
		s.position("Sun", aries, false),
		s.position("Moon", sagittarius, false),
		s.position("Mars", aquarius, false),
		s.position("Saturn", s.saturnSign, true),
	}

	body, _ := json.Marshal(map[string]any{
		"at":             slotStart,
		"ayanamsa":       "lahiri",
		"ayanamsa_value": 24.21,
		"sade_sati": map[string]any{
			"is_active": false, "current_phase": nil, "houses_from_moon": wrongHouse,
			"moon_sign": "Aries", "saturn_sign": signNames[s.saturnSign],
		},
		"transits": positions,
	})
	return body
}

func (s *stubAstro) position(planet string, sign int, retrograde bool) map[string]any {
	return map[string]any{
		"planet": planet, "sign": signNames[sign], "sign_index": sign,
		"degree": 12.5, "longitude": float64(sign)*30 + 12.5,
		"is_retrograde": retrograde,
		// Wrong on purpose. Nothing may persist or echo these.
		"house_from_moon": wrongHouse, "house_from_ascendant": wrongHouse,
	}
}

func (s *stubAstro) setDown(down bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.down = down
}

func (s *stubAstro) calls() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.requests...)
}

func (h *harness) countRows(t *testing.T) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM transits`).Scan(&n); err != nil {
		t.Fatalf("count transits: %v", err)
	}
	return n
}

// ─── the slot boundary ───────────────────────────────────────────────

// A stored row must be reproducible from its own primary key: ask astro
// for `timestamp` again and the same numbers must come back. That is only
// true if the worker asked for the BOUNDARY rather than for the moment it
// woke up.
func TestRefreshComputesAtTheSlotBoundaryNotAtWakeUpTime(t *testing.T) {
	h, cleanup := newHarness(t)
	defer cleanup()

	if _, err := h.refresher.Refresh(context.Background(), runAt); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	calls := h.astro.calls()
	if len(calls) != 1 {
		t.Fatalf("expected one call to astro, got %d", len(calls))
	}

	asked, _ := time.Parse(time.RFC3339, calls[0]["at"].(string))
	if !asked.Equal(slotStart) {
		t.Fatalf("astro was asked for %s but the slot is %s — "+
			"a stored row is then not reproducible from its own key",
			asked.Format(time.RFC3339), slotStart.Format(time.RFC3339))
	}

	var stored time.Time
	if err := h.pool.QueryRow(context.Background(),
		`SELECT timestamp FROM transits LIMIT 1`).Scan(&stored); err != nil {
		t.Fatalf("read timestamp: %v", err)
	}
	if !stored.UTC().Equal(slotStart) {
		t.Fatalf("stored timestamp %s, want the slot boundary %s",
			stored.UTC().Format(time.RFC3339), slotStart.Format(time.RFC3339))
	}
}

// Two refreshes inside one slot — a retry, or two replicas that both got
// through the lock — must upsert, not accumulate.
func TestTwoRefreshesInOneSlotProduceOneRowPerPlanet(t *testing.T) {
	h, cleanup := newHarness(t)
	defer cleanup()
	ctx := context.Background()

	written, err := h.refresher.Refresh(ctx, slotStart.Add(1*time.Minute))
	if err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	if got := h.countRows(t); got != written {
		t.Fatalf("after one refresh: %d rows for %d planets", got, written)
	}

	// Nearly six hours later, still inside the same slot.
	if _, err := h.refresher.Refresh(ctx, slotStart.Add(5*time.Hour+59*time.Minute)); err != nil {
		t.Fatalf("second Refresh: %v", err)
	}

	if got := h.countRows(t); got != written {
		t.Fatalf("two refreshes in one slot left %d rows, want %d — "+
			"the UNIQUE constraint is not doing its job, and the table grows without bound",
			got, written)
	}

	// The next slot is a genuinely different instant and must add rows.
	if _, err := h.refresher.Refresh(ctx, slotStart.Add(6*time.Hour)); err != nil {
		t.Fatalf("next-slot Refresh: %v", err)
	}
	if got := h.countRows(t); got != 2*written {
		t.Fatalf("the next slot produced %d rows in total, want %d — "+
			"if this equals the previous count, every slot is overwriting the last",
			got, 2*written)
	}
}

// ─── the table stays global ──────────────────────────────────────────

// The single most important property in this package. astro returns
// houses-from-Moon computed against whatever natal Moon it was given;
// the refresher hands it a fixed reference sign and must throw those
// houses away. If even one reaches the database, every user whose Moon is
// not in Aries reads somebody else's gochara — and it looks completely
// plausible.
func TestStoredRowsCarryNoHouseFromMoon(t *testing.T) {
	h, cleanup := newHarness(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := h.refresher.Refresh(ctx, runAt); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// The reference sign the refresher claims must be the documented one,
	// or the discarded houses were discarded for a different reason than
	// the comment says.
	calls := h.astro.calls()
	if got := int(calls[0]["natal_moon_sign"].(float64)); got != transits.ReferenceMoonSign {
		t.Fatalf("refresher asked astro with natal_moon_sign %d, want the documented reference %d",
			got, transits.ReferenceMoonSign)
	}

	rows, err := h.pool.Query(ctx, `SELECT planet, metadata FROM transits`)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var planet string
		var raw []byte
		if scanErr := rows.Scan(&planet, &raw); scanErr != nil {
			t.Fatalf("scan: %v", scanErr)
		}

		var metadata map[string]any
		if unmarshalErr := json.Unmarshal(raw, &metadata); unmarshalErr != nil {
			t.Fatalf("decode metadata for %s: %v", planet, unmarshalErr)
		}

		keys := make([]string, 0, len(metadata))
		for key := range metadata {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		want := []string{"longitude", "sign_index"}
		if len(keys) != len(want) {
			t.Fatalf("%s metadata holds %v; only %v are global to every user, "+
				"anything else is one user's reading stored in a shared table",
				planet, keys, want)
		}
		for i := range want {
			if keys[i] != want[i] {
				t.Fatalf("%s metadata holds %v, want exactly %v", planet, keys, want)
			}
		}
	}
}

// And the other half: the house a caller reads back must come from the
// rotation, never from what astro said.
func TestHousesAreDerivedPerReaderNotReadFromStorage(t *testing.T) {
	h, cleanup := newHarness(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := h.refresher.Refresh(ctx, runAt); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Saturn is in Capricorn (the stub default). Three different natal
	// Moons must see three different houses for that one stored row.
	cases := []struct {
		moon int
		want int
	}{
		{capricorn, 1},   // Saturn over the Moon
		{aquarius, 12},   // Saturn one sign behind
		{sagittarius, 2}, // Saturn one sign ahead
	}

	for _, tc := range cases {
		positions, err := h.reader.At(ctx, runAt, tc.moon)
		if err != nil {
			t.Fatalf("At(moon=%d): %v", tc.moon, err)
		}

		saturn := find(t, positions, "Saturn")
		if saturn.HouseFromMoon == wrongHouse {
			t.Fatalf("Saturn came back in house %d, which is exactly what the stub "+
				"reported — astro's house_from_moon is being passed through", wrongHouse)
		}
		if saturn.HouseFromMoon != tc.want {
			t.Fatalf("with the Moon in sign %d, Saturn in Capricorn is house %d, want %d",
				tc.moon, saturn.HouseFromMoon, tc.want)
		}
		if !saturn.IsRetrograde {
			t.Fatal("retrograde was not preserved through storage")
		}
	}
}

// ─── the outage story ────────────────────────────────────────────────

// The same property PR 10 established for charts, for the answer users
// ask for most often. Sade Sati must keep working while astro is down,
// because it is computed from stored rows and a rotation, with no call
// out at all.
func TestSadeSatiKeepsAnsweringWhileAstroIsDown(t *testing.T) {
	h, cleanup := newHarness(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := h.refresher.Refresh(ctx, runAt); err != nil {
		t.Fatalf("seed Refresh: %v", err)
	}
	before := h.countRows(t)

	h.astro.setDown(true)

	// The scheduled job fails, and must SAY it failed so asynq retries.
	if err := h.handler.Run(ctx, nil); err == nil {
		t.Fatal("the refresh job reported success with astro down; " +
			"asynq only retries a task that returns an error, so this would " +
			"leave the table stale until the next six-hourly run")
	}

	if after := h.countRows(t); after != before {
		t.Fatalf("a failed refresh changed the row count from %d to %d — "+
			"the fallback data was damaged by the outage it exists to survive",
			before, after)
	}

	status, err := h.reader.SadeSatiAt(ctx, runAt, capricorn)
	if err != nil {
		t.Fatalf("Sade Sati during an outage: %v", err)
	}
	if !status.IsActive || status.Phase == nil || *status.Phase != transits.PhasePeak {
		t.Fatalf("Saturn in Capricorn over a Capricorn Moon during an outage: "+
			"active=%v phase=%v, want an active peak", status.IsActive, status.Phase)
	}
}

// Pruning must be downstream of a successful write. The other order
// deletes the fallback during the outage.
func TestAFailedRefreshNeverPrunes(t *testing.T) {
	h, cleanup := newHarness(t)
	defer cleanup()
	ctx := context.Background()

	// A row from well outside any retention window.
	ancient := slotStart.Add(-365 * 24 * time.Hour)
	if _, err := h.pool.Exec(ctx,
		`INSERT INTO transits (planet, sign, degree, timestamp, metadata)
		 VALUES ('Saturn', 'Leo', 1.0, $1, '{"longitude":121.0,"sign_index":4}')`,
		ancient); err != nil {
		t.Fatalf("seed ancient row: %v", err)
	}

	h.astro.setDown(true)
	if err := h.handler.Run(ctx, nil); err == nil {
		t.Fatal("expected the job to fail with astro down")
	}
	if got := h.countRows(t); got != 1 {
		t.Fatalf("a failed run left %d rows, want the 1 seeded — it pruned before it knew "+
			"whether it had anything to replace them with", got)
	}

	// With astro back, the same run both writes and prunes.
	h.astro.setDown(false)
	if err := h.handler.Run(ctx, nil); err != nil {
		t.Fatalf("Run with astro up: %v", err)
	}

	var survivors int
	if err := h.pool.QueryRow(ctx,
		`SELECT count(*) FROM transits WHERE timestamp = $1`, ancient).Scan(&survivors); err != nil {
		t.Fatalf("count ancient: %v", err)
	}
	if survivors != 0 {
		t.Fatalf("the year-old row survived a successful run; retention is %s",
			jobs.TransitRetention)
	}
	if h.countRows(t) == 0 {
		t.Fatal("the prune took the fresh rows with it")
	}
}

// A 200 carrying an empty planet list is not success. Treating it as one
// would prune good rows and store nothing in their place.
func TestAnEmptyPlanetListIsAFailureNotAnEmptyRefresh(t *testing.T) {
	h, cleanup := newHarness(t)
	defer cleanup()
	ctx := context.Background()

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"at":"2026-09-16T12:00:00Z","ayanamsa":"lahiri",
			"ayanamsa_value":24.21,"transits":[],
			"sade_sati":{"is_active":false,"current_phase":null,"houses_from_moon":7,
			             "moon_sign":"Aries","saturn_sign":"Leo"}}`))
	}))
	defer empty.Close()

	astro, err := clients.NewAstro(empty.URL, "token", 2*time.Second)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}
	handler := transits.NewRefreshHandler(
		transits.NewRefresher(dbgen.New(h.pool), astro, jobs.TransitRefreshInterval, testLogger()),
		jobs.TransitRetention, func() time.Time { return runAt }, testLogger())

	if err := handler.Run(ctx, nil); err == nil {
		t.Fatal("a 200 with no planets was accepted as a successful refresh; " +
			"that stores nothing and then prunes what was there")
	}
}

// ─── reading ─────────────────────────────────────────────────────────

func TestSadeSatiPhasesFromOneStoredSaturn(t *testing.T) {
	h, cleanup := newHarness(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := h.refresher.Refresh(ctx, runAt); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Saturn is in Capricorn; the natal Moon moves around it.
	cases := []struct {
		name   string
		moon   int
		active bool
		phase  string
	}{
		{"rising: Saturn in the 12th", aquarius, true, transits.PhaseRising},
		{"peak: Saturn over the Moon", capricorn, true, transits.PhasePeak},
		{"setting: Saturn in the 2nd", sagittarius, true, transits.PhaseSetting},
		{"not Sade Sati at all", aries, false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, err := h.reader.SadeSatiAt(ctx, runAt, tc.moon)
			if err != nil {
				t.Fatalf("SadeSatiAt: %v", err)
			}
			if status.IsActive != tc.active {
				t.Fatalf("is_active = %v, want %v (Saturn %d houses from the Moon)",
					status.IsActive, tc.active, status.HousesFromMoon)
			}
			if !tc.active {
				if status.Phase != nil {
					t.Fatalf("an inactive Sade Sati reported phase %q", *status.Phase)
				}
				return
			}
			if status.Phase == nil || *status.Phase != tc.phase {
				t.Fatalf("phase = %v, want %q", status.Phase, tc.phase)
			}
		})
	}
}

// "We have not computed transits yet" and "no planets are transiting"
// are wildly different statements, and only one of them can ever be true.
// An empty slice would let a caller render the second.
func TestAnEmptyTableIsAnErrorNotAnEmptyList(t *testing.T) {
	h, cleanup := newHarness(t)
	defer cleanup()

	_, err := h.reader.At(context.Background(), runAt, aries)
	if !errors.Is(err, transits.ErrNoTransits) {
		t.Fatalf("reading an empty table gave %v, want ErrNoTransits", err)
	}
}

// A partial refresh that lost Saturn must not be reported as "you are
// not in Sade Sati". That is a confident wrong answer to the most
// consequential question the product answers.
func TestSaturnMissingIsAnErrorNotAnAllClear(t *testing.T) {
	h, cleanup := newHarness(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := h.pool.Exec(ctx,
		`INSERT INTO transits (planet, sign, degree, timestamp, metadata)
		 VALUES ('Sun', 'Leo', 1.0, $1, '{"longitude":121.0,"sign_index":4}')`,
		slotStart); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := h.reader.SadeSatiAt(ctx, runAt, capricorn)
	if err == nil {
		t.Fatal("a table with no Saturn reported a Sade Sati verdict")
	}
	if !errors.Is(err, transits.ErrNoTransits) {
		t.Fatalf("got %v, want an ErrNoTransits", err)
	}
}

func TestReaderRejectsAMoonSignOutsideTheZodiac(t *testing.T) {
	h, cleanup := newHarness(t)
	defer cleanup()

	for _, sign := range []int{-1, 12, 99} {
		if _, err := h.reader.At(context.Background(), runAt, sign); err == nil {
			t.Fatalf("natal moon sign %d was accepted; the rotation would silently "+
				"produce a house outside 1..12", sign)
		}
	}
}

// ─── helpers ─────────────────────────────────────────────────────────

// testLogger discards. These tests assert on rows and errors, not on log
// lines, and the refresher is chatty enough to bury a real failure.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func find(t *testing.T, positions []transits.Position, planet string) transits.Position {
	t.Helper()
	for _, p := range positions {
		if p.Planet == planet {
			return p
		}
	}
	t.Fatalf("%s is missing from %d positions", planet, len(positions))
	return transits.Position{}
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
