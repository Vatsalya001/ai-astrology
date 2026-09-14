//go:build integration

package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// Rotation against real Postgres.
//
// rotation_test.go covers the state machine with a fake whose Rotate
// holds a mutex — that proves the Go logic, and nothing about the SQL. The
// safety property actually lives in
//
//	UPDATE sessions SET used_at = now()
//	WHERE refresh_hash = $1 AND used_at IS NULL ... RETURNING *
//
// and whether Postgres serialises that is not something a fake can
// answer. A mocked database would test my assumptions rather than the
// database's behaviour, which is the failure `.claude/rules/testing.md`
// names explicitly.

func startPostgresWithSchema(ctx context.Context, t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()

	initSQL, err := filepath.Abs(filepath.Join(
		"..", "..", "..", "..", "infrastructure", "docker", "init", "01-init.sql"))
	if err != nil {
		t.Fatalf("resolve init script: %v", err)
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
				WithOccurrence(2).
				WithStartupTimeout(90 * time.Second),
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

	dsn := fmt.Sprintf("postgresql://astro:astro@%s:%s/astro_dev?sslmode=disable",
		host, port.Port())

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect pool: %v", err)
	}

	// Apply the real migrations rather than a copy of the schema, so this
	// test fails if a migration and the queries drift apart.
	dir, err := filepath.Abs(filepath.Join("..", "..", "db", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("glob migrations in %s: %v", dir, err)
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

// seedUser inserts a row to satisfy sessions.user_id's foreign key.
func seedUser(ctx context.Context, t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(ctx,
		`INSERT INTO users (phone, phone_verified) VALUES ($1, TRUE) RETURNING id`,
		fmt.Sprintf("+9199%08d", time.Now().UnixNano()%100000000),
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

func pgRotator(t *testing.T, pool *pgxpool.Pool) *Rotator {
	t.Helper()
	return NewRotator(testIssuer(t), NewPostgresSessionStore(dbgen.New(pool)))
}

func TestPostgresRotationRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgresWithSchema(ctx, t)
	defer stop()

	userID := seedUser(ctx, t, pool)
	rot := pgRotator(t, pool)

	first, err := rot.Issue(ctx, userID, "user", "Mozilla/5.0", HashIP("203.0.113.7", "salt"))
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	second, err := rot.Rotate(ctx, first.RefreshToken, "user", "Mozilla/5.0", nil)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if second.FamilyID != first.FamilyID {
		t.Errorf("family changed: %s → %s", first.FamilyID, second.FamilyID)
	}

	// The plaintext token must never reach the database.
	var stored int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE encode(refresh_hash, 'escape') LIKE '%' || $1 || '%'`,
		first.RefreshToken,
	).Scan(&stored); err != nil {
		t.Fatalf("scan for plaintext: %v", err)
	}
	if stored != 0 {
		t.Error("a plaintext refresh token is stored in the database")
	}

	// And the IP must be hashed, not raw.
	var rawIPs int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE encode(ip_hash, 'escape') LIKE '%203.0.113.7%'`,
	).Scan(&rawIPs); err != nil {
		t.Fatalf("scan for raw ip: %v", err)
	}
	if rawIPs != 0 {
		t.Error("a raw IP address is stored in the database")
	}
}

// The gate item: "Refresh reuse revokes the token family — integration
// test green."
func TestPostgresReuseRevokesTheFamily(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgresWithSchema(ctx, t)
	defer stop()

	userID := seedUser(ctx, t, pool)
	rot := pgRotator(t, pool)

	first, err := rot.Issue(ctx, userID, "user", "ua", nil)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	second, err := rot.Rotate(ctx, first.RefreshToken, "user", "ua", nil)
	if err != nil {
		t.Fatalf("first rotation: %v", err)
	}

	// The attacker replays the spent token.
	if _, err := rot.Rotate(ctx, first.RefreshToken, "user", "attacker", nil); !errors.Is(err, ErrRefreshReused) {
		t.Fatalf("reuse returned %v, want ErrRefreshReused", err)
	}

	// Every session in the lineage must now be revoked in the database —
	// not just the leaked one.
	var live int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE family_id = $1 AND revoked_at IS NULL`,
		first.FamilyID,
	).Scan(&live); err != nil {
		t.Fatalf("count live sessions: %v", err)
	}
	if live != 0 {
		t.Errorf("%d sessions in the family are still live after a reuse was detected", live)
	}

	// The consequence that matters: the legitimate user's current token
	// is dead too, forcing re-authentication.
	if _, err := rot.Rotate(ctx, second.RefreshToken, "user", "ua", nil); err == nil {
		t.Error("the legitimate current token still works after reuse was detected")
	}
}

// The gate item: "Concurrent refresh handled correctly under -race."
//
// This is the test the whole design exists for. Many goroutines present
// the SAME token against real Postgres. Exactly one UPDATE may match
// `used_at IS NULL`; every other caller must be told the token was
// reused.
//
// If the query were a read-then-write, several would observe an unused
// token and several would win — a leaked refresh token usable repeatedly,
// with no detection.
func TestPostgresConcurrentRotationYieldsExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgresWithSchema(ctx, t)
	defer stop()

	userID := seedUser(ctx, t, pool)
	rot := pgRotator(t, pool)

	pair, err := rot.Issue(ctx, userID, "user", "ua", nil)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	const goroutines = 32
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		wins    int
		reuses  int
		invalid int
		failed  []error
		release = make(chan struct{})
	)

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-release
			_, err := rot.Rotate(ctx, pair.RefreshToken, "user", "ua", nil)

			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.Is(err, ErrRefreshReused):
				reuses++
			case errors.Is(err, ErrRefreshInvalid):
				invalid++
			default:
				failed = append(failed, err)
			}
		}()
	}
	close(release)
	wg.Wait()

	if wins != 1 {
		t.Fatalf("%d of %d concurrent rotations succeeded against real Postgres; "+
			"exactly 1 must — the UPDATE is not atomic", wins, goroutines)
	}
	if len(failed) > 0 {
		t.Fatalf("unexpected errors during concurrent rotation: %v", failed)
	}
	if wins+reuses+invalid != goroutines {
		t.Fatalf("accounting lost a result: %d+%d+%d != %d", wins, reuses, invalid, goroutines)
	}

	t.Logf("1 winner, %d detected as reuse, %d invalid", reuses, invalid)

	// Losing the race means presenting a spent token, which is by
	// definition reuse — so the family must have been revoked. This is
	// arguably harsh on an honest client with two tabs open, and it is
	// the correct trade: the alternative is being unable to distinguish
	// two tabs from a stolen token.
	if reuses > 0 {
		var live int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM sessions WHERE family_id = $1 AND revoked_at IS NULL`,
			pair.FamilyID,
		).Scan(&live); err != nil {
			t.Fatalf("count live sessions: %v", err)
		}
		if live != 0 {
			t.Errorf("%d sessions still live after concurrent reuse was detected", live)
		}
	}
}

func TestPostgresExpiredTokenCannotRotate(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgresWithSchema(ctx, t)
	defer stop()

	userID := seedUser(ctx, t, pool)
	rot := pgRotator(t, pool)

	pair, err := rot.Issue(ctx, userID, "user", "ua", nil)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Age the row rather than waiting 30 days. The expiry check lives in
	// the SQL, so this exercises the real predicate.
	if _, err := pool.Exec(ctx,
		`UPDATE sessions SET expires_at = now() - INTERVAL '1 hour' WHERE family_id = $1`,
		pair.FamilyID,
	); err != nil {
		t.Fatalf("age the session: %v", err)
	}

	if _, err := rot.Rotate(ctx, pair.RefreshToken, "user", "ua", nil); !errors.Is(err, ErrRefreshInvalid) {
		t.Errorf("expired token returned %v, want ErrRefreshInvalid", err)
	}

	// Expiry is routine. It must not be mistaken for a leak and revoke
	// every other device.
	var revoked int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE family_id = $1 AND revoked_at IS NOT NULL`,
		pair.FamilyID,
	).Scan(&revoked); err != nil {
		t.Fatalf("count revoked: %v", err)
	}
	if revoked != 0 {
		t.Error("an expired token triggered a family revocation")
	}
}

// The UNIQUE index on refresh_hash is what guarantees the rotation UPDATE
// touches at most one row. Without it the atomicity argument collapses,
// so the constraint is asserted rather than assumed.
func TestRefreshHashIsUnique(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgresWithSchema(ctx, t)
	defer stop()

	userID := seedUser(ctx, t, pool)
	hash := HashRefreshToken("a-fixed-token-for-this-test")

	insert := func() error {
		_, err := pool.Exec(ctx,
			`INSERT INTO sessions (user_id, family_id, refresh_hash, expires_at)
			 VALUES ($1, gen_random_uuid(), $2, now() + INTERVAL '1 day')`,
			userID, hash)
		return err
	}

	if err := insert(); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := insert(); err == nil {
		t.Fatal("a duplicate refresh_hash was accepted — sessions_hash_idx is not UNIQUE, " +
			"so the rotation UPDATE could match multiple rows")
	}
}
