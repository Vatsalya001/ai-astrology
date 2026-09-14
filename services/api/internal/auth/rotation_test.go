package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeSessionStore emulates the SQL contract in memory.
//
// A fake, not a mock: it enforces the same atomicity the real UPDATE
// does, under a mutex. That makes the state machine testable without
// Docker — but it cannot prove the SQL is atomic, only that the Go logic
// around it is right. The SQL itself is covered by the integration test,
// which is the one that would catch a broken WHERE clause.
type fakeSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*Session // keyed by hex of refresh hash
	now      func() time.Time

	rotateErr error // injectable infrastructure failure
	revokeErr error
	revoked   []uuid.UUID
}

func newFakeStore() *fakeSessionStore {
	return &fakeSessionStore{
		sessions: map[string]*Session{},
		now:      time.Now,
	}
}

func key(hash []byte) string { return string(hash) }

func (f *fakeSessionStore) Create(_ context.Context, s NewSession) (Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	session := Session{
		ID:        uuid.New(),
		UserID:    s.UserID,
		FamilyID:  s.FamilyID,
		ExpiresAt: s.ExpiresAt,
	}
	f.sessions[key(s.RefreshHash)] = &session
	return session, nil
}

// Rotate mirrors `UPDATE ... WHERE used_at IS NULL AND revoked_at IS NULL
// AND expires_at > now() RETURNING *` — the check and the write under one
// lock, exactly as Postgres serialises the statement.
func (f *fakeSessionStore) Rotate(_ context.Context, hash []byte) (Session, error) {
	if f.rotateErr != nil {
		return Session{}, f.rotateErr
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	s, ok := f.sessions[key(hash)]
	if !ok {
		return Session{}, ErrRefreshInvalid
	}
	if s.UsedAt != nil || s.RevokedAt != nil || !s.ExpiresAt.After(f.now()) {
		return Session{}, ErrRefreshInvalid
	}

	used := f.now()
	s.UsedAt = &used
	return *s, nil
}

func (f *fakeSessionStore) FindByHash(_ context.Context, hash []byte) (Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	s, ok := f.sessions[key(hash)]
	if !ok {
		return Session{}, errors.New("not found")
	}
	return *s, nil
}

func (f *fakeSessionStore) RevokeFamily(_ context.Context, familyID uuid.UUID) error {
	if f.revokeErr != nil {
		return f.revokeErr
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.revoked = append(f.revoked, familyID)
	revoked := f.now()
	for _, s := range f.sessions {
		if s.FamilyID == familyID && s.RevokedAt == nil {
			s.RevokedAt = &revoked
		}
	}
	return nil
}

func (f *fakeSessionStore) revokedFamilies() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uuid.UUID(nil), f.revoked...)
}

func newRotator(t *testing.T) (*Rotator, *fakeSessionStore) {
	t.Helper()
	store := newFakeStore()
	return NewRotator(testIssuer(t), store), store
}

// ─── Happy path ──────────────────────────────────────────────────────

func TestIssueCreatesASessionAndAFamily(t *testing.T) {
	rot, store := newRotator(t)
	userID := uuid.New()

	pair, err := rot.Issue(context.Background(), userID, "user", "Mozilla/5.0", nil)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("Issue returned an empty token")
	}
	if pair.FamilyID == uuid.Nil {
		t.Error("FamilyID is nil; reuse detection would have nothing to revoke")
	}
	if len(store.sessions) != 1 {
		t.Errorf("%d sessions persisted, want 1", len(store.sessions))
	}
}

func TestRotateIssuesNewTokensAndKeepsTheFamily(t *testing.T) {
	rot, _ := newRotator(t)
	userID := uuid.New()

	first, err := rot.Issue(context.Background(), userID, "user", "ua", nil)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	second, err := rot.Rotate(context.Background(), first.RefreshToken, "user", "ua", nil)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	if second.RefreshToken == first.RefreshToken {
		t.Error("rotation returned the same refresh token; it must be replaced")
	}
	if second.AccessToken == first.AccessToken {
		t.Error("rotation returned the same access token")
	}
	// The lineage must survive, or a leak detected three rotations later
	// could not be traced back to revoke everything.
	if second.FamilyID != first.FamilyID {
		t.Errorf("family changed across rotation: %s → %s", first.FamilyID, second.FamilyID)
	}
}

func TestRotationChainsRepeatedly(t *testing.T) {
	rot, _ := newRotator(t)
	ctx := context.Background()

	pair, err := rot.Issue(ctx, uuid.New(), "user", "ua", nil)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	family := pair.FamilyID

	for i := range 10 {
		pair, err = rot.Rotate(ctx, pair.RefreshToken, "user", "ua", nil)
		if err != nil {
			t.Fatalf("rotation %d: %v", i+1, err)
		}
		if pair.FamilyID != family {
			t.Fatalf("rotation %d changed the family", i+1)
		}
	}
}

// ─── Reuse detection ─────────────────────────────────────────────────

// The property this whole phase is built around.
//
// An attacker who steals a refresh token and uses it first would
// otherwise hold a valid chain forever, while the victim is logged out
// once and re-authenticates without ever knowing. Revoking the family
// turns a silent compromise into a visible one.
func TestReusingASpentTokenRevokesTheWholeFamily(t *testing.T) {
	rot, store := newRotator(t)
	ctx := context.Background()

	first, err := rot.Issue(ctx, uuid.New(), "user", "ua", nil)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	second, err := rot.Rotate(ctx, first.RefreshToken, "user", "ua", nil)
	if err != nil {
		t.Fatalf("first rotation: %v", err)
	}

	// The attacker presents the already-spent token.
	_, err = rot.Rotate(ctx, first.RefreshToken, "user", "attacker-ua", nil)
	if !errors.Is(err, ErrRefreshReused) {
		t.Fatalf("reuse returned %v, want ErrRefreshReused", err)
	}

	revoked := store.revokedFamilies()
	if len(revoked) != 1 || revoked[0] != first.FamilyID {
		t.Fatalf("revoked families = %v, want exactly [%s]", revoked, first.FamilyID)
	}

	// And the consequence that matters: the CURRENT token — the one the
	// legitimate user holds — must now be dead too. Revoking only the
	// leaked token would leave the attacker's chain alive.
	if _, err := rot.Rotate(ctx, second.RefreshToken, "user", "ua", nil); err == nil {
		t.Error("the legitimate current token still works after a reuse was detected")
	}
}

// Distinguishing "unknown" from "expired" from "revoked" tells a prober
// which guesses were closer. They collapse to one error.
func TestUnknownTokenIsInvalidWithoutRevokingAnything(t *testing.T) {
	rot, store := newRotator(t)

	_, err := rot.Rotate(context.Background(), "a-token-that-was-never-issued", "user", "ua", nil)
	if !errors.Is(err, ErrRefreshInvalid) {
		t.Errorf("got %v, want ErrRefreshInvalid", err)
	}
	if got := store.revokedFamilies(); len(got) != 0 {
		t.Errorf("an unknown token revoked %v — nothing should be revoked", got)
	}
}

func TestExpiredTokenIsInvalidNotReuse(t *testing.T) {
	rot, store := newRotator(t)
	ctx := context.Background()

	base := time.Now()
	store.now = func() time.Time { return base }
	rot.now = func() time.Time { return base }

	pair, err := rot.Issue(ctx, uuid.New(), "user", "ua", nil)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Past the refresh TTL.
	store.now = func() time.Time { return base.Add(721 * time.Hour) }
	rot.now = func() time.Time { return base.Add(721 * time.Hour) }

	_, err = rot.Rotate(ctx, pair.RefreshToken, "user", "ua", nil)
	if !errors.Is(err, ErrRefreshInvalid) {
		t.Errorf("expired token returned %v, want ErrRefreshInvalid", err)
	}
	// Expiry is routine, not a security event. Revoking a family over it
	// would log every device out whenever someone came back after a
	// month away.
	if got := store.revokedFamilies(); len(got) != 0 {
		t.Errorf("an expired token revoked %v", got)
	}
}

func TestRevokedTokenIsInvalidNotReuse(t *testing.T) {
	rot, store := newRotator(t)
	ctx := context.Background()

	pair, err := rot.Issue(ctx, uuid.New(), "user", "ua", nil)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if err := store.RevokeFamily(ctx, pair.FamilyID); err != nil {
		t.Fatalf("RevokeFamily: %v", err)
	}
	before := len(store.revokedFamilies())

	_, err = rot.Rotate(ctx, pair.RefreshToken, "user", "ua", nil)
	if !errors.Is(err, ErrRefreshInvalid) {
		t.Errorf("revoked token returned %v, want ErrRefreshInvalid", err)
	}
	// It was never used, so this is a logged-out device, not a leak.
	if after := len(store.revokedFamilies()); after != before {
		t.Error("a revoked-but-unused token triggered another family revocation")
	}
}

// A transient database failure must not be read as an attack. Revoking a
// family because Postgres blipped would log every device out over a
// retryable error.
func TestInfrastructureFailureIsNotTreatedAsReuse(t *testing.T) {
	rot, store := newRotator(t)
	ctx := context.Background()

	pair, err := rot.Issue(ctx, uuid.New(), "user", "ua", nil)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	store.rotateErr = errors.New("connection reset by peer")

	_, err = rot.Rotate(ctx, pair.RefreshToken, "user", "ua", nil)
	if errors.Is(err, ErrRefreshReused) {
		t.Fatal("a database error was reported as token reuse")
	}
	if err == nil {
		t.Fatal("a database error was swallowed")
	}
	if got := store.revokedFamilies(); len(got) != 0 {
		t.Errorf("a database error revoked %v", got)
	}
}

// If the revocation itself fails, the caller must hear about it rather
// than being told the token was merely invalid — otherwise a detected
// leak is silently not acted upon.
func TestFailedRevocationSurfacesRatherThanBeingSwallowed(t *testing.T) {
	rot, store := newRotator(t)
	ctx := context.Background()

	first, err := rot.Issue(ctx, uuid.New(), "user", "ua", nil)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := rot.Rotate(ctx, first.RefreshToken, "user", "ua", nil); err != nil {
		t.Fatalf("first rotation: %v", err)
	}

	store.revokeErr = errors.New("deadlock detected")

	_, err = rot.Rotate(ctx, first.RefreshToken, "user", "ua", nil)
	if err == nil {
		t.Fatal("a failed family revocation was swallowed")
	}
	if errors.Is(err, ErrRefreshInvalid) {
		t.Error("a failed revocation was reported as a merely-invalid token")
	}
}

// Concurrency at the Go level, under -race.
//
// Two tabs refreshing at once is routine, not an attack. Exactly one must
// win; the loser sees reuse because it presented a token the winner had
// already spent. The important assertion is that exactly one succeeds —
// two successes would mean the token was consumed twice.
func TestConcurrentRotationYieldsExactlyOneWinner(t *testing.T) {
	rot, _ := newRotator(t)
	ctx := context.Background()

	pair, err := rot.Issue(ctx, uuid.New(), "user", "ua", nil)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	const goroutines = 16
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		wins    int
		reuses  int
		others  int
		release = make(chan struct{})
	)

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-release
			_, err := rot.Rotate(ctx, pair.RefreshToken, "user", "ua", nil)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.Is(err, ErrRefreshReused):
				reuses++
			default:
				others++
			}
		}()
	}
	close(release)
	wg.Wait()

	if wins != 1 {
		t.Fatalf("%d of %d concurrent rotations succeeded; exactly 1 must", wins, goroutines)
	}
	if wins+reuses+others != goroutines {
		t.Fatalf("accounting lost a result: %d + %d + %d != %d", wins, reuses, others, goroutines)
	}
}
