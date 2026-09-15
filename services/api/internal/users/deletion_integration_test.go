//go:build integration

package users

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// seedFullUser creates a user with a row in every user-owned table, so a
// deletion test has something to fail to remove.
func seedFullUser(ctx context.Context, t *testing.T, pool *pgxpool.Pool, email string) uuid.UUID {
	t.Helper()

	var userID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, email_verified, name) VALUES ($1, TRUE, 'Test Person') RETURNING id`,
		email,
	).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	for _, stmt := range []struct {
		what string
		sql  string
		args []any
	}{
		{"preferences",
			`INSERT INTO user_preferences (user_id) VALUES ($1)`,
			[]any{userID}},
		{"identity",
			`INSERT INTO auth_identities (user_id, provider, provider_user_id) VALUES ($1, 'email', $2)`,
			[]any{userID, email}},
		{"session",
			`INSERT INTO sessions (user_id, family_id, refresh_hash, expires_at)
			 VALUES ($1, gen_random_uuid(), $2, now() + INTERVAL '30 days')`,
			[]any{userID, []byte(email + "-session")}},
		{"audit",
			`INSERT INTO audit_logs (user_id, action, metadata) VALUES ($1, 'auth.login', '{"channel":"email"}')`,
			[]any{userID}},
		// Phase 2. The spec makes this a standing obligation: "every
		// phase that adds a user-owned table extends the deletion
		// integration test". userOwnedTables discovers birth_profiles on
		// its own and fails if it is not seeded — which is how this
		// requirement got enforced rather than remembered.
		{"birth profile",
			`INSERT INTO birth_profiles
				(user_id, birth_date, birth_time, time_accuracy, birth_place,
				 latitude, longitude, timezone, utc_offset_min, utc_instant)
			 VALUES ($1, '1994-08-17', '14:35', 'exact', 'Jaipur',
			         26.9124, 75.7873, 'Asia/Kolkata', 330, '1994-08-17T09:05:00Z')`,
			[]any{userID}},
	} {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed %s: %v", stmt.what, err)
		}
	}

	// charts and dashas hang off the birth profile rather than the user,
	// so userOwnedTables cannot discover them — there is no user_id
	// column to find. They are seeded and asserted explicitly, because a
	// cascade that stops one level short leaves a chart of a deleted
	// person's sky behind, which is exactly the residue the Phase 1 gate
	// exists to forbid.
	seedChartAndDashas(ctx, t, pool, userID)

	return userID
}

// seedChartAndDashas hangs a chart and a two-level dasha tree off the
// user's birth profile.
func seedChartAndDashas(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) {
	t.Helper()

	var profileID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM birth_profiles WHERE user_id = $1 LIMIT 1`, userID,
	).Scan(&profileID); err != nil {
		t.Fatalf("find seeded birth profile: %v", err)
	}

	var chartID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO charts (birth_profile_id, chart_type, engine_version, chart_data)
		 VALUES ($1, 'D1', 'test-engine', '{"schema_version":1}') RETURNING id`,
		profileID,
	).Scan(&chartID); err != nil {
		t.Fatalf("seed chart: %v", err)
	}

	var mahaID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO dashas (chart_id, planet, start_date, end_date, level)
		 VALUES ($1, 'Ketu', '2020-01-01T00:00:00Z', '2027-01-01T00:00:00Z', 1) RETURNING id`,
		chartID,
	).Scan(&mahaID); err != nil {
		t.Fatalf("seed mahadasha: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO dashas (chart_id, planet, start_date, end_date, level, parent_id)
		 VALUES ($1, 'Venus', '2020-01-01T00:00:00Z', '2021-01-01T00:00:00Z', 2, $2)`,
		chartID, mahaID,
	); err != nil {
		t.Fatalf("seed antardasha: %v", err)
	}
}

// userOwnedTables discovers every table with a user_id column.
//
// Discovered, not listed — the same reasoning as the single-writer test.
// Phase 2 adds birth profiles, Phase 5 adds conversations, and an
// enumerated list in this file stops being complete the moment someone
// forgets it. "No residue" has to mean no residue anywhere, including in
// tables that did not exist when this was written.
func userOwnedTables(ctx context.Context, t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()

	rows, err := pool.Query(ctx, `
		SELECT table_name FROM information_schema.columns
		WHERE table_schema = 'public' AND column_name = 'user_id'
		ORDER BY table_name`)
	if err != nil {
		t.Fatalf("discover user-owned tables: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		tables = append(tables, name)
	}
	return tables
}

// The gate item: "Account deletion + hard-delete worker verified to
// leave no residue."
func TestHardDeleteLeavesNoResidue(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	q := dbgen.New(pool)
	dir := NewSessionDirectory(q)
	deleter := NewDeleter(q, dir, 168*time.Hour, nil)

	userID := seedFullUser(ctx, t, pool, "doomed@example.com")
	survivor := seedFullUser(ctx, t, pool, "survivor@example.com")

	tables := userOwnedTables(ctx, t, pool)
	if len(tables) < 4 {
		t.Fatalf("discovered only %v — the schema does not look applied, so this "+
			"test would pass without checking anything", tables)
	}
	t.Logf("checking %d user-owned tables: %v", len(tables), tables)

	// Every table must actually have a row to begin with, or "no rows
	// afterwards" proves nothing.
	for _, table := range tables {
		var count int
		if err := pool.QueryRow(ctx,
			fmt.Sprintf(`SELECT count(*) FROM %q WHERE user_id = $1`, table), userID,
		).Scan(&count); err != nil {
			t.Fatalf("count %s before: %v", table, err)
		}
		if count == 0 {
			t.Fatalf("seed did not create a row in %q — deleting it would prove nothing. "+
				"A new user-owned table needs seeding here.", table)
		}
	}

	// Request, then run the worker with the grace window already past.
	if _, err := deleter.Request(ctx, userID); err != nil {
		t.Fatalf("Request: %v", err)
	}
	deleter.now = func() time.Time { return time.Now().Add(200 * time.Hour) }

	deleted, err := deleter.RunHardDeletes(ctx)
	if err != nil {
		t.Fatalf("RunHardDeletes: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted %d accounts, want 1", deleted)
	}

	// The assertion that matters.
	for _, table := range tables {
		var count int
		if err := pool.QueryRow(ctx,
			fmt.Sprintf(`SELECT count(*) FROM %q WHERE user_id = $1`, table), userID,
		).Scan(&count); err != nil {
			t.Fatalf("count %s after: %v", table, err)
		}

		// audit_logs is the one deliberate exception: its user_id is not a
		// foreign key, so the record that a deletion happened outlives the
		// account. It holds an ID and an action, never PII.
		if table == "audit_logs" {
			if count == 0 {
				t.Errorf("the audit trail was erased along with the account — " +
					"the record that a deletion happened must survive it")
			}
			continue
		}

		if count != 0 {
			t.Errorf("%d rows remain in %q after hard deletion — this is residue, "+
				"and the gate says there must be none", count, table)
		}
	}

	// charts and dashas have no user_id, so the discovery loop above
	// cannot see them. They reach the user through
	// birth_profiles → charts → dashas, and a cascade that stops one
	// level short leaves a chart of a deleted person's sky behind —
	// which is residue by any reading of the gate.
	//
	// Counted globally rather than by user, because the survivor account
	// seeds its own rows: a global count of zero would be satisfied by a
	// cascade that deleted everybody's. So the assertion is that the
	// SURVIVOR's rows remain and the doomed user's are gone, which only
	// a correctly scoped cascade satisfies.
	for _, c := range []struct {
		table string
		sql   string
	}{
		{"charts", `SELECT count(*) FROM charts c
		            JOIN birth_profiles p ON p.id = c.birth_profile_id
		            WHERE p.user_id = $1`},
		{"dashas", `SELECT count(*) FROM dashas d
		            JOIN charts c ON c.id = d.chart_id
		            JOIN birth_profiles p ON p.id = c.birth_profile_id
		            WHERE p.user_id = $1`},
	} {
		var gone int
		if err := pool.QueryRow(ctx, c.sql, userID).Scan(&gone); err != nil {
			t.Fatalf("count %s after: %v", c.table, err)
		}
		if gone != 0 {
			t.Errorf("%d rows remain in %q after hard deletion — the cascade "+
				"stopped short of the chart data", gone, c.table)
		}

		var survived int
		if err := pool.QueryRow(ctx, c.sql, survivor).Scan(&survived); err != nil {
			t.Fatalf("count %s for survivor: %v", c.table, err)
		}
		if survived == 0 {
			t.Errorf("deleting one account removed another account's %s", c.table)
		}
	}

	// The users row itself.
	var userRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1`, userID).Scan(&userRows); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if userRows != 0 {
		t.Error("the users row survived; the account was flagged, not deleted")
	}

	// And nobody else was touched.
	for _, table := range tables {
		var count int
		if err := pool.QueryRow(ctx,
			fmt.Sprintf(`SELECT count(*) FROM %q WHERE user_id = $1`, table), survivor,
		).Scan(&count); err != nil {
			t.Fatalf("count survivor in %s: %v", table, err)
		}
		if count == 0 {
			t.Errorf("deleting one account removed another user's rows from %q", table)
		}
	}
}

// The grace window is the promise. Running the worker early must delete
// nothing, or "cancellable for 7 days" is not true.
func TestWorkerRespectsTheGraceWindow(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	q := dbgen.New(pool)
	deleter := NewDeleter(q, NewSessionDirectory(q), 168*time.Hour, nil)

	userID := seedFullUser(ctx, t, pool, "waiting@example.com")
	if _, err := deleter.Request(ctx, userID); err != nil {
		t.Fatalf("Request: %v", err)
	}

	// Six days in — still inside a seven-day window.
	deleter.now = func() time.Time { return time.Now().Add(144 * time.Hour) }

	deleted, err := deleter.RunHardDeletes(ctx)
	if err != nil {
		t.Fatalf("RunHardDeletes: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("deleted %d accounts before the grace window closed", deleted)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1`, userID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Error("the account was removed before its grace window closed")
	}
}

// Requesting deletion signs the user out everywhere immediately. They
// asked to be gone; leaving them signed in on four devices for a week is
// not what they asked for.
func TestRequestingDeletionRevokesEverySession(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	q := dbgen.New(pool)
	dir := NewSessionDirectory(q)
	deleter := NewDeleter(q, dir, 168*time.Hour, nil)

	userID := seedFullUser(ctx, t, pool, "signedout@example.com")

	before, err := dir.ListActive(ctx, userID)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("the fixture has no active session, so this proves nothing")
	}

	if _, err := deleter.Request(ctx, userID); err != nil {
		t.Fatalf("Request: %v", err)
	}

	after, err := dir.ListActive(ctx, userID)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("%d sessions still active after a deletion request", len(after))
	}
}

func TestDeletionCanBeCancelledWithinTheWindow(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	q := dbgen.New(pool)
	deleter := NewDeleter(q, NewSessionDirectory(q), 168*time.Hour, nil)

	userID := seedFullUser(ctx, t, pool, "changedmind@example.com")
	if _, err := deleter.Request(ctx, userID); err != nil {
		t.Fatalf("Request: %v", err)
	}

	if err := deleter.Cancel(ctx, userID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	// Cancelling must restore the account, not leave it in limbo.
	var status string
	var pending *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT status, deletion_requested_at FROM users WHERE id = $1`, userID,
	).Scan(&status, &pending); err != nil {
		t.Fatalf("read user: %v", err)
	}
	if status != "active" {
		t.Errorf("status = %q after cancelling, want active", status)
	}
	if pending != nil {
		t.Error("deletion_requested_at is still set after cancelling")
	}

	// And the worker must not pick it up afterwards.
	deleter.now = func() time.Time { return time.Now().Add(200 * time.Hour) }
	deleted, err := deleter.RunHardDeletes(ctx)
	if err != nil {
		t.Fatalf("RunHardDeletes: %v", err)
	}
	if deleted != 0 {
		t.Error("the worker deleted an account whose deletion had been cancelled")
	}
}

func TestCancellingWithNothingPendingIsAnError(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	q := dbgen.New(pool)
	deleter := NewDeleter(q, NewSessionDirectory(q), 168*time.Hour, nil)

	userID := seedFullUser(ctx, t, pool, "nothing@example.com")
	if err := deleter.Cancel(ctx, userID); !errors.Is(err, ErrNoDeletionPending) {
		t.Errorf("got %v, want ErrNoDeletionPending", err)
	}
}

// ─── export ──────────────────────────────────────────────────────────

// The gate item: "Data export returns complete user data."
//
// Completeness is the point: an export that quietly omits a table is
// worse than none, because it tells the user they have seen everything.
func TestExportIsComplete(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	q := dbgen.New(pool)
	exporter := NewExporter(q)

	userID := seedFullUser(ctx, t, pool, "exportme@example.com")
	other := seedFullUser(ctx, t, pool, "notyours@example.com")

	export, err := exporter.Export(ctx, userID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	if export.Profile.ID != userID {
		t.Errorf("the export is for the wrong user")
	}
	if export.Profile.Email == nil || *export.Profile.Email != "exportme@example.com" {
		t.Error("the profile email is missing from the export")
	}
	if export.Preferences.PreferredLanguage == "" {
		t.Error("preferences are missing from the export")
	}
	if len(export.Identities) == 0 {
		t.Error("auth_identities are missing — the field would silently return [] " +
			"and tell the user there are none")
	}
	if len(export.Sessions) == 0 {
		t.Error("sessions are missing from the export")
	}
	if len(export.AuditLog) == 0 {
		t.Error("the audit log is missing from the export")
	}
	if export.Format == "" {
		t.Error("the export carries no format version")
	}

	// Nobody else's data may appear.
	for _, id := range export.Identities {
		if id.ProviderUserID == "notyours@example.com" {
			t.Error("the export contains another user's identity")
		}
	}
	_ = other
}

// The export is the user's own data, but it must not be a copy of a live
// credential. refresh_hash is not "data about you" in any useful sense.
func TestExportCarriesNoCredentials(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	exporter := NewExporter(dbgen.New(pool))
	userID := seedFullUser(ctx, t, pool, "nocreds@example.com")

	export, err := exporter.Export(ctx, userID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	raw, err := jsonMarshal(export)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, forbidden := range []string{"refresh_hash", "nocreds@example.com-session"} {
		if containsString(raw, forbidden) {
			t.Errorf("the export contains %q — a downloadable file must not carry a credential", forbidden)
		}
	}
}

func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func containsString(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
