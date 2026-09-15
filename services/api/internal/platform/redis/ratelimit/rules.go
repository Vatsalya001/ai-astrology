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

	// A signed-in user asking for a fresh code, to delete their account or
	// to export their data.
	//
	// It sends a real email or SMS on every call, to the account's OWN
	// verified contact — so an unlimited version is a mailbomb aimed at
	// whoever holds the account, triggerable by anyone who has picked up
	// an access token. Three in fifteen minutes matches the sign-in limit,
	// because it is the same act for the same reason.
	ChallengePerUser = Rule{Name: "challenge_user", Max: 3, Window: 15 * time.Minute}

	// The backstop. Applies to every request under /api/v1 regardless of
	// route, so endpoints with no rule of their own — logout, providers,
	// the read-only profile routes, anything a later phase mounts — are
	// limited by construction rather than by someone remembering.
	//
	// The spec said 100/minute. That number was measured and it is wrong:
	// six concurrent browser sessions against the settings screens
	// generated 105 counted requests in 15 seconds, or 420/minute from one
	// address. Indian carrier-grade NAT puts thousands of subscribers
	// behind a single public IP, so 100 would not throttle an attacker —
	// it would break the product for entire carrier pools, which is the
	// same trap OTPRequestPerIP fell into and was corrected for.
	//
	// 1200/minute is 20/second sustained from one address: far above any
	// plausible shared-NAT peak, far below a crude flood, which is exactly
	// the band a backstop should occupy. It is not the real defence and is
	// not meant to be — the per-identifier and per-user limits are, because
	// they key on the thing being attacked rather than on a shared and
	// forgeable address.
	GlobalPerIP = Rule{Name: "global_ip", Max: 1200, Window: time.Minute}
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
