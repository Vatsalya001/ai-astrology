//go:build integration

package db_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// The Phase 2 schema constraints, asserted against real Postgres.
//
// A CHECK constraint is a guard, and the project's rule applies to it
// like any other: one that has never been observed to fire is one you
// cannot trust. Each case below was first written as a shell one-off,
// and two of those one-offs LIED — the ids they interpolated were
// malformed, so every insert failed on a foreign key or a UUID parse
// while reporting "constraint refused it". That is why these live here
// now, where the ids come from the driver rather than from string
// surgery on psql output.

// schemaPool brings up Postgres and returns a writer pool.
//
// startPostgres (singlewriter_test.go) hands back DSNs rather than a
// pool, because its own subject is the GRANTS — it needs to connect as
// two different roles. These tests only need the writer.
func schemaPool(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx := context.Background()

	writerDSN, _, terminate := startPostgres(ctx, t)

	pool, err := pgxpool.New(ctx, writerDSN)
	if err != nil {
		terminate()
		t.Fatalf("connect: %v", err)
	}

	// The container's init script creates the roles and grants — all
	// singlewriter_test.go needs, since GRANTS are its subject. These
	// tests need the tables, so the migrations must run too. Without it
	// every case fails with `relation "users" does not exist`, which
	// reads like a broken test rather than a missing step.
	conn, err := pgx.Connect(ctx, writerDSN)
	if err != nil {
		terminate()
		t.Fatalf("connect for migrations: %v", err)
	}
	applyMigrations(ctx, t, conn)
	_ = conn.Close(ctx)

	return pool, func() {
		pool.Close()
		terminate()
	}
}

func astrologyFixtures(t *testing.T, pool *pgxpool.Pool) (dbgen.Querier, pgtype.UUID) {
	t.Helper()
	ctx := context.Background()
	q := dbgen.New(pool)

	email := "schema-" + t.Name() + "@example.com"
	user, err := q.CreateUserWithEmail(ctx, &email)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return q, user.ID
}

func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) error {
	t.Helper()
	_, err := pool.Exec(context.Background(), sql, args...)
	return err
}

// `unknown` is a first-class state, not a missing value. The constraint
// has to refuse a missing time when the user claimed to know it, and
// ALLOW a missing time when they said they do not — getting only the
// first half right would block the escape hatch that keeps a large
// fraction of Indian users in the funnel.
func TestBirthTimeIsRequiredUnlessItIsUnknown(t *testing.T) {
	pool, stop := schemaPool(t)
	defer stop()

	_, userID := astrologyFixtures(t, pool)

	const insert = `
		INSERT INTO birth_profiles
			(user_id, birth_date, birth_time, time_accuracy, birth_place,
			 latitude, longitude, timezone, utc_offset_min, utc_instant)
		VALUES ($1, '1994-08-17', $2, $3, 'Jaipur', 26.9, 75.8, 'Asia/Kolkata', 330, now())`

	t.Run("exact with no time is refused", func(t *testing.T) {
		err := exec(t, pool, insert, userID, nil, "exact")
		requireConstraint(t, err, "birth_time_present_unless_unknown")
	})

	t.Run("approximate with no time is refused", func(t *testing.T) {
		err := exec(t, pool, insert, userID, nil, "approximate")
		requireConstraint(t, err, "birth_time_present_unless_unknown")
	})

	t.Run("unknown with no time is allowed", func(t *testing.T) {
		if err := exec(t, pool, insert, userID, nil, "unknown"); err != nil {
			t.Fatalf("the unknown-time escape hatch is blocked: %v", err)
		}
	})
}

func TestBirthProfileRejectsImpossibleCoordinates(t *testing.T) {
	pool, stop := schemaPool(t)
	defer stop()

	_, userID := astrologyFixtures(t, pool)

	const insert = `
		INSERT INTO birth_profiles
			(user_id, birth_date, birth_time, time_accuracy, birth_place,
			 latitude, longitude, timezone, utc_offset_min, utc_instant)
		VALUES ($1, '1994-08-17', '14:35', 'exact', 'X', $2, $3, 'Asia/Kolkata', 330, now())`

	for name, c := range map[string]struct{ lat, lon float64 }{
		"latitude above 90":    {95, 75.8},
		"latitude below -90":   {-91, 75.8},
		"longitude above 180":  {26.9, 200},
		"longitude below -180": {26.9, -200},
	} {
		t.Run(name, func(t *testing.T) {
			if err := exec(t, pool, insert, userID, c.lat, c.lon); err == nil {
				t.Fatalf("lat=%v lon=%v was accepted", c.lat, c.lon)
			}
		})
	}

	// The positive case, so this test cannot pass by refusing everything.
	if err := exec(t, pool, insert, userID, 26.9, 75.8); err != nil {
		t.Fatalf("a valid coordinate was refused: %v", err)
	}
}

func TestTimeAccuracyIsAClosedSet(t *testing.T) {
	pool, stop := schemaPool(t)
	defer stop()

	_, userID := astrologyFixtures(t, pool)

	const insert = `
		INSERT INTO birth_profiles
			(user_id, birth_date, birth_time, time_accuracy, birth_place,
			 latitude, longitude, timezone, utc_offset_min, utc_instant)
		VALUES ($1, '1994-08-17', '14:35', $2, 'X', 26.9, 75.8, 'Asia/Kolkata', 330, now())`

	// "guessed" is the word somebody will reach for eventually. The
	// column is a closed vocabulary precisely so that the day they do,
	// it fails here rather than producing a chart whose caveats the UI
	// does not know how to render.
	if err := exec(t, pool, insert, userID, "guessed"); err == nil {
		t.Fatal("an unknown time_accuracy value was accepted")
	}

	for _, valid := range []string{"exact", "approximate", "unknown"} {
		if err := exec(t, pool, insert, userID, valid); err != nil {
			t.Errorf("%q was refused: %v", valid, err)
		}
	}
}

// The dasha constraints exist because of how the arithmetic fails. Float
// accumulation across three levels of subdivision produces periods that
// drift, overlap, or invert — and an inverted period looks entirely
// plausible in a UI. The database refusing it is the backstop for the
// Decimal arithmetic upstream.
func TestDashaTreeConstraints(t *testing.T) {
	pool, stop := schemaPool(t)
	defer stop()

	ctx := context.Background()
	q, userID := astrologyFixtures(t, pool)

	profile, err := q.CreateBirthProfile(ctx, dbgen.CreateBirthProfileParams{
		UserID:       userID,
		Label:        "self",
		BirthDate:    pgtype.Date{Time: time.Date(1994, 8, 17, 0, 0, 0, 0, time.UTC), Valid: true},
		BirthTime:    pgtype.Time{Microseconds: 14*3600*1e6 + 35*60*1e6, Valid: true},
		TimeAccuracy: "exact",
		BirthPlace:   "Jaipur",
		Latitude:     26.9,
		Longitude:    75.8,
		Timezone:     "Asia/Kolkata",
		UtcOffsetMin: 330,
		UtcInstant:   time.Now().UTC(),
		Source:       "user",
		Version:      1,
	})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}

	chart, err := q.UpsertChart(ctx, dbgen.UpsertChartParams{
		BirthProfileID:    profile.ID,
		ChartType:         "D1",
		CalculationSystem: "vedic",
		Ayanamsa:          "lahiri",
		HouseSystem:       "whole_sign",
		EngineVersion:     "test",
		ChartData:         []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create chart: %v", err)
	}

	const insert = `
		INSERT INTO dashas (chart_id, planet, start_date, end_date, level, parent_id)
		VALUES ($1, 'Ketu', $2, $3, $4, $5)`

	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(7, 0, 0)

	t.Run("a period ending before it starts is refused", func(t *testing.T) {
		err := exec(t, pool, insert, chart.ID, end, start, 1, nil)
		requireConstraint(t, err, "dasha_ends_after_it_starts")
	})

	t.Run("a zero-length period is refused", func(t *testing.T) {
		err := exec(t, pool, insert, chart.ID, start, start, 1, nil)
		requireConstraint(t, err, "dasha_ends_after_it_starts")
	})

	t.Run("a Mahadasha with a parent is refused", func(t *testing.T) {
		parent := mustInsertMahadasha(t, pool, chart.ID, start, end)
		err := exec(t, pool, insert, chart.ID, start, end, 1, parent)
		requireConstraint(t, err, "dasha_parent_matches_level")
	})

	t.Run("an Antardasha without a parent is refused", func(t *testing.T) {
		// This is the one that matters most. An orphaned Antardasha is
		// queryable as though it were a Mahadasha, so "which period am I
		// in" silently returns a sub-period as a major one.
		err := exec(t, pool, insert, chart.ID, start, end, 2, nil)
		requireConstraint(t, err, "dasha_parent_matches_level")
	})

	t.Run("a fourth level is refused", func(t *testing.T) {
		parent := mustInsertMahadasha(t, pool, chart.ID, start, end)
		if err := exec(t, pool, insert, chart.ID, start, end, 4, parent); err == nil {
			t.Fatal("level 4 was accepted; the tree is three levels deep")
		}
	})

	t.Run("a well-formed two-level tree is allowed", func(t *testing.T) {
		parent := mustInsertMahadasha(t, pool, chart.ID, start, end)
		if err := exec(t, pool, insert, chart.ID, start, start.AddDate(1, 0, 0), 2, parent); err != nil {
			t.Fatalf("a valid Antardasha was refused: %v", err)
		}
	})
}

func TestTransitDegreeStaysWithinASign(t *testing.T) {
	pool, stop := schemaPool(t)
	defer stop()

	const insert = `
		INSERT INTO transits (planet, sign, degree, timestamp)
		VALUES ('Saturn', 'Pisces', $1, $2)`

	now := time.Now().UTC()

	// A sign is 30 degrees. A degree outside that range means the
	// sign/degree split went wrong upstream, which is the kind of error
	// that renders as a plausible-looking chart.
	for _, bad := range []float64{30, 31, 360, -1} {
		if err := exec(t, pool, insert, bad, now.Add(time.Duration(bad)*time.Hour)); err == nil {
			t.Errorf("degree %v was accepted", bad)
		}
	}

	if err := exec(t, pool, insert, 29.999, now); err != nil {
		t.Fatalf("a valid degree was refused: %v", err)
	}
}

func mustInsertMahadasha(t *testing.T, pool *pgxpool.Pool, chartID pgtype.UUID, start, end time.Time) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO dashas (chart_id, planet, start_date, end_date, level)
		VALUES ($1, 'Ketu', $2, $3, 1) RETURNING id`, chartID, start, end).Scan(&id)
	if err != nil {
		t.Fatalf("insert mahadasha: %v", err)
	}
	return id
}

// requireConstraint asserts that a specific constraint refused the row.
//
// Naming it matters. "The insert failed" is satisfied by a typo, a
// foreign-key violation or a malformed UUID — which is exactly how the
// shell version of these checks reported four passes while testing
// nothing at all.
func requireConstraint(t *testing.T, err error, name string) {
	t.Helper()
	if err == nil {
		t.Fatalf("the row was accepted; %q did not fire", name)
	}
	if !strings.Contains(err.Error(), name) {
		t.Fatalf("refused by something other than %q: %v", name, err)
	}
}
