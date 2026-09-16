package birthprofiles

import (
	"fmt"
	"time"

	// The full IANA database, compiled into the binary.
	//
	// Without this the binary depends on the container having a zoneinfo
	// database. A scratch or distroless image does not, and
	// time.LoadLocation then fails — or worse, silently falls back to UTC
	// on some platforms, which for an Indian birth is five and a half
	// hours of error and a chart more than a whole sign wrong.
	_ "time/tzdata"
)

// ResolvedInstant is a local birth time turned into an absolute moment.
type ResolvedInstant struct {
	// UTCInstant is the source of truth for every calculation. Once this
	// is right, nothing downstream has to think about timezones again.
	UTCInstant time.Time

	// OffsetMinutes is what was actually in force at that instant.
	// Stored so it can be shown and audited — "we used +06:30 because
	// your birth was during the war" is an answerable question only if
	// the number was kept.
	OffsetMinutes int

	// Zone is the abbreviation tzdata reports, e.g. "IST". Informational.
	Zone string
}

// ResolveInstant converts a local birth date and time in an IANA zone
// into an absolute UTC instant.
//
// This is the single most consequential conversion in the product. The
// ascendant moves about one degree every four minutes, so a 30-minute
// offset error changes the rising sign and with it every house placement
// in the chart — and the result still looks like a perfectly ordinary
// chart.
//
// It lives in Go rather than Python because the Go standard library
// carries the full historical IANA database, including transitions that
// naive implementations miss. India is the example that matters here,
// and these are measured values from the tzdata this binary embeds:
//
//	1900-01-01  +05:21   Calcutta local mean time, before IST existed
//	1905-12-31  +05:21   still LMT the day before
//	1906-01-01  +05:30   IST adopted
//	1943-01-15  +06:30   wartime, and it ran through the winter too
//	1943-06-15  +06:30
//	1994-08-17  +05:30   modern
//
// Note the 1943 pair. The spec describes the wartime offset as applying
// "for parts of the year"; tzdata reports +06:30 in January as well as
// June, so for 1943 it was continuous. Recorded because the difference
// is an hour, and an hour is fifteen degrees of ascendant.
//
// Anyone tempted to shortcut this with a fixed +05:30 should note that
// doing so puts a 1943 Kolkata birth an hour out and a 1900 one nine
// minutes out.
func ResolveInstant(
	birthDate time.Time,
	birthTime time.Duration,
	zoneName string,
) (ResolvedInstant, error) {
	// time.LoadLocation("") returns UTC and NO ERROR — documented Go
	// behaviour, and a silent catastrophe here. An empty zone reaching
	// this function means the caller lost it somewhere upstream, and
	// treating that as UTC puts an Indian birth five and a half hours
	// out with nothing in the output to show for it.
	//
	// Found by a test asserting bad zones are refused; the empty string
	// was the one that got through.
	if zoneName == "" {
		return ResolvedInstant{}, fmt.Errorf(
			"birthprofiles: empty timezone; refusing to default to UTC, which " +
				"would silently shift the chart")
	}

	location, err := time.LoadLocation(zoneName)
	if err != nil {
		// Named, not swallowed. A missing zone must never degrade to UTC:
		// that failure is invisible in the output and catastrophic in the
		// chart.
		return ResolvedInstant{}, fmt.Errorf("birthprofiles: load timezone %q: %w", zoneName, err)
	}

	hours := int(birthTime / time.Hour)
	minutes := int((birthTime % time.Hour) / time.Minute)
	seconds := int((birthTime % time.Minute) / time.Second)

	// Constructed IN the location, so tzdata applies the offset that was
	// actually in force on that date — not today's.
	local := time.Date(
		birthDate.Year(), birthDate.Month(), birthDate.Day(),
		hours, minutes, seconds, 0,
		location,
	)

	zone, offsetSeconds := local.Zone()

	return ResolvedInstant{
		UTCInstant:    local.UTC(),
		OffsetMinutes: offsetSeconds / 60,
		Zone:          zone,
	}, nil
}

// ResolveUnknownTime handles a birth whose time nobody knows.
//
// Noon local is used ONLY so the planetary longitudes are computable —
// the Moon moves about 13 degrees a day, so midday puts it within half a
// sign of wherever it really was, which is honest enough for a sign
// placement.
//
// It is emphatically not a guess at the birth time. The ascendant moves a
// full circle in a day and cannot be estimated at all, which is why
// astro-service omits it, the houses and the dashas entirely when
// time_accuracy is "unknown". Noon is chosen over midnight because
// midnight sits on a date boundary: an hour of error either way changes
// the calendar day, and with it the Sun's sign at the turn of a month.
func ResolveUnknownTime(birthDate time.Time, zoneName string) (ResolvedInstant, error) {
	return ResolveInstant(birthDate, 12*time.Hour, zoneName)
}
