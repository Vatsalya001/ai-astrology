//go:build integration

package db_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

/*
Every migration has a real down, and this is what says so.

`.claude/rules/database.md`: "Every migration has a real `down`. 'Revert
the commit' is not a rollback plan."

That rule was prose until now. Nothing executed a down file, so a down
that referenced a table it never created, dropped things in an order the
foreign keys forbid, or was simply empty would sit in the repository
looking like a rollback plan until the night somebody needed one.

── Why a round trip rather than just running them ──

Rolling back once proves the SQL parses. Rolling forward again proves it
actually removed what the up created: a down that drops nothing
succeeds, and the second up then fails on an object that still exists.
That second pass is what makes this a test of the down rather than of
the parser.
*/
func TestEveryMigrationCanBeRolledBackAndReapplied(t *testing.T) {
	ctx := context.Background()
	writerDSN, _, terminate := startPostgres(ctx, t)
	defer terminate()

	conn, err := pgx.Connect(ctx, writerDSN)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	ups := migrationFiles(t, "*.up.sql")
	downs := migrationFiles(t, "*.down.sql")

	// Every up has a down. A missing file is the most common version of
	// this defect and the easiest to miss in review.
	if len(ups) != len(downs) {
		t.Fatalf("%d up migrations and %d down migrations:\n  ups:   %v\n  downs: %v",
			len(ups), len(downs), base(ups), base(downs))
	}
	for i, up := range ups {
		wantDown := strings.TrimSuffix(up, ".up.sql") + ".down.sql"
		if downs[i] != wantDown {
			t.Fatalf("%s has no matching down migration (found %s)",
				filepath.Base(up), filepath.Base(downs[i]))
		}
	}

	apply := func(files []string, label string) {
		t.Helper()
		for _, file := range files {
			body, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			if strings.TrimSpace(stripComments(string(body))) == "" {
				t.Fatalf("%s contains no statements. An empty down is not a rollback "+
					"plan — it is a file that makes one look like it exists",
					filepath.Base(file))
			}
			if _, err := conn.Exec(ctx, string(body)); err != nil {
				t.Fatalf("%s %s: %v", label, filepath.Base(file), err)
			}
		}
	}

	apply(ups, "apply")

	// Reverse order: a down that runs before its dependents' downs will
	// block on a foreign key, which is exactly the mistake worth
	// catching.
	reversed := make([]string, len(downs))
	for i, file := range downs {
		reversed[len(downs)-1-i] = file
	}
	apply(reversed, "roll back")

	/*
	   Forward again.

	   This is the half that tests the DOWN rather than the parser. A
	   down that dropped nothing exits zero; the up that follows then
	   fails on an object that already exists, and says which one.
	*/
	apply(ups, "re-apply after rollback")
}

func migrationFiles(t *testing.T, pattern string) []string {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "db", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}
	if len(files) == 0 {
		t.Fatalf("no files matching %s in %s — this test would pass vacuously",
			pattern, dir)
	}
	sort.Strings(files) // numeric prefixes make lexical order correct
	return files
}

// stripComments removes `--` lines so an all-comment file is recognised
// as empty. A down consisting only of an explanation is the most
// plausible way to end up with a rollback that does nothing.
func stripComments(sql string) string {
	var kept []string
	for _, line := range strings.Split(sql, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func base(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, filepath.Base(path))
	}
	return out
}
