package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/reqctx"
)

// A birth chart is among the most sensitive objects in this system, and
// every route that reaches one does so through a birth profile ID in the
// URL. Checking ownership inside each handler would work right up until
// somebody adds the fourteenth handler and forgets — and the failure is
// silent, because the endpoint keeps returning perfectly valid data,
// just somebody else's.
//
// So the check is a middleware mounted on the subtree, and the verified
// ID is taken out of the request context rather than out of the URL. A
// handler that never reads chi.URLParam cannot be reached with an
// unverified ID.
//
// TestEveryProfileScopedRouteRefusesAStranger walks the real route table
// and asserts this for every route as it is actually mounted, so a route
// added outside the group fails the suite rather than shipping.

// ProfileOwnership answers one question and returns nothing else.
//
// Deliberately narrow. A lookup returning the profile would tempt a
// handler into using the middleware as a data source, and then the
// interface could not be satisfied by a cheap existence check.
//
// Declared here, by the consumer, so the domain package does not have to
// know an HTTP layer exists.
type ProfileOwnership interface {
	// Owns returns nil when the user owns the profile, and an error
	// otherwise. It does not distinguish "no such profile" from "not
	// yours" — see RequireProfileOwnership for why.
	Owns(ctx context.Context, userID, profileID uuid.UUID) error
}

// RequireProfileOwnership rejects any request for a profile the caller
// does not own.
//
// Everything that goes wrong here — malformed UUID, missing profile,
// somebody else's profile — produces the SAME 404 with the same body.
// A 403 would confirm that the ID names a real birth profile belonging to
// a real person, and the set of IDs an attacker can confirm is the set
// they can enumerate. The security rules require 404 for exactly this
// reason, and the difference is invisible to a legitimate client, which
// never asks for an ID it was not given.
func RequireProfileOwnership(owner ProfileOwnership, param string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := auth.PrincipalFrom(r.Context())
			if !ok {
				// Reaching here means the route was mounted outside the
				// authenticate group. Say 401 rather than 404: this one is
				// a wiring mistake, not a probe, and it should look like
				// one in the logs.
				WriteError(w, r, http.StatusUnauthorized, CodeUnauthorized,
					"Authentication required.", nil)
				return
			}

			profileID, err := uuid.Parse(chi.URLParam(r, param))
			if err != nil {
				notYours(w, r)
				return
			}

			if err := owner.Owns(r.Context(), principal.UserID, profileID); err != nil {
				notYours(w, r)
				return
			}

			next.ServeHTTP(w, r.WithContext(reqctx.WithProfileID(r.Context(), profileID)))
		})
	}
}

// notYours is the single response for every ownership failure.
//
// One function so the three call sites cannot drift into three subtly
// different bodies — which is exactly how a timing or wording oracle gets
// built by accident.
func notYours(w http.ResponseWriter, r *http.Request) {
	WriteError(w, r, http.StatusNotFound, CodeNotFound, "Not found.", nil)
}

// ProfileIDFrom re-exports reqctx.ProfileIDFrom for callers already in
// this package. Domain handlers use reqctx directly, because they cannot
// import httpapi — see that package's doc comment.
func ProfileIDFrom(ctx context.Context) (uuid.UUID, bool) {
	return reqctx.ProfileIDFrom(ctx)
}
