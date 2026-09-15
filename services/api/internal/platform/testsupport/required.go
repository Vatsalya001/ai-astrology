// Package testsupport holds helpers shared by integration tests.
//
// It is a normal package rather than a _test.go file because the tests
// that need it live in five different packages, and Go gives test files
// no way to share code across package boundaries.
package testsupport

import (
	"os"
	"testing"
)

// EnvRequireContainers, when set, turns "Docker is unavailable" from a
// skip into a failure.
const EnvRequireContainers = "REQUIRE_CONTAINERS"

// ContainerUnavailable reports that a container could not be started.
//
// Locally this skips: a developer without Docker running should get a
// fast unit suite and a clear note, not a wall of red.
//
// In CI it FAILS, because the alternative is the worst kind of green. If
// the runner loses Docker, every integration test skips, `go test`
// reports ok, and the job passes having proven nothing — while the
// things it exists to prove are the single-writer grants, refresh reuse
// detection and the rate limiter under concurrency. A gate that cannot
// tell "passed" from "did not run" is not a gate.
//
// CI opts in by setting REQUIRE_CONTAINERS=1.
func ContainerUnavailable(t *testing.T, what string, err error) {
	t.Helper()

	if os.Getenv(EnvRequireContainers) != "" {
		t.Fatalf("could not start %s: %v\n"+
			"%s is set, so this is a failure rather than a skip — "+
			"an integration suite that silently does not run is worse than one that fails",
			what, err, EnvRequireContainers)
	}

	t.Skipf("could not start %s (is Docker running?): %v", what, err)
}
