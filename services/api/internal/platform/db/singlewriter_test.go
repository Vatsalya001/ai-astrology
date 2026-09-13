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
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
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
		t.Skipf("could not start Postgres container (is Docker running?): %v", err)
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
