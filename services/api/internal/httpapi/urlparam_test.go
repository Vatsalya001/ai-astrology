package httpapi

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ownership.go says handlers must read the verified profile ID from the
// request context rather than from chi.URLParam, "so a handler written
// against it stays correct if the route is later moved".
//
// That was a claim no test enforced. Swapping reqctx.ProfileIDFrom for
// uuid.Parse(chi.URLParam(...)) in the birth-profile handler passed the
// entire suite — because the two carry the same value whenever the route
// IS mounted correctly, and every existing test mounts it correctly.
//
// The route walk in routes_integration_test.go catches the consequence
// (an unguarded route leaking somebody else's profile). This catches the
// cause, in one cheap unit test with no containers, at the moment the
// line is written rather than at the moment a second mistake compounds
// it.
//
// Scoped to the profile-ID parameters only. Reading {id} for a session
// or a place is fine — those are not profile-scoped resources, and the
// session handler does exactly that.

var profileIDParams = []string{`"id"`, `"birthProfileId"`}

// handlersUnderTheOwnershipRule are the files whose routes sit behind
// RequireProfileOwnership. Extend this list when a phase adds another —
// and note that forgetting to is caught anyway, one layer out, by the
// route walk.
var handlersUnderTheOwnershipRule = []string{
	filepath.Join("..", "birthprofiles", "handler.go"),
}

func TestNoProfileScopedHandlerReadsTheIDFromTheURL(t *testing.T) {
	urlParam := regexp.MustCompile(`chi\.URLParam\(\s*r(?:eq)?\s*,\s*("[^"]*")\s*\)`)

	for _, path := range handlersUnderTheOwnershipRule {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v\n"+
				"this guard names the files it protects; if one moved, point it at the "+
				"new path rather than dropping the entry", path, err)
		}

		for _, match := range urlParam.FindAllStringSubmatch(string(source), -1) {
			for _, param := range profileIDParams {
				if match[1] != param {
					continue
				}
				t.Fatalf("%s reads %s from the URL with %s\n\n"+
					"Routes behind RequireProfileOwnership must take the profile ID from\n"+
					"reqctx.ProfileIDFrom(ctx), which cannot be present unless the check\n"+
					"passed. Reading the URL directly works identically today and keeps\n"+
					"working if the route is later mounted outside the ownership group —\n"+
					"at which point it serves one user's birth profile to another with no\n"+
					"error anywhere.",
					filepath.Base(path), param, strings.TrimSpace(match[0]))
			}
		}
	}
}
