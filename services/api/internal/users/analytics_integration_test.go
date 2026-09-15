//go:build integration

package users

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/analytics"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// Are the events actually wired?
//
// The analytics package's own tests prove the emitter refuses PII and
// unknown names. They cannot prove that anything ever calls it — and a
// vocabulary that nothing emits is the same as no analytics at all, but
// with a passing test suite saying otherwise. So this drives real service
// calls against real Postgres and asserts on what came out.

// recorder captures events instead of writing them.
type recorder struct {
	mu     sync.Mutex
	events []captured
}

type captured struct {
	name   string
	userID *uuid.UUID
	props  map[string]any
}

func (r *recorder) Emit(_ context.Context, name string, userID *uuid.UUID, props map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, captured{name: name, userID: userID, props: props})
}

func (r *recorder) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.events))
	for _, e := range r.events {
		out = append(out, e.name)
	}
	return out
}

func (r *recorder) find(t *testing.T, name string) captured {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.name == name {
			return e
		}
	}
	t.Fatalf("%q was never emitted; got %v", name, r.names())
	return captured{}
}

func TestPreferencesUpdateEmitsTheChangedFieldNames(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	q := dbgen.New(pool)
	rec := &recorder{}
	svc := NewService(q).WithAnalytics(rec)

	user, _, err := svc.FindOrCreateByIdentity(ctx, findParams("analytics-prefs@example.com"))
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	language, theme := "hi", "dark"
	if _, err := svc.UpdatePreferences(ctx, user.ID, PreferenceUpdate{
		PreferredLanguage: &language,
		Theme:             &theme,
	}); err != nil {
		t.Fatalf("update preferences: %v", err)
	}

	event := rec.find(t, analytics.PreferencesUpdated)

	fields, ok := event.props["fields"].([]string)
	if !ok {
		t.Fatalf("fields is %T, want []string", event.props["fields"])
	}
	want := []string{"preferred_language", "theme"}
	if strings.Join(fields, ",") != strings.Join(want, ",") {
		t.Errorf("fields = %v, want %v", fields, want)
	}

	// The VALUES must not be there. Which preferences people change is the
	// product question; what they changed them to is on the row already.
	for key, value := range event.props {
		if key != "fields" {
			t.Errorf("unexpected property %q = %v", key, value)
		}
	}
	if strings.Contains(strings.Join(fields, ","), "hi") {
		t.Error("a preference value leaked into the field list")
	}
}

// A gender-only edit from the settings screen is not an onboarding
// completion, and counting it as one makes the funnel report more
// completions than there were users.
func TestOnboardingEventFiresOnlyForAName(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	q := dbgen.New(pool)
	rec := &recorder{}
	svc := NewService(q).WithAnalytics(rec)

	user, _, err := svc.FindOrCreateByIdentity(ctx, findParams("analytics-name@example.com"))
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	gender := "female"
	if _, err := svc.UpdateProfile(ctx, user.ID, nil, &gender); err != nil {
		t.Fatalf("update gender: %v", err)
	}
	for _, name := range rec.names() {
		if name == analytics.OnboardingNameCompleted {
			t.Fatal("a gender-only edit counted as onboarding completion")
		}
	}

	name := "Priya"
	if _, err := svc.UpdateProfile(ctx, user.ID, &name, nil); err != nil {
		t.Fatalf("update name: %v", err)
	}

	event := rec.find(t, analytics.OnboardingNameCompleted)
	if event.userID == nil || *event.userID != user.ID {
		t.Error("the event does not identify the user")
	}
	// The name itself must not travel with it.
	for key := range event.props {
		t.Errorf("unexpected property %q — the name must not be attached", key)
	}
}

// Revoking a device that is not yours returns ErrNotFound. It must not
// also emit, or the funnel counts failed cross-user probes as users
// managing their devices.
func TestSessionRevokedEventFiresOnlyOnARealRevocation(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	q := dbgen.New(pool)
	rec := &recorder{}
	directory := NewSessionDirectory(q).WithAnalytics(rec)

	if err := directory.Revoke(ctx, uuid.New(), uuid.New()); err == nil {
		t.Fatal("revoking a device that does not exist succeeded")
	}

	for _, name := range rec.names() {
		if name == analytics.SessionRevoked {
			t.Fatal("a failed revocation emitted session_revoked")
		}
	}
}

func findParams(email string) auth.FindOrCreateParams {
	return auth.FindOrCreateParams{Channel: "email", Identifier: email}
}

// account_deleted fires in the WORKER, after the grace window, not in the
// API. The API only marks an account; this is the event that says the
// rows are actually gone.
//
// It is emitted after HardDeleteUser succeeds and carries only the id —
// which by then references nothing. That is the point: the event records
// that an account was deleted without preserving anything about whose it
// was.
func TestAccountDeletedFiresFromTheWorkerAfterTheGraceWindow(t *testing.T) {
	ctx := context.Background()
	pool, stop := startPostgres(ctx, t)
	defer stop()

	q := dbgen.New(pool)
	rec := &recorder{}

	// A zero grace window, so the worker's cutoff is "now" and the
	// deletion is due the moment it is requested.
	deleter := NewDeleter(q, NewSessionDirectory(q), 0, nil).WithAnalytics(rec)
	svc := NewService(q)

	user, _, err := svc.FindOrCreateByIdentity(ctx, findParams("analytics-deleted@example.com"))
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if _, err := deleter.Request(ctx, user.ID); err != nil {
		t.Fatalf("request deletion: %v", err)
	}

	// Requesting alone must NOT emit account_deleted — the rows are still
	// there and the window is still cancellable.
	for _, name := range rec.names() {
		if name == analytics.AccountDeleted {
			t.Fatal("account_deleted fired while the account still existed")
		}
	}

	deleted, err := deleter.RunHardDeletes(ctx)
	if err != nil {
		t.Fatalf("RunHardDeletes: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("%d accounts were deleted, want 1", deleted)
	}

	event := rec.find(t, analytics.AccountDeleted)
	if event.userID == nil || *event.userID != user.ID {
		t.Error("the event does not identify which account was deleted")
	}
	if len(event.props) != 0 {
		t.Errorf("unexpected properties %v — nothing about the person may survive", event.props)
	}
}
