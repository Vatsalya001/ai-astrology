//go:build integration

// Package db integration tests.
//
// Run with:  go test ./... -race -tags=integration
//
// Behind a build tag because these need Docker. The unit suite must stay
// runnable without it.
package db_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/testsupport"
)

// Postgres error code for insufficient_privilege.
// https://www.postgresql.org/docs/current/errcodes-appendix.html
const errInsufficientPrivilege = "42501"

// TestSingleWriterRuleIsEnforcedByPostgres is the machine-readable form
// of invariant #2.
//
// Only api-service may write. ai-service connects as astro_ro and must
// be able to read and nothing else. This is enforced by Postgres grants
// rather than by code review, because a grant cannot be forgotten during
// a refactor — but a grant CAN be accidentally widened by a future
// migration, which is what this test exists to catch.
func TestSingleWriterRuleIsEnforcedByPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	writerDSN, readerDSN, terminate := startPostgres(ctx, t)
	defer terminate()

	writer, err := pgx.Connect(ctx, writerDSN)
	if err != nil {
		t.Fatalf("connect as writer: %v", err)
	}
	defer writer.Close(ctx)

	reader, err := pgx.Connect(ctx, readerDSN)
	if err != nil {
		t.Fatalf("connect as astro_ro: %v", err)
	}
	defer reader.Close(ctx)

	// The writer creates a table and a row. Default privileges should
	// grant astro_ro SELECT on it automatically.
	mustExec(ctx, t, writer, `CREATE TABLE probe (id int PRIMARY KEY, note text)`)
	mustExec(ctx, t, writer, `INSERT INTO probe VALUES (1, 'written by api-service')`)

	t.Run("reader can SELECT", func(t *testing.T) {
		var note string
		err := reader.QueryRow(ctx, `SELECT note FROM probe WHERE id = 1`).Scan(&note)
		if err != nil {
			t.Fatalf("astro_ro could not read, but it must be able to: %v", err)
		}
		if note != "written by api-service" {
			t.Errorf("note = %q, unexpected value", note)
		}
	})

	// Every mutating statement must be refused. Testing only INSERT
	// would miss a migration that granted UPDATE or DELETE.
	mutations := map[string]string{
		"INSERT":   `INSERT INTO probe VALUES (2, 'should not happen')`,
		"UPDATE":   `UPDATE probe SET note = 'tampered' WHERE id = 1`,
		"DELETE":   `DELETE FROM probe WHERE id = 1`,
		"TRUNCATE": `TRUNCATE probe`,
	}

	for name, sql := range mutations {
		t.Run("reader cannot "+name, func(t *testing.T) {
			_, err := reader.Exec(ctx, sql)
			if err == nil {
				t.Fatalf("astro_ro executed %s successfully — the single-writer "+
					"rule is BROKEN. Check the grants in "+
					"infrastructure/docker/init/01-init.sql", name)
			}

			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("%s failed with a non-Postgres error: %v", name, err)
			}
			if pgErr.Code != errInsufficientPrivilege {
				t.Errorf("%s failed with SQLSTATE %s (%s); expected %s "+
					"(insufficient_privilege) — it may be failing for the "+
					"wrong reason",
					name, pgErr.Code, pgErr.Message, errInsufficientPrivilege)
			}
		})
	}

	t.Run("reader cannot CREATE TABLE", func(t *testing.T) {
		if _, err := reader.Exec(ctx, `CREATE TABLE sneaky (id int)`); err == nil {
			t.Fatal("astro_ro created a table — it has schema privileges it must not have")
		}
	})

	// The row must be exactly as the writer left it.
	t.Run("data is unchanged after all attempts", func(t *testing.T) {
		var count int
		if err := writer.QueryRow(ctx, `SELECT count(*) FROM probe`).Scan(&count); err != nil {
			t.Fatalf("count: %v", err)
		}
		if count != 1 {
			t.Errorf("probe has %d rows, want 1 — a mutation got through", count)
		}
	})
}

// TestExtensionsAreInstalled guards the Phase 5 and Phase 2 dependencies.
func TestExtensionsAreInstalled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	writerDSN, _, terminate := startPostgres(ctx, t)
	defer terminate()

	conn, err := pgx.Connect(ctx, writerDSN)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	for _, ext := range []string{"vector", "pg_trgm"} {
		var present bool
		err := conn.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = $1)`, ext,
		).Scan(&present)
		if err != nil {
			t.Fatalf("query pg_extension: %v", err)
		}
		if !present {
			t.Errorf("extension %q is not installed", ext)
		}
	}
}

// startPostgres boots a pgvector container with the real bootstrap SQL
// applied, and returns writer and reader DSNs.
//
// It uses the actual init script rather than a copy, so this test fails
// if someone edits that file and weakens the grants.
func startPostgres(ctx context.Context, t *testing.T) (writerDSN, readerDSN string, terminate func()) {
	t.Helper()

	initSQL, err := filepath.Abs(
		filepath.Join("..", "..", "..", "..", "..",
			"infrastructure", "docker", "init", "01-init.sql"))
	if err != nil {
		t.Fatalf("resolve init script path: %v", err)
	}
	if _, err := os.Stat(initSQL); err != nil {
		t.Fatalf("bootstrap SQL not found at %s: %v", initSQL, err)
	}

	req := testcontainers.ContainerRequest{
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
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
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

	dsn := func(user, pass string) string {
		return fmt.Sprintf("postgresql://%s:%s@%s:%s/astro_dev?sslmode=disable",
			user, pass, host, port.Port())
	}

	return dsn("astro", "astro"), dsn("astro_ro", "astro_ro"), func() {
		_ = container.Terminate(context.Background())
	}
}

func mustExec(ctx context.Context, t *testing.T, conn *pgx.Conn, sql string) {
	t.Helper()
	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// TestEveryRealTableRefusesWritesFromReader applies the actual migrations
// and then discovers the tables rather than listing them.
//
// The test above proves default privileges work for a newly created
// table. It does not prove they were applied to the tables this product
// actually has — and an enumerated list is exactly the kind of thing that
// stops being complete the moment someone adds a table and forgets this
// file. Phase 1 adds five tables; Phase 2 adds more.
//
// Discovering from information_schema means a table added later is
// covered without anyone remembering to come back here. That is the
// difference between an invariant and a habit.
func TestEveryRealTableRefusesWritesFromReader(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	writerDSN, readerDSN, terminate := startPostgres(ctx, t)
	defer terminate()

	writer, err := pgx.Connect(ctx, writerDSN)
	if err != nil {
		t.Fatalf("connect as writer: %v", err)
	}
	defer writer.Close(ctx)

	applyMigrations(ctx, t, writer)

	reader, err := pgx.Connect(ctx, readerDSN)
	if err != nil {
		t.Fatalf("connect as astro_ro: %v", err)
	}
	defer reader.Close(ctx)

	rows, err := writer.Query(ctx, `
		SELECT tablename FROM pg_tables
		WHERE schemaname = 'public'
		ORDER BY tablename`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		tables = append(tables, name)
	}
	rows.Close()

	// If discovery returns nothing the test would vacuously pass, which is
	// worse than failing: it would report the invariant as holding while
	// checking nothing at all.
	if len(tables) < 5 {
		t.Fatalf("found only %d tables (%v) — migrations do not appear to have "+
			"applied, so this test proves nothing", len(tables), tables)
	}
	t.Logf("checking %d tables: %v", len(tables), tables)

	for _, table := range tables {
		t.Run(table, func(t *testing.T) {
			// DELETE and UPDATE with an always-false predicate: the
			// privilege check happens before any row is examined, so this
			// asserts the grant without depending on table contents or
			// column names.
			for name, sql := range map[string]string{
				"UPDATE": fmt.Sprintf(`UPDATE %q SET id = id WHERE false`, table),
				"DELETE": fmt.Sprintf(`DELETE FROM %q WHERE false`, table),
			} {
				_, err := reader.Exec(ctx, sql)
				if err == nil {
					t.Fatalf("astro_ro executed %s on %q — the single-writer rule "+
						"is BROKEN for this table. Check the GRANTs in its migration.",
						name, table)
				}

				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) {
					t.Fatalf("%s on %q failed with a non-Postgres error: %v", name, table, err)
				}
				if pgErr.Code != errInsufficientPrivilege {
					t.Errorf("%s on %q failed with SQLSTATE %s (%s), want %s — "+
						"it may be failing for the wrong reason, which would mean "+
						"the grant is untested",
						name, table, pgErr.Code, pgErr.Message, errInsufficientPrivilege)
				}
			}
		})
	}

	// The reader must still be able to read every one of them, or the
	// grants have been tightened past usefulness rather than loosened.
	t.Run("reader can still SELECT from every table", func(t *testing.T) {
		for _, table := range tables {
			if _, err := reader.Exec(ctx, fmt.Sprintf(`SELECT 1 FROM %q LIMIT 1`, table)); err != nil {
				t.Errorf("astro_ro cannot read %q, but ai-service needs to: %v", table, err)
			}
		}
	})
}

// applyMigrations runs every *.up.sql in order, as the real deployment
// does. Reading the files rather than duplicating the schema means this
// test fails if a future migration forgets its grants.
func applyMigrations(ctx context.Context, t *testing.T, conn *pgx.Conn) {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "db", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no migrations found in %s", dir)
	}
	sort.Strings(files) // numeric prefixes make lexical order correct

	for _, file := range files {
		sqlBytes, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if _, err := conn.Exec(ctx, string(sqlBytes)); err != nil {
			t.Fatalf("apply %s: %v", filepath.Base(file), err)
		}
	}
}
