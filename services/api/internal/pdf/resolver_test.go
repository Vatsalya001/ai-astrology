package pdf

import "testing"

// PHASE-03 §11.5: "chromedp runs sandboxed with no network access beyond
// the print route."
//
// The sandbox half is a deployment constraint. This is the other half,
// and it was absent for the whole of Phase 3 — the browser that renders
// somebody's birth data could reach anything on the internet.
//
// The threat is not a compromised Chrome. It is the print page carrying
// content it should not: a profile label that escaped escaping, a future
// template change, an SVG with a remote reference. Under that
// assumption, what matters is whether the render can phone home. With
// every hostname resolving to nothing, it cannot.
func TestTheBrowserCanResolveOnlyThePrintHost(t *testing.T) {
	for _, tc := range []struct {
		name, url string
		want      []string
		reject    []string
	}{
		{
			name:   "a real origin is allowed, nothing else is",
			url:    "https://ayana.app/kundli/print?token=x",
			want:   []string{"MAP * ~NOTFOUND", "EXCLUDE ayana.app"},
			reject: []string{"EXCLUDE evil.example"},
		},
		{
			name: "loopback is always allowed — dev and the worker container",
			url:  "http://localhost:3000/kundli/print",
			want: []string{"MAP * ~NOTFOUND", "EXCLUDE localhost"},
		},
		{
			name: "a port does not leak into the host rule",
			url:  "https://ayana.app:8443/kundli/print",
			want: []string{"EXCLUDE ayana.app"},
			// A rule carrying the port would never match, so the render
			// would fail to load its own page — an empty PDF, which is
			// the worst way to learn about it.
			reject: []string{"EXCLUDE ayana.app:8443"},
		},
		{
			name: "a malformed URL still denies everything",
			url:  "://not a url",
			want: []string{"MAP * ~NOTFOUND"},
		},
		{
			name: "an empty URL fails CLOSED, not open",
			url:  "",
			want: []string{"MAP * ~NOTFOUND"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := resolverRules(tc.url)
			for _, w := range tc.want {
				if !contains(got, w) {
					t.Errorf("rules %q missing %q", got, w)
				}
			}
			for _, r := range tc.reject {
				if contains(got, r) {
					t.Errorf("rules %q must not contain %q", got, r)
				}
			}
		})
	}
}

// The negative case. Without this, a `resolverRules` that returned ""
// would satisfy nothing above and every test would still pass, because
// they only assert on what IS present.
func TestTheRulesAreNeverEmptyOrPermissive(t *testing.T) {
	for _, u := range []string{"", "https://ayana.app/x", "garbage"} {
		got := resolverRules(u)
		if got == "" {
			t.Fatalf("empty rules for %q — the browser would have full egress", u)
		}
		if !contains(got, "MAP * ~NOTFOUND") {
			t.Errorf("rules for %q do not deny by default: %q", u, got)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		(haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
