package transits_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/transits"
)

// The rotation is four characters of modular arithmetic and one +1, and
// getting it wrong does not crash anything — it produces a complete,
// confident, plausible transit reading that is shifted by one house or
// reflected about the Moon. There is no way to notice that by looking at
// it, so it gets an exhaustive test instead.

func TestHouseFromMoonCountsInclusivelyFromTheMoonsOwnSign(t *testing.T) {
	// Sign indices: 0 Aries … 8 Sagittarius, 9 Capricorn, 10 Aquarius,
	// 11 Pisces.
	cases := []struct {
		name          string
		planetSign    int
		natalMoonSign int
		want          int
	}{
		// The Vedic convention: the Moon's own sign is the FIRST house,
		// not the zeroth. Every one of these is off by one if the +1 goes.
		{"a planet in the Moon's own sign is the 1st", 9, 9, 1},
		{"the next sign along is the 2nd", 10, 9, 2},

		// Sade Sati is exactly these three, so they are the rows that
		// decide whether the product's most-asked-about answer is right.
		{"the sign before the Moon is the 12th", 8, 9, 12},
		{"Saturn rising: 12th from a Capricorn Moon", 8, 9, 12},
		{"Saturn at peak: over a Capricorn Moon", 9, 9, 1},
		{"Saturn setting: 2nd from a Capricorn Moon", 10, 9, 2},

		// Wrapping backwards past Aries is where a single modulo fails:
		// Go's % keeps the dividend's sign, so 11-0 is fine but 0-11 is
		// -11 and yields -10 without the second fold.
		{"wrapping backwards across Aries", 11, 0, 12},
		{"wrapping forwards across Pisces", 0, 11, 2},
		{"the opposite sign is the 7th", 3, 9, 7},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := transits.HouseFromMoon(tc.planetSign, tc.natalMoonSign)
			if got != tc.want {
				t.Fatalf("HouseFromMoon(planet=%d, moon=%d) = %d, want %d",
					tc.planetSign, tc.natalMoonSign, got, tc.want)
			}
		})
	}
}

// Every natal Moon must see the twelve signs as the twelve houses, once
// each. This is the property that a reflected or truncated rotation
// fails: it still returns numbers in 1..12, but two signs collide and
// one house goes missing.
func TestEveryMoonSignSeesEachHouseExactlyOnce(t *testing.T) {
	for moon := 0; moon < transits.SignCount; moon++ {
		seen := map[int]int{}
		for sign := 0; sign < transits.SignCount; sign++ {
			house := transits.HouseFromMoon(sign, moon)
			if house < 1 || house > transits.SignCount {
				t.Fatalf("moon %d, sign %d: house %d is outside 1..12", moon, sign, house)
			}
			seen[house]++
		}
		for house := 1; house <= transits.SignCount; house++ {
			if seen[house] != 1 {
				t.Fatalf("with the Moon in sign %d, house %d was produced %d times "+
					"— the rotation is not a permutation", moon, house, seen[house])
			}
		}
	}
}

// The three Sade Sati phase names are duplicated across a language
// boundary: a Go constant here and a Python Literal in astro-service.
// Nothing in either build would notice if one side were renamed, and the
// symptom would be an API returning a phase string the UI has no case
// for — rendering as blank, on the one screen users care most about.
//
// So this test reads the Python and compares.
func TestSadeSatiPhaseNamesMatchAstroService(t *testing.T) {
	path := filepath.Join("..", "..", "..", "astro", "app", "core", "transit.py")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v\n"+
			"this test exists to catch drift between the Go and Python phase names; "+
			"if the file moved, point it at the new path rather than deleting the test", path, err)
	}

	// SADE_SATI_PHASES = {12: "rising", 1: "peak", 2: "setting"}
	block := regexp.MustCompile(`(?s)SADE_SATI_PHASES\s*=\s*\{(.*?)\}`).FindSubmatch(source)
	if block == nil {
		t.Fatal("could not find SADE_SATI_PHASES in astro-service; " +
			"the mapping this test compares against has been renamed or restructured")
	}

	python := map[int]string{}
	for _, m := range regexp.MustCompile(`(\d+)\s*:\s*"([a-z]+)"`).FindAllSubmatch(block[1], -1) {
		house, convErr := strconv.Atoi(string(m[1]))
		if convErr != nil {
			t.Fatalf("unparseable house key %q", m[1])
		}
		python[house] = string(m[2])
	}

	golang := map[int]string{
		12: transits.PhaseRising,
		1:  transits.PhasePeak,
		2:  transits.PhaseSetting,
	}

	if len(python) != len(golang) {
		t.Fatalf("astro-service maps %d houses to phases, Go maps %d: %v vs %v",
			len(python), len(golang), python, golang)
	}
	for house, name := range golang {
		if python[house] != name {
			t.Fatalf("house %d is %q in Go and %q in astro-service — "+
				"the two have drifted", house, name, python[house])
		}
	}
}

// The zodiac is a second list duplicated across the Go/Python boundary,
// and it is load-bearing in a way the phase names are not: the natal
// Moon sign arrives from a stored chart as a NAME and the rotation needs
// an INDEX, so a reordering here shifts every user's gochara by however
// far the list moved. It still renders. It still looks plausible.
func TestSignNamesMatchAstroService(t *testing.T) {
	path := filepath.Join("..", "..", "..", "astro", "app", "core", "constants.py")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	block := regexp.MustCompile(`(?s)SIGNS:\s*Final\s*=\s*\((.*?)\)`).FindSubmatch(source)
	if block == nil {
		t.Fatal("could not find SIGNS in astro-service constants.py; " +
			"the tuple this test compares against has been renamed or restructured")
	}

	var python []string
	for _, m := range regexp.MustCompile(`"([A-Za-z]+)"`).FindAllSubmatch(block[1], -1) {
		python = append(python, string(m[1]))
	}

	if len(python) != len(transits.SignNames) {
		t.Fatalf("astro-service lists %d signs, Go lists %d: %v vs %v",
			len(python), len(transits.SignNames), python, transits.SignNames)
	}
	for i, name := range transits.SignNames {
		if python[i] != name {
			t.Fatalf("sign %d is %q in Go and %q in astro-service — the two orders "+
				"have drifted, so a chart naming either sign now rotates to the "+
				"wrong house for every user",
				i, name, python[i])
		}
	}
}

// An unrecognised sign must be an error, never index 0 — because Aries
// IS index 0, and a not-found zero would place every unknown sign at the
// start of the zodiac and compute houses from a Moon it never found.
func TestAnUnknownSignIsAnErrorNotAries(t *testing.T) {
	if index, err := transits.SignIndex("Ophiuchus"); err == nil {
		t.Fatalf("an unknown sign resolved to index %d instead of failing", index)
	}

	index, err := transits.SignIndex("Aries")
	if err != nil || index != 0 {
		t.Fatalf("SignIndex(\"Aries\") = %d, %v; want 0, nil", index, err)
	}
}
