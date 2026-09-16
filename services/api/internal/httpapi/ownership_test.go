package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/reqctx"
)

// owner is a ProfileOwnership that allows exactly one pair.
type owner struct {
	userID    uuid.UUID
	profileID uuid.UUID
	// calls counts the lookups, which is how "the middleware ran" becomes
	// a measurement rather than an inference from the status code.
	calls int
}

func (o *owner) Owns(_ context.Context, userID, profileID uuid.UUID) error {
	o.calls++
	if userID == o.userID && profileID == o.profileID {
		return nil
	}
	return errors.New("not found")
}

// notARealSigningKey is long enough for NewIssuer to accept and nothing
// else. The value is deliberately self-describing: the pre-commit secret
// scanner flagged the previous one, and its own advice is to make a test
// value obviously fake rather than to allowlist the file — an allowlist
// entry blunts the scanner for real secrets too.
const notARealSigningKey = "example-not-a-real-signing-key-32b"

// guarded builds a router with one route behind the REAL authenticate
// middleware and the ownership middleware, and reports whether the
// handler was reached.
//
// The real auth chain rather than a forged principal: the alternative
// would be exporting a WithPrincipal helper from the auth package, which
// is a function whose entire purpose is to manufacture an identity. That
// is not a thing this codebase should own for the convenience of a test.
func guarded(t *testing.T, o *owner) (http.Handler, *bool, *uuid.UUID, *auth.Issuer) {
	t.Helper()

	issuer, err := auth.NewIssuer(notARealSigningKey, time.Minute, time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	reached := false
	var seen uuid.UUID

	r := chi.NewRouter()
	r.Route("/birth-profiles", func(r chi.Router) {
		r.Use(auth.Authenticate(issuer, AuthMiddlewareErrorWriter))
		r.Group(func(r chi.Router) {
			r.Use(RequireProfileOwnership(o, "id"))
			r.Get("/{id}", func(w http.ResponseWriter, req *http.Request) {
				reached = true
				seen, _ = reqctx.ProfileIDFrom(req.Context())
				w.WriteHeader(http.StatusOK)
			})
		})
	})

	return r, &reached, &seen, issuer
}

// tokenFor mints a real access token for a user.
func tokenFor(t *testing.T, issuer *auth.Issuer, userID uuid.UUID) string {
	t.Helper()
	token, err := issuer.IssueAccessToken(userID, uuid.New(), "user")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	return token
}

func get(handler http.Handler, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// The property the specification names: user B asking for user A's
// resource gets 404, not 403. A 403 confirms the ID belongs to a real
// person, which turns the endpoint into an oracle for enumerating them.
func TestAStrangerGetsNotFoundAndNeverReachesTheHandler(t *testing.T) {
	o := &owner{userID: uuid.New(), profileID: uuid.New()}

	handler, reached, _, issuer := guarded(t, o)
	stranger := tokenFor(t, issuer, uuid.New())
	rec := get(handler, "/birth-profiles/"+o.profileID.String(), stranger)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("a stranger got %d, want 404 — a 403 would confirm the profile exists",
			rec.Code)
	}
	if *reached {
		t.Fatal("the handler ran for a profile the caller does not own; " +
			"the middleware is mounted but not blocking")
	}
}

// Every failure mode must be indistinguishable. A malformed UUID that
// returned 400 while a stranger's valid UUID returned 404 would let an
// attacker separate "well-formed but not yours" from "nonsense" — and
// from there, "yours" from "somebody's".
func TestEveryRefusalIsByteIdentical(t *testing.T) {
	o := &owner{userID: uuid.New(), profileID: uuid.New()}

	handler, _, _, issuer := guarded(t, o)
	caller := tokenFor(t, issuer, o.userID)

	cases := map[string]string{
		"somebody else's profile": uuid.NewString(),
		"a malformed id":          "not-a-uuid",
		"an empty-looking id":     "00000000-0000-0000-0000-000000000000",
	}

	var reference string
	for name, id := range cases {
		rec := get(handler, "/birth-profiles/"+id, caller)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s produced %d, want 404", name, rec.Code)
		}
		if reference == "" {
			reference = rec.Body.String()
			continue
		}
		if rec.Body.String() != reference {
			t.Fatalf("%s returned a different body from the other refusals:\n  %s\n  %s\n"+
				"the difference is an oracle — it tells a caller which IDs are well-formed, "+
				"and from there which are real", name, rec.Body.String(), reference)
		}
	}
}

// The owner gets through, and gets the verified ID in the context. This
// is the positive case, and without it every test above would also pass
// against a middleware that rejected everything.
func TestTheOwnerReachesTheHandlerWithTheVerifiedID(t *testing.T) {
	o := &owner{userID: uuid.New(), profileID: uuid.New()}

	handler, reached, seen, issuer := guarded(t, o)
	rec := get(handler, "/birth-profiles/"+o.profileID.String(), tokenFor(t, issuer, o.userID))

	if rec.Code != http.StatusOK {
		t.Fatalf("the owner got %d, want 200", rec.Code)
	}
	if !*reached {
		t.Fatal("the owner did not reach the handler")
	}
	if *seen != o.profileID {
		t.Fatalf("the handler read profile ID %s from the context, want %s — "+
			"a handler that cannot find the verified ID would fall back to the URL",
			*seen, o.profileID)
	}
}

// An unauthenticated request is turned away, and never costs a lookup.
func TestAnUnauthenticatedRequestIsRefusedBeforeTheLookup(t *testing.T) {
	o := &owner{userID: uuid.New(), profileID: uuid.New()}

	handler, reached, _, _ := guarded(t, o)
	rec := get(handler, "/birth-profiles/"+o.profileID.String(), "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
	if *reached {
		t.Fatal("an unauthenticated request reached the handler")
	}
	if o.calls != 0 {
		t.Fatalf("the ownership lookup ran %d times for an unauthenticated request; "+
			"it should short-circuit before touching the database", o.calls)
	}
}

// The test above passes because auth.Authenticate rejects first, so it
// says nothing about this middleware's own missing-principal branch.
//
// That branch is what fires when the route is mounted OUTSIDE the
// authenticate group — the exact wiring mistake the group exists to
// prevent, and one that would otherwise surface as a nil-principal panic
// or, far worse, as an unauthenticated read. So it gets its own router,
// built wrong on purpose.
func TestTheMiddlewareRefusesEvenWithoutAuthenticateAboveIt(t *testing.T) {
	o := &owner{userID: uuid.New(), profileID: uuid.New()}

	reached := false
	r := chi.NewRouter()
	// Deliberately no auth.Authenticate. This is the mistake.
	r.With(RequireProfileOwnership(o, "id")).
		Get("/birth-profiles/{id}", func(w http.ResponseWriter, _ *http.Request) {
			reached = true
			w.WriteHeader(http.StatusOK)
		})

	rec := get(r, "/birth-profiles/"+o.profileID.String(), "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a route mounted outside the authenticate group returned %d; "+
			"want 401 — 404 would look like a probe in the logs when it is our own "+
			"wiring, and 200 would be an unauthenticated read of a birth profile",
			rec.Code)
	}
	if reached {
		t.Fatal("the handler ran with no authenticated caller at all")
	}
	if o.calls != 0 {
		t.Fatalf("the ownership lookup ran %d times with no principal to check against", o.calls)
	}
}

// A malformed ID must not cost a database round trip. Otherwise the
// endpoint is a free query generator for anyone sending garbage.
func TestAMalformedIDIsRejectedBeforeTheLookup(t *testing.T) {
	o := &owner{userID: uuid.New(), profileID: uuid.New()}

	handler, _, _, issuer := guarded(t, o)
	get(handler, "/birth-profiles/not-a-uuid", tokenFor(t, issuer, o.userID))

	if o.calls != 0 {
		t.Fatalf("the ownership lookup ran %d times for an unparseable id", o.calls)
	}
}
