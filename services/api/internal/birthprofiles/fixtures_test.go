package birthprofiles_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/birthprofiles"
)

// The thirty shared chart fixtures, resolved through Go's tzdata.
//
// `expected_utc_offset_min` in profiles.json is generated from Python's
// `zoneinfo`. Asserting it here checks it against Go's `time/tzdata`,
// which is a SECOND, independently packaged copy of the tz database.
// Two implementations agreeing on a historical offset is evidence; one
// person's recollection of what India's wartime offset was is not — the
// hand-written fixture that preceded this got 1901 Calcutta wrong by 32
// minutes for exactly that reason.
//
// It also covers the half of the pipeline Python never sees. astro-service
// receives a UTC instant; deriving that instant from a local date, a local
// clock time and an IANA zone is Go's job alone, and it is the hardest
// part of the problem. A 1943 Kolkata birth resolved with today's +05:30
// instead of the wartime +06:30 moves the ascendant by roughly fifteen
// degrees, and nothing downstream can tell.

type fixtureProfile struct {
	ID           string  `json:"id"`
	Note         string  `json:"note"`
	BirthDate    string  `json:"birth_date"`
	BirthTime    *string `json:"birth_time"`
	TimeAccuracy string  `json:"time_accuracy"`
	Timezone     string  `json:"timezone"`
	OffsetMin    int     `json:"expected_utc_offset_min"`
}

func loadFixtures(t *testing.T) []fixtureProfile {
	t.Helper()

	path, err := filepath.Abs(filepath.Join(
		"..", "..", "..", "..", "tests", "fixtures", "charts", "profiles.json"))
	if err != nil {
		t.Fatalf("resolve fixtures: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v\n"+
			"these fixtures are shared with astro-service; if they moved, point this "+
			"test at the new path rather than deleting it", path, err)
	}

	var file struct {
		Profiles []fixtureProfile `json:"profiles"`
	}
	if err := json.Unmarshal(body, &file); err != nil {
		t.Fatalf("decode fixtures: %v", err)
	}
	return file.Profiles
}

func TestThereAreThirtySharedFixtures(t *testing.T) {
	profiles := loadFixtures(t)
	if len(profiles) != 30 {
		t.Fatalf("profiles.json has %d profiles; the Phase 2 gate specifies 30. "+
			"Deleting the awkward ones — the date-line pair, the quarter-hour "+
			"offsets — is how this suite goes green while losing the coverage "+
			"that was hard to get", len(profiles))
	}
}

// The headline: Go and Python must agree on every historical offset.
func TestEveryFixtureResolvesToTheOffsetPythonComputed(t *testing.T) {
	for _, profile := range loadFixtures(t) {
		t.Run(profile.ID, func(t *testing.T) {
			birthDate, err := time.Parse("2006-01-02", profile.BirthDate)
			if err != nil {
				t.Fatalf("parse birth_date: %v", err)
			}

			var clock time.Duration
			if profile.BirthTime != nil {
				parsed, parseErr := time.Parse("15:04", *profile.BirthTime)
				if parseErr != nil {
					t.Fatalf("parse birth_time: %v", parseErr)
				}
				clock = time.Duration(parsed.Hour())*time.Hour +
					time.Duration(parsed.Minute())*time.Minute
			}

			var resolved birthprofiles.ResolvedInstant
			if profile.TimeAccuracy == birthprofiles.AccuracyUnknown {
				resolved, err = birthprofiles.ResolveUnknownTime(birthDate, profile.Timezone)
			} else {
				resolved, err = birthprofiles.ResolveInstant(birthDate, clock, profile.Timezone)
			}
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}

			if resolved.OffsetMinutes != profile.OffsetMin {
				t.Fatalf("Go's tzdata says %s was UTC%+d minutes on %s; Python's zoneinfo "+
					"said %+d.\n%s\n\nTwo copies of the tz database disagreeing means one "+
					"of them was updated and a historical zone moved. The chart shifts by "+
					"%.2f degrees of ascendant.",
					profile.Timezone, resolved.OffsetMinutes, profile.BirthDate,
					profile.OffsetMin, profile.Note,
					float64(resolved.OffsetMinutes-profile.OffsetMin)*0.25)
			}
		})
	}
}

// The wartime trio, called out because it is the case the fixtures exist
// for and the one a naive implementation gets wrong in a way that looks
// completely normal.
func TestIndiasWartimeOffsetIsBracketedOnBothSides(t *testing.T) {
	byID := map[string]fixtureProfile{}
	for _, profile := range loadFixtures(t) {
		byID[profile.ID] = profile
	}

	expected := map[string]int{
		"028-kolkata-1942-before-wartime-dst": 330,
		"003-kolkata-1943-wartime-dst":        390,
		"029-kolkata-1945-after-wartime-dst":  330,
	}

	for id, offset := range expected {
		profile, ok := byID[id]
		if !ok {
			t.Fatalf("fixture %s is missing; the wartime offset is now asserted on "+
				"only one side and a constant +06:30 would pass", id)
		}
		if profile.OffsetMin != offset {
			t.Fatalf("%s expects %+d minutes, fixture says %+d", id, offset, profile.OffsetMin)
		}
	}
}

// Offsets that are not a whole number of hours, which is where an
// implementation storing hours rather than minutes stops working.
func TestFractionalHourOffsetsSurviveTheRoundTrip(t *testing.T) {
	interesting := map[string]int{
		"013-kathmandu-1987-quarter-hour-offset": 345,  // +05:45
		"014-chatham-2005-quarter-hour-dst":      825,  // +13:45
		"018-st-johns-2005-negative-half-hour":   -150, // -02:30
		"009-kolkata-1901-local-mean-time":       321,  // +05:21
		"021-kiritimati-2000-furthest-forward":   840,  // +14:00
		"019-apia-2010-east-of-the-date-line":    -660, // -11:00
		"020-apia-2012-west-of-the-date-line":    780,  // +13:00
	}

	for _, profile := range loadFixtures(t) {
		want, checked := interesting[profile.ID]
		if !checked {
			continue
		}
		if profile.OffsetMin != want {
			t.Fatalf("%s: fixture says %+d minutes, this test expects %+d",
				profile.ID, profile.OffsetMin, want)
		}
		if profile.OffsetMin%60 == 0 && want%60 != 0 {
			t.Fatalf("%s is no longer a fractional-hour case", profile.ID)
		}
	}
}

// Samoa moved across the date line in 2011 by skipping 30 December
// entirely. The same coordinates, three hundred and sixty degrees of
// nothing apart: a 24-hour swing at one location.
func TestSamoaSwingsTwentyFourHoursAcrossTheDateLine(t *testing.T) {
	byID := map[string]fixtureProfile{}
	for _, profile := range loadFixtures(t) {
		byID[profile.ID] = profile
	}

	before := byID["019-apia-2010-east-of-the-date-line"]
	after := byID["020-apia-2012-west-of-the-date-line"]

	swing := after.OffsetMin - before.OffsetMin
	if swing != 24*60 {
		t.Fatalf("Apia's offset moved by %d minutes between 2010 and 2012, want 1440. "+
			"A cached or hard-coded zone offset for this location is wrong by a "+
			"whole day on one side of 2011.", swing)
	}
}
