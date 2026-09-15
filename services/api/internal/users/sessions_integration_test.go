//go:build integration

package users

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// Session revocation against real Postgres.
//
// The property under test is cross-user isolation, which lives entirely
// in the SQL — `WHERE id = $1 AND user_id = $2`. A mock would assert that
// I remembered to write the predicate, not that Postgres honours it.

func startPostgres(ctx context.Context, t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()

	// internal/users → internal → services/api → services → repo root.
	initSQL, err := filepath.Abs(filepath.Join(
		"..", "..", "..", "..", "infrastructure", "docker", "init", "01-init.sql"))
	if err != nil {
		t.Fatalf("resolve init script: %v", err)
	}
	// Fail rather than skip on a missing file. A skip over a wrong path
	// reads as a pass, which is how a whole suite silently stops running.
	if _, statErr := os.Stat(initSQL); statErr != nil {
		t.Fatalf("bootstrap SQL not found at %s: %v", initSQL, statErr)
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "pgvector/pgvector:pg16",
			ExposedPorts: []string{"5432/tcp"},
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
		t.Skipf("could not start Postgres (is Docker running?): %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("mapped port: %v", err)
	}

	pool, err := pgxpool.New(ctx, fmt.Sprintf(
		"postgresql://astro:astro@%s:%s/astro_dev?sslmode=disable", host, port.Port()))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	dir, _ := filepath.Abs(filepath.Join("..", "..", "db", "migrations"))
	files, _ := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if len(files) == 0 {
		t.Fatalf("no migrations found in %s", dir)
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

	return pool, func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	}
}

// Returns the user id and the FAMILY id.
//
// The family is what the device list and Revoke both take — a session id
// would revoke one link in a rotation chain and leave the device signed
// in, which is the bug these tests exist to prevent.
func seedUserWithSession(ctx context.Context, t *testing.T, pool *pgxpool.Pool, email string) (uuid.UUID, uuid.UUID) {
	t.Helper()

	var userID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, email_verified) VALUES ($1, TRUE) RETURNING id`, email,
	).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	var familyID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO sessions (user_id, family_id, refresh_hash, user_agent, expires_at)
		 VALUES ($1, gen_random_uuid(), $2, $3, now() + INTERVAL '30 days')
		 RETURNING family_id`,
		userID, []byte(email+"-hash"), "Test/1.0",
	).Scan(&familyID); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return userID, familyID
}

// The gate item this covers: "Revoking invalidates that device's refresh
// immediately", and the security rule that cross-user access returns 404.
func TestRevokeIsScopedToTheOwner(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	dir := NewSessionDirectory(dbgen.New(pool))

	aliceID, aliceDevice := seedUserWithSession(ctx, t, pool, "alice@example.com")
	bobID, bobDevice := seedUserWithSession(ctx, t, pool, "bob@example.com")

	t.Run("cannot revoke another user's device", func(t *testing.T) {
		err := dir.Revoke(ctx, bobDevice, aliceID)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("got %v, want ErrNotFound — reporting anything else confirms "+
				"the session exists and belongs to someone", err)
		}

		// And it must genuinely still be live.
		sessions, err := dir.ListActive(ctx, bobID)
		if err != nil {
			t.Fatalf("ListActive: %v", err)
		}
		if len(sessions) != 1 {
			t.Errorf("bob has %d live sessions, want 1 — another user revoked it", len(sessions))
		}
	})

	t.Run("can revoke own device", func(t *testing.T) {
		if err := dir.Revoke(ctx, aliceDevice, aliceID); err != nil {
			t.Fatalf("Revoke: %v", err)
		}

		sessions, err := dir.ListActive(ctx, aliceID)
		if err != nil {
			t.Fatalf("ListActive: %v", err)
		}
		if len(sessions) != 0 {
			t.Errorf("alice has %d live sessions after revoking, want 0", len(sessions))
		}
	})

	// This is what the :execrows change bought. With :exec the second
	// call returned success, telling the caller it had revoked something
	// it had not — and a UI would optimistically drop the row.
	t.Run("revoking twice reports not found", func(t *testing.T) {
		if err := dir.Revoke(ctx, aliceDevice, aliceID); !errors.Is(err, ErrNotFound) {
			t.Errorf("second revoke returned %v, want ErrNotFound", err)
		}
	})

	t.Run("unknown device id reports not found", func(t *testing.T) {
		if err := dir.Revoke(ctx, uuid.New(), aliceID); !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})
}

// The device list must never carry the refresh hash. A credential-shaped
// value rendered into a settings page ends up in a screenshot and a
// support ticket.
func TestListActiveExposesNoCredential(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	dir := NewSessionDirectory(dbgen.New(pool))
	userID, _ := seedUserWithSession(ctx, t, pool, "list@example.com")

	sessions, err := dir.ListActive(ctx, userID)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}

	// SessionView is a deliberate projection. If someone widens it to the
	// database row, this is what notices.
	s := sessions[0]
	if s.ID == uuid.Nil {
		t.Error("the session id is missing; the UI could not revoke it")
	}
	if s.UserAgent != "Test/1.0" {
		t.Errorf("UserAgent = %q, want the stored value", s.UserAgent)
	}
	if s.CreatedAt == "" || s.ExpiresAt == "" {
		t.Error("timestamps are missing; the UI cannot show when a device signed in")
	}
}

// Expired and revoked sessions must not appear as devices someone can
// still be signed in on.
func TestListActiveExcludesDeadSessions(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	dir := NewSessionDirectory(dbgen.New(pool))
	userID, liveSession := seedUserWithSession(ctx, t, pool, "filter@example.com")

	// An expired one.
	if _, err := pool.Exec(ctx,
		`INSERT INTO sessions (user_id, family_id, refresh_hash, expires_at)
		 VALUES ($1, gen_random_uuid(), $2, now() - INTERVAL '1 day')`,
		userID, []byte("expired-hash"),
	); err != nil {
		t.Fatalf("seed expired: %v", err)
	}

	// A revoked one.
	if _, err := pool.Exec(ctx,
		`INSERT INTO sessions (user_id, family_id, refresh_hash, expires_at, revoked_at)
		 VALUES ($1, gen_random_uuid(), $2, now() + INTERVAL '30 days', now())`,
		userID, []byte("revoked-hash"),
	); err != nil {
		t.Fatalf("seed revoked: %v", err)
	}

	sessions, err := dir.ListActive(ctx, userID)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want only the live one", len(sessions))
	}
	if sessions[0].ID != liveSession {
		t.Errorf("the wrong session was returned")
	}
}

func TestRevokeAllEndsEverySession(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	dir := NewSessionDirectory(dbgen.New(pool))
	userID, _ := seedUserWithSession(ctx, t, pool, "everywhere@example.com")

	for i := range 3 {
		if _, err := pool.Exec(ctx,
			`INSERT INTO sessions (user_id, family_id, refresh_hash, expires_at)
			 VALUES ($1, gen_random_uuid(), $2, now() + INTERVAL '30 days')`,
			userID, []byte(fmt.Sprintf("device-%d", i)),
		); err != nil {
			t.Fatalf("seed device %d: %v", i, err)
		}
	}

	otherID, _ := seedUserWithSession(ctx, t, pool, "other@example.com")

	if err := dir.RevokeAll(ctx, userID); err != nil {
		t.Fatalf("RevokeAll: %v", err)
	}

	sessions, err := dir.ListActive(ctx, userID)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("%d sessions survived RevokeAll", len(sessions))
	}

	// And nobody else was signed out.
	others, err := dir.ListActive(ctx, otherID)
	if err != nil {
		t.Fatalf("ListActive for the other user: %v", err)
	}
	if len(others) != 1 {
		t.Errorf("RevokeAll affected another user: they have %d sessions, want 1", len(others))
	}
}

// A device is a FAMILY, not a session row.
//
// Every refresh issues a new row sharing its predecessor's family_id, so
// a browser left open for an hour accumulates a dozen. Listing those as
// devices shows the user sign-ins they do not recognise — this test
// caught exactly that: one signup, three rows, one device.
func TestDeviceListCollapsesARotationChain(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	dir := NewSessionDirectory(dbgen.New(pool))
	userID, _ := seedUserWithSession(ctx, t, pool, "rotating@example.com")

	// The family the fixture created.
	var family uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT family_id FROM sessions WHERE user_id = $1`, userID,
	).Scan(&family); err != nil {
		t.Fatalf("read family: %v", err)
	}

	// Four more rotations of the SAME device.
	for i := range 4 {
		if _, err := pool.Exec(ctx,
			`INSERT INTO sessions (user_id, family_id, refresh_hash, user_agent, expires_at)
			 VALUES ($1, $2, $3, 'Test/1.0', now() + INTERVAL '30 days')`,
			userID, family, []byte(fmt.Sprintf("rotation-%d", i)),
		); err != nil {
			t.Fatalf("seed rotation %d: %v", i, err)
		}
	}

	// A genuinely different device.
	if _, err := pool.Exec(ctx,
		`INSERT INTO sessions (user_id, family_id, refresh_hash, user_agent, expires_at)
		 VALUES ($1, gen_random_uuid(), $2, 'Other/2.0', now() + INTERVAL '30 days')`,
		userID, []byte("other-device"),
	); err != nil {
		t.Fatalf("seed other device: %v", err)
	}

	devices, err := dir.ListActive(ctx, userID)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}

	// Six rows, two devices.
	if len(devices) != 2 {
		t.Fatalf("listed %d devices from 6 session rows, want 2 — the list is "+
			"showing rotations, not devices", len(devices))
	}
}

// Signing a device out must end its whole lineage. Revoking one row
// leaves the device's CURRENT token working, which is the opposite of
// what the button says.
func TestRevokingADeviceEndsItsWholeFamily(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	dir := NewSessionDirectory(dbgen.New(pool))
	userID, _ := seedUserWithSession(ctx, t, pool, "familyrevoke@example.com")

	var family uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT family_id FROM sessions WHERE user_id = $1`, userID,
	).Scan(&family); err != nil {
		t.Fatalf("read family: %v", err)
	}

	for i := range 3 {
		if _, err := pool.Exec(ctx,
			`INSERT INTO sessions (user_id, family_id, refresh_hash, expires_at)
			 VALUES ($1, $2, $3, now() + INTERVAL '30 days')`,
			userID, family, []byte(fmt.Sprintf("chain-%d", i)),
		); err != nil {
			t.Fatalf("seed chain %d: %v", i, err)
		}
	}

	if err := dir.Revoke(ctx, family, userID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	var live int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE family_id = $1 AND revoked_at IS NULL`, family,
	).Scan(&live); err != nil {
		t.Fatalf("count: %v", err)
	}
	if live != 0 {
		t.Errorf("%d sessions in the family are still live — the device's current "+
			"token still works after the user signed it out", live)
	}
}
