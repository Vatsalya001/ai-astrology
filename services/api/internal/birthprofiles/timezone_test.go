package birthprofiles

import (
	"testing"
	"time"
)

// The conversion that decides whether a chart is right.
//
// The ascendant moves about a degree every four minutes. A 30-minute
// offset error changes the rising sign and therefore every house
// placement — and the resulting chart looks completely ordinary, so
// nothing downstream can catch it.
//
// The spec asks for exactly these era fixtures, and the reason is that
// "just use +05:30" is wrong for a large fraction of Indian births.

func mustResolve(t *testing.T, date time.Time, clock time.Duration, zone string) ResolvedInstant {
	t.Helper()
	resolved, err := ResolveInstant(date, clock, zone)
	if err != nil {
		t.Fatalf("ResolveInstant(%v, %v, %q): %v", date, clock, zone, err)
	}
	return resolved
}

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// THE test for this file.
//
// A birth in Kolkata in 1943 was under wartime time, UTC+06:30. Resolving
// it with the modern +05:30 puts the instant an hour out, which is about
// fifteen degrees of ascendant — half a sign.
func TestIndianEraOffsets(t *testing.T) {
	cases := []struct {
		name    string
		when    time.Time
		clock   time.Duration
		wantMin int
	}{
		{
			// Before IST existed. Calcutta ran on its own local mean time,
			// set by its longitude rather than by a national standard.
			name: "1900 — local mean time, before IST",
			when: date(1900, time.January, 1), clock: 12 * time.Hour,
			wantMin: 5*60 + 21,
		},
		{
			name: "1905 — still local mean time the day before",
			when: date(1905, time.December, 31), clock: 12 * time.Hour,
			wantMin: 5*60 + 21,
		},
		{
			name: "1906 — IST adopted",
			when: date(1906, time.January, 1), clock: 12 * time.Hour,
			wantMin: 5*60 + 30,
		},
		{
			// The wartime offset, and it ran through the winter too —
			// measured, against the spec's "for parts of the year".
			name: "1943 January — wartime +06:30",
			when: date(1943, time.January, 15), clock: 12 * time.Hour,
			wantMin: 6*60 + 30,
		},
		{
			name: "1943 June — wartime +06:30",
			when: date(1943, time.June, 15), clock: 12 * time.Hour,
			wantMin: 6*60 + 30,
		},
		{
			name: "1994 — modern IST",
			when: date(1994, time.August, 17), clock: 14*time.Hour + 35*time.Minute,
			wantMin: 5*60 + 30,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resolved := mustResolve(t, c.when, c.clock, "Asia/Kolkata")

			if resolved.OffsetMinutes != c.wantMin {
				t.Errorf("offset = %+d:%02d, want %+d:%02d",
					resolved.OffsetMinutes/60, abs(resolved.OffsetMinutes%60),
					c.wantMin/60, c.wantMin%60)
			}
		})
	}
}

// The consequence, stated in the unit that matters.
//
// An implementation that hardcoded +05:30 would pass every modern test
// and be an hour wrong for a wartime birth. An hour is fifteen degrees of
// ascendant — half a sign — which changes the rising sign for half of all
// such births and every house placement for all of them.
func TestHardcodingTheModernOffsetWouldBeAnHourWrongIn1943(t *testing.T) {
	const clock = 14*time.Hour + 35*time.Minute

	correct := mustResolve(t, date(1943, time.June, 15), clock, "Asia/Kolkata")

	// What a naive implementation produces.
	naive := time.Date(1943, time.June, 15, 14, 35, 0, 0, time.FixedZone("IST", 5*3600+30*60)).UTC()

	drift := naive.Sub(correct.UTCInstant)
	if drift != time.Hour {
		t.Fatalf("expected a one-hour error from the naive offset, got %v", drift)
	}

	// Roughly a degree of ascendant every four minutes.
	degrees := drift.Minutes() / 4
	if degrees < 14 || degrees > 16 {
		t.Errorf("an hour is %.1f degrees of ascendant; expected about 15", degrees)
	}
}

// The same absolute moment described in two different zones must produce
// the same instant, because the instant is what the chart is computed
// from. If this failed, two users born simultaneously in Delhi and London
// would get different planetary positions.
func TestTheSameInstantInTwoZonesResolvesIdentically(t *testing.T) {
	// 1994-08-17 14:35 IST is 09:05 UTC, which is 10:05 BST in London.
	india := mustResolve(t, date(1994, time.August, 17), 14*time.Hour+35*time.Minute, "Asia/Kolkata")
	london := mustResolve(t, date(1994, time.August, 17), 10*time.Hour+5*time.Minute, "Europe/London")

	if !india.UTCInstant.Equal(london.UTCInstant) {
		t.Fatalf("same moment resolved differently: %v vs %v",
			india.UTCInstant, london.UTCInstant)
	}

	// And the offsets differ, so the test is not passing because both
	// were treated as UTC.
	if india.OffsetMinutes == london.OffsetMinutes {
		t.Error("both zones reported the same offset; timezone handling is not engaged")
	}
}

// A late-evening birth must not roll into the wrong calendar day.
//
// 23:45 on 31 December in India is 18:15 UTC on the SAME day, but the
// naive instinct is that a late local time crosses midnight. It does the
// other way for zones west of Greenwich, which is why both directions
// are checked.
func TestDateBoundariesAreHandledInBothDirections(t *testing.T) {
	t.Run("east of Greenwich rolls backwards", func(t *testing.T) {
		resolved := mustResolve(t,
			date(2000, time.December, 31), 23*time.Hour+45*time.Minute, "Asia/Kolkata")

		want := time.Date(2000, time.December, 31, 18, 15, 0, 0, time.UTC)
		if !resolved.UTCInstant.Equal(want) {
			t.Errorf("got %v, want %v", resolved.UTCInstant.Format(time.RFC3339), want.Format(time.RFC3339))
		}
	})

	t.Run("west of Greenwich rolls forwards", func(t *testing.T) {
		// 23:45 in New York on 31 December is 04:45 UTC on 1 JANUARY —
		// a different year, which is where an off-by-one becomes a
		// visibly wrong chart.
		resolved := mustResolve(t,
			date(2000, time.December, 31), 23*time.Hour+45*time.Minute, "America/New_York")

		if resolved.UTCInstant.Year() != 2001 {
			t.Errorf("got year %d, want 2001 — the date boundary was not crossed",
				resolved.UTCInstant.Year())
		}
	})
}

// A zone that does not exist must fail loudly.
//
// Degrading to UTC would be invisible in the output and catastrophic in
// the chart: five and a half hours of error for an Indian birth.
func TestAnUnknownZoneIsRefused(t *testing.T) {
	for _, zone := range []string{"Asia/Atlantis", "", "IST", "+05:30"} {
		_, err := ResolveInstant(date(1994, time.August, 17), 12*time.Hour, zone)
		if err == nil {
			t.Errorf("zone %q was accepted", zone)
		}
	}
}

// The embedded database, asserted directly.
//
// Without `_ "time/tzdata"` the binary depends on the container having a
// zoneinfo database. A scratch or distroless image has none, and this is
// the kind of failure that only shows up in production.
func TestTheTimezoneDatabaseIsEmbedded(t *testing.T) {
	for _, zone := range []string{
		"Asia/Kolkata", "Europe/London", "America/New_York",
		"America/Anchorage", "America/Guayaquil", "Australia/Sydney",
	} {
		if _, err := time.LoadLocation(zone); err != nil {
			t.Errorf("zone %q is unavailable: %v — is time/tzdata still imported?", zone, err)
		}
	}
}

// Noon, and why it is not midnight.
//
// This is not a guess at the birth time. It exists only so planetary
// longitudes are computable — the Moon moves ~13 degrees a day, so midday
// places it within half a sign. Midnight would sit on a date boundary,
// where an hour of error changes the calendar day and, at the turn of a
// month, the Sun's sign.
func TestUnknownTimeUsesLocalNoon(t *testing.T) {
	resolved, err := ResolveUnknownTime(date(1994, time.August, 17), "Asia/Kolkata")
	if err != nil {
		t.Fatalf("ResolveUnknownTime: %v", err)
	}

	// Noon IST is 06:30 UTC.
	want := time.Date(1994, time.August, 17, 6, 30, 0, 0, time.UTC)
	if !resolved.UTCInstant.Equal(want) {
		t.Errorf("got %v, want %v", resolved.UTCInstant, want)
	}

	// Still on the intended calendar day, which midnight would risk.
	local := resolved.UTCInstant.In(time.FixedZone("IST", 5*3600+30*60))
	if local.Day() != 17 {
		t.Errorf("noon resolution landed on day %d, not 17", local.Day())
	}
}

// Extreme latitudes and the equator, from the spec's fixture list. The
// resolver itself is latitude-independent, but these zones have unusual
// DST rules and must at least resolve.
func TestUnusualZonesResolve(t *testing.T) {
	cases := map[string]struct {
		zone string
		when time.Time
	}{
		"Anchorage, high latitude":    {"America/Anchorage", date(2000, time.June, 21)},
		"Quito, on the equator":       {"America/Guayaquil", date(1988, time.March, 15)},
		"London, mid-DST":             {"Europe/London", date(1995, time.July, 1)},
		"London, the day DST ends":    {"Europe/London", date(1995, time.October, 22)},
		"Sydney, southern-hemisphere": {"Australia/Sydney", date(2001, time.January, 15)},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			resolved := mustResolve(t, c.when, 14*time.Hour, c.zone)

			if resolved.UTCInstant.IsZero() {
				t.Fatal("resolved to the zero time")
			}
			// Sanity: every inhabited offset is within ±14 hours.
			if resolved.OffsetMinutes < -14*60 || resolved.OffsetMinutes > 14*60 {
				t.Errorf("implausible offset %d minutes", resolved.OffsetMinutes)
			}
		})
	}
}

// A leap day is a real date and must survive the round trip.
func TestLeapDayResolves(t *testing.T) {
	resolved := mustResolve(t, date(2000, time.February, 29), 10*time.Hour, "Asia/Kolkata")

	local := resolved.UTCInstant.In(time.FixedZone("IST", 5*3600+30*60))
	if local.Month() != time.February || local.Day() != 29 {
		t.Errorf("leap day became %v", local.Format("2006-01-02"))
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
