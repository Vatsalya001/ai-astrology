package users

import (
	"testing"
)

// Preferences are validated in the application, not by a CHECK
// constraint: adding a language should not require a migration, and the
// valid set changes per phase.
func TestPreferenceValidation(t *testing.T) {
	valid := map[string][]string{
		"language": {"en", "hi", "hinglish"},
		"system":   {"vedic", "western"},
		"style":    {"north", "south", "east"},
		"theme":    {"dark", "light", "system"},
	}
	sets := map[string]map[string]bool{
		"language": validLanguages,
		"system":   validSystems,
		"style":    validStyles,
		"theme":    validThemes,
	}

	for name, values := range valid {
		for _, v := range values {
			if !sets[name][v] {
				t.Errorf("%s: %q should be accepted", name, v)
			}
		}
	}

	// A value from the wrong category must not be accepted. These are the
	// mistakes that actually happen — a client sending the theme into the
	// chart-style field.
	rejects := map[string][]string{
		"language": {"fr", "dark", "north", "", "EN"},
		"system":   {"vedic ", "chinese", "north", ""},
		"style":    {"diagonal", "dark", "vedic", ""},
		"theme":    {"midnight", "north", "en", ""},
	}
	for name, values := range rejects {
		for _, v := range values {
			if sets[name][v] {
				t.Errorf("%s: %q should be rejected", name, v)
			}
		}
	}
}

// Gender is a closed set including an explicit opt-out. Leaving it free
// text invites analytics grouping on something people spell twelve ways;
// omitting "prefer_not_to_say" forces a disclosure the product does not
// need.
func TestGenderValues(t *testing.T) {
	for _, v := range []string{"male", "female", "other", "prefer_not_to_say"} {
		if !validGenders[v] {
			t.Errorf("%q should be accepted", v)
		}
	}
	for _, v := range []string{"", "Male", "m", "unknown"} {
		if validGenders[v] {
			t.Errorf("%q should be rejected", v)
		}
	}
}
