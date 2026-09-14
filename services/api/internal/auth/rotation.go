package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrRefreshInvalid means the token does not correspond to any
	// session. Returned for an unknown token, an expired one, and a
	// revoked one alike — the client can do nothing differently, and
	// distinguishing them tells a probe which guesses were closer.
	ErrRefreshInvalid = errors.New("auth: refresh token is invalid")

	// ErrRefreshReused means a token that had already been exchanged was
	// presented again. The family is revoked before this is returned.
	//
	// Surfaced separately from ErrRefreshInvalid because the two mean
	// different things operationally: the first is routine (an old tab,
	// an expired session), the second is a signal that a token leaked and
	// deserves an alert.
	ErrRefreshReused = errors.New("auth: refresh token was reused")
)

// Session is the subset of a sessions row this package needs.
//
// Declared here rather than using the sqlc struct directly so the
// rotation logic can be tested without a database, and so a schema
// change does not silently alter this package's contract.
type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	FamilyID  uuid.UUID
	ExpiresAt time.Time
	UsedAt    *time.Time
	RevokedAt *time.Time
}

// SessionStore is what rotation needs from persistence.
//
// The consumer declares the interface, per the Go rules: `auth` never
// imports another domain's internals, and this is trivially faked in a
// test without a database.
type SessionStore interface {
	// Rotate atomically marks a token used and returns its session, but
	// ONLY if it was unused, unrevoked and unexpired. It must return
	// ErrRefreshInvalid (wrapped or exact) when no row matched.
	//
	// The atomicity is not optional and cannot be emulated in Go — see
	// RotateRefreshToken in db/queries/auth.sql.
	Rotate(ctx context.Context, hash []byte) (Session, error)

	// FindByHash locates a session regardless of its state. Used only on
	// the reuse path, to discover which family to revoke.
	FindByHash(ctx context.Context, hash []byte) (Session, error)

	// RevokeFamily revokes every session sharing a lineage.
	RevokeFamily(ctx context.Context, familyID uuid.UUID) error

	// Create persists a newly issued refresh token.
	Create(ctx context.Context, s NewSession) (Session, error)

	// RevokeSession ends one session, scoped by owner.
	RevokeSession(ctx context.Context, sessionID, userID uuid.UUID) error
}

// NewSession is the input to Create.
type NewSession struct {
	UserID      uuid.UUID
	FamilyID    uuid.UUID
	RefreshHash []byte
	UserAgent   string
	IPHash      []byte
	ExpiresAt   time.Time
}

// Rotator exchanges a refresh token for a new pair.
type Rotator struct {
	issuer *Issuer
	store  SessionStore
	now    func() time.Time
}

func NewRotator(issuer *Issuer, store SessionStore) *Rotator {
	return &Rotator{issuer: issuer, store: store}
}

func (r *Rotator) nowFunc() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// Issue creates the first session of a new family.
//
// Called after a successful OTP or OAuth login — the point at which a
// lineage begins.
func (r *Rotator) Issue(ctx context.Context, userID uuid.UUID, role, userAgent string, ipHash []byte) (TokenPair, error) {
	familyID, err := uuid.NewRandom()
	if err != nil {
		return TokenPair{}, fmt.Errorf("auth: generate family id: %w", err)
	}
	return r.issueInFamily(ctx, userID, role, familyID, userAgent, ipHash)
}

func (r *Rotator) issueInFamily(
	ctx context.Context,
	userID uuid.UUID,
	role string,
	familyID uuid.UUID,
	userAgent string,
	ipHash []byte,
) (TokenPair, error) {
	access, err := r.issuer.IssueAccessToken(userID, role)
	if err != nil {
		return TokenPair{}, err
	}

	refresh, hash, err := GenerateRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}

	expiresAt := r.nowFunc().Add(r.issuer.RefreshTTL())

	if _, err := r.store.Create(ctx, NewSession{
		UserID:      userID,
		FamilyID:    familyID,
		RefreshHash: hash,
		UserAgent:   userAgent,
		IPHash:      ipHash,
		ExpiresAt:   expiresAt,
	}); err != nil {
		return TokenPair{}, fmt.Errorf("auth: persist session: %w", err)
	}

	return TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		FamilyID:     familyID,
		ExpiresAt:    expiresAt,
	}, nil
}

// Rotate exchanges a refresh token for a new pair, detecting reuse.
//
// The whole security property of this phase is in the ordering below.
//
//  1. Attempt the atomic rotate. Exactly one caller can win, because the
//     UPDATE's WHERE clause and its write are one statement.
//  2. If nothing matched, the token is unknown, expired, revoked — or it
//     was already spent. Only the last case is interesting.
//  3. Look it up ignoring state. If it exists and has been used, it
//     leaked: the legitimate holder already exchanged it, so whoever is
//     presenting it now is not the legitimate holder. Revoke the entire
//     family and force re-authentication.
//
// Revoking the family rather than the single token is what makes this
// worth doing. An attacker who steals a token and refreshes first would
// otherwise hold a valid chain forever while the victim is merely logged
// out once and re-authenticates without ever knowing.
func (r *Rotator) Rotate(
	ctx context.Context,
	presented, role, userAgent string,
	ipHash []byte,
) (TokenPair, error) {
	hash := HashRefreshToken(presented)

	session, err := r.store.Rotate(ctx, hash)
	if err == nil {
		// Won the race. Continue the same lineage so a later leak can
		// still be traced back through it.
		return r.issueInFamily(ctx, session.UserID, role, session.FamilyID, userAgent, ipHash)
	}
	if !errors.Is(err, ErrRefreshInvalid) {
		// A genuine infrastructure failure. Do NOT treat it as reuse:
		// revoking a family because Postgres blipped would log every
		// device out over a transient error.
		return TokenPair{}, fmt.Errorf("auth: rotate: %w", err)
	}

	// Nothing matched. Determine whether this was a spent token.
	existing, findErr := r.store.FindByHash(ctx, hash)
	if findErr != nil {
		// Unknown token. Nothing to revoke, nothing to alert on.
		return TokenPair{}, ErrRefreshInvalid
	}

	if existing.UsedAt != nil {
		// Reuse. Revoke first, then report — if the revocation fails the
		// caller must learn about that rather than being told the token
		// was merely invalid.
		if err := r.store.RevokeFamily(ctx, existing.FamilyID); err != nil {
			return TokenPair{}, fmt.Errorf("auth: revoke family after detecting reuse: %w", err)
		}
		return TokenPair{}, ErrRefreshReused
	}

	// Exists, never used: expired or already revoked. Routine.
	return TokenPair{}, ErrRefreshInvalid
}

// RevokeByToken revokes the single session a token belongs to.
//
// Deliberately not the family. Logging out is routine; revoking the
// lineage is what happens when reuse is DETECTED, and conflating the two
// would sign someone out everywhere each time they signed out anywhere.
func (r *Rotator) RevokeByToken(ctx context.Context, presented string) error {
	session, err := r.store.FindByHash(ctx, HashRefreshToken(presented))
	if err != nil {
		// An unknown token is not an error worth surfacing: the caller is
		// logging out, and the outcome they want has already happened.
		return nil
	}
	return r.store.RevokeSession(ctx, session.ID, session.UserID)
}
