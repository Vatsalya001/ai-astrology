package httpapi

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis/ratelimit"
)

// GlobalThrottle is the per-IP backstop.
//
// Every route-specific limit is a decision someone has to remember to
// make. This one is not: it applies to everything under /api/v1, so an
// endpoint added in a later phase is limited from the moment it is
// mounted rather than from the moment somebody notices.
//
// It is deliberately loose — 100 requests a minute is far above anything
// a person generates and far below a flood. The narrow limits (OTP,
// verify, challenge) are the real defence for the endpoints that cost
// money or send mail; this exists so that "every endpoint is rate
// limited" is true by construction.
//
// It keys on the HASHED IP. A raw IP is PII under the project's rules and
// must never be persisted, and a Redis key is persistence.
func GlobalThrottle(limiter *ratelimit.Limiter, trustProxy bool, ipSalt string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Preflight carries no credentials and does no work. Counting
			// it would mean a page making N cross-origin calls burns 2N.
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			subject := ipSubject(r, trustProxy, ipSalt)

			res, err := limiter.Allow(r.Context(), ratelimit.GlobalPerIP, subject)
			if err != nil {
				// Fail OPEN. Redis being down must not take authentication
				// with it: the narrow per-identifier limits are the ones
				// that matter for abuse, and they fail closed at their own
				// call sites where the trade is different. A backstop that
				// takes the site down when the cache blips is a worse
				// outage than the one it prevents.
				logThrottleError(r, err)
				next.ServeHTTP(w, r)
				return
			}

			if !res.Allowed {
				retry := int(res.RetryAfter.Seconds())
				if retry < 1 {
					retry = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retry))
				WriteError(w, r, http.StatusTooManyRequests, CodeRateLimited,
					"Too many requests. Please slow down.", nil)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// logThrottleError records a limiter failure without the address.
//
// Loud, because failing open is a real reduction in protection and a
// silent one would never be noticed. The trace ID ties it to the request
// without naming the client.
func logThrottleError(r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "rate limiter unavailable; allowing the request",
		slog.String("rule", "global_ip"),
		slog.Any("err", err),
	)
}

// ipSubject is the limiter key for a client address.
//
// Hashed with the same salt used for persistence, so one IP produces one
// key across the process without the raw address ever being written.
func ipSubject(r *http.Request, trustProxy bool, salt string) string {
	return string(auth.HashIP(auth.ClientIP(r, trustProxy), salt))
}
