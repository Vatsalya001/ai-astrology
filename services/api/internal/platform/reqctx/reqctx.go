// Package reqctx holds request-scoped values that cross the boundary
// between the HTTP layer and a domain handler.
//
// It exists because of a direction problem. `httpapi` imports the domain
// packages in order to mount their routes, so a domain package cannot
// import `httpapi` back — which is where the ownership middleware lives,
// and which is therefore where the verified profile ID would naturally
// be stashed.
//
// Putting the key in `platform/` breaks the cycle without weakening
// anything: platform imports no domain, so both sides can depend on it.
package reqctx

import (
	"context"

	"github.com/google/uuid"
)

// profileIDKey is an unexported struct type, which is the only way to
// make a context key that no other package can collide with. A string
// key would be one `"profile_id"` elsewhere away from a silent overwrite.
type profileIDKey struct{}

// WithProfileID records a birth profile ID that has been verified to
// belong to the authenticated caller.
//
// Only the ownership middleware should call this. The value's meaning is
// "somebody checked" — writing it anywhere else makes that a lie, and
// every handler downstream is relying on it.
func WithProfileID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, profileIDKey{}, id)
}

// ProfileIDFrom returns the verified birth profile ID.
//
// Handlers must use this rather than chi.URLParam. The two carry the same
// value today; the difference is that this one cannot be present unless
// the ownership check passed, so a handler written against it stays
// correct if the route is later moved or copied.
//
// That "must" is enforced, not merely asked for. Swapping this call for
// uuid.Parse(chi.URLParam(...)) passed the whole suite when it was only
// a comment — the two are identical whenever the route is mounted
// correctly, and every test mounted it correctly.
// httpapi.TestNoProfileScopedHandlerReadsTheIDFromTheURL now fails on the
// swap, and the route walk catches the consequence one layer out.
func ProfileIDFrom(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(profileIDKey{}).(uuid.UUID)
	return id, ok
}
