package auth

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// Roles, matching the user_role enum in migration 000002.
const (
	RoleUser       = "user"
	RoleAstrologer = "astrologer"
	RoleAdmin      = "admin"
	RoleSuperAdmin = "super_admin"
)

type contextKey int

const (
	principalKey contextKey = iota
)

// Principal is the authenticated caller.
//
// Only what the token carries. Anything else — name, email, preferences
// — is a database read the handler makes deliberately, which keeps PII
// out of the request context where it would be easy to log wholesale.
type Principal struct {
	UserID uuid.UUID
	Role   string
	// TokenID is the jti, so a specific token can be denied before it
	// expires (Phase 6).
	TokenID string
}

// PrincipalFrom returns the authenticated caller, if any.
//
// The boolean is not decoration: a handler that ignores it and uses a
// zero-valued Principal would treat every request as user uuid.Nil,
// which is one missing check away from serving one person's data to
// everybody.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey).(Principal)
	return p, ok
}

// ErrorWriter renders an error response.
//
// Injected rather than imported so this package does not depend on
// httpapi — which would create the import cycle the Go rules exist to
// prevent.
type ErrorWriter func(w http.ResponseWriter, r *http.Request, status int, code, message string)

// Authenticate validates the bearer token and attaches the Principal.
//
// Rejects with 401 and a generic message. The client can do nothing
// differently for an expired token versus a forged one, and saying which
// helps only someone probing.
func Authenticate(issuer *Issuer, writeErr ErrorWriter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				writeErr(w, r, http.StatusUnauthorized, "unauthorized", "Authentication required.")
				return
			}

			claims, err := issuer.VerifyAccessToken(token)
			if err != nil {
				// Expiry is the one case the client CAN act on — it means
				// "refresh and retry" rather than "log in again" — so it
				// gets a distinct code while the message stays generic.
				code := "unauthorized"
				if errors.Is(err, ErrTokenExpired) {
					code = "token_expired"
				}
				writeErr(w, r, http.StatusUnauthorized, code, "Authentication required.")
				return
			}

			userID, err := uuid.Parse(claims.Subject)
			if err != nil {
				writeErr(w, r, http.StatusUnauthorized, "unauthorized", "Authentication required.")
				return
			}

			ctx := context.WithValue(r.Context(), principalKey, Principal{
				UserID:  userID,
				Role:    claims.Role,
				TokenID: claims.ID,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole admits only the listed roles.
//
// Must be mounted AFTER Authenticate. A missing Principal is treated as
// 401 rather than 403: without one there is nobody to deny, and
// reporting 403 would imply an identity that does not exist.
//
// Roles are compared exactly, with no hierarchy. An implicit
// "admin includes user" ordering reads well and goes wrong the first
// time a role is added in the middle — so a route that admits both says
// so explicitly.
func RequireRole(writeErr ErrorWriter, allowed ...string) func(http.Handler) http.Handler {
	permitted := make(map[string]bool, len(allowed))
	for _, role := range allowed {
		permitted[role] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := PrincipalFrom(r.Context())
			if !ok {
				writeErr(w, r, http.StatusUnauthorized, "unauthorized", "Authentication required.")
				return
			}

			if !permitted[principal.Role] {
				// 403, not 404. The resource here is a capability, not a
				// record: hiding the existence of an admin route achieves
				// nothing, and 403 is what an operator needs to see in a
				// log. The 404-for-cross-user rule applies to records, and
				// is enforced in the handlers that read them.
				writeErr(w, r, http.StatusForbidden, "forbidden", "You do not have access to this resource.")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// bearerToken extracts a token from the Authorization header.
//
// Case-insensitive on the scheme, because RFC 7235 says the scheme is
// case-insensitive and some clients send "bearer".
func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", false
	}

	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "bearer") {
		return "", false
	}

	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	return token, true
}

// ClientIP resolves the caller's address for rate limiting.
//
// chi's middleware.RealIP is deliberately NOT used, and this is the
// reason: it trusts X-Forwarded-For unconditionally. Since Phase 1 rate
// limits per IP, a spoofable IP makes the limiter decorative — an
// attacker sends a different X-Forwarded-For on every request and never
// hits a limit, while a forged header belonging to someone else gets
// that person throttled.
//
// `trustProxy` must be false unless the service genuinely sits behind a
// proxy that overwrites the header. The default is not to trust it.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		// The leftmost entry is the original client; entries to its right
		// were added by successive proxies. Only meaningful when we know
		// our own proxy rewrites rather than appends.
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if first, _, found := strings.Cut(xff, ","); found {
				return strings.TrimSpace(first)
			}
			return strings.TrimSpace(xff)
		}
		if real := r.Header.Get("X-Real-IP"); real != "" {
			return strings.TrimSpace(real)
		}
	}

	// RemoteAddr is set by the server from the socket and cannot be
	// forged by the client.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// No port — take it as given rather than dropping the address,
		// since an empty subject would put every caller in one bucket.
		return r.RemoteAddr
	}
	return host
}
