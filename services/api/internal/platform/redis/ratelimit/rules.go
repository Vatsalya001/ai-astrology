package ratelimit

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// The limits from the Phase 1 spec §5.
//
// Declared as values rather than magic numbers at call sites so the
// whole policy is reviewable in one place — which is where a limit set
// too generously would be spotted.
var (
	// Per identifier. Three is enough for a genuine "I didn't get it",
	// and the daily cap stops a slow drip from adding up to an SMS bill.
	OTPRequestPerIdentifier      = Rule{Name: "otp_req_id", Max: 3, Window: 15 * time.Minute}
	OTPRequestPerIdentifierDaily = Rule{Name: "otp_req_id_day", Max: 10, Window: 24 * time.Hour}

	// Per IP, so one source cannot spray requests across many numbers.
	// Higher than the per-identifier limit because a shared NAT or an
	// office is legitimately several people.
	OTPRequestPerIP = Rule{Name: "otp_req_ip", Max: 10, Window: 15 * time.Minute}

	// Verification attempts. The OTP store burns the code after five
	// wrong guesses; this stops the endpoint being hammered with codes
	// that were never issued, which the store cannot see.
	OTPVerifyPerIdentifier = Rule{Name: "otp_verify_id", Max: 10, Window: 15 * time.Minute}

	// A client refreshing more than once a minute is broken or hostile.
	RefreshPerSession = Rule{Name: "refresh", Max: 60, Window: time.Hour}

	// The backstop. Applies to every request regardless of route.
	GlobalPerIP = Rule{Name: "global_ip", Max: 100, Window: time.Minute}
)

// randomSuffix disambiguates two events in the same nanosecond.
//
// Without it, two requests arriving together would share a sorted-set
// member and the second would be recorded as the first — one free
// request per collision, under exactly the concurrent load a limiter
// exists to handle.
func randomSuffix() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Cannot fail in practice. If it somehow does, an empty suffix
		// degrades to the collision above rather than dropping the
		// request — a limiter that fails closed on entropy exhaustion
		// would take the whole site down.
		return ""
	}
	return hex.EncodeToString(b[:])
}
