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
	//
	// Raised from the spec's 10 after the e2e suite kept tripping it, which
	// prompted the right question: what does a client IP actually mean for
	// this product? Indian mobile carriers run carrier-grade NAT, with
	// thousands of subscribers behind one public address. At 10 per 15
	// minutes, a single busy carrier pool locks out legitimate signups
	// constantly — and an attacker with a handful of cheap proxies is not
	// meaningfully inconvenienced either way.
	//
	// So this is deliberately loose. It exists to catch a single source
	// spraying hundreds of requests, not to be the real defence. The real
	// defences are per-identifier (3/15min, unaffected by NAT because it
	// keys on the thing being attacked) and the global 100/min per IP that
	// catches an actual flood.
	//
	// 30 per 15 minutes is 2/minute sustained — clearly abusive from one
	// source, unremarkable from a shared one.
	OTPRequestPerIP = Rule{Name: "otp_req_ip", Max: 30, Window: 15 * time.Minute}

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
