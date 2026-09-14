package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// PostgresSessionStore is the real SessionStore.
//
// A thin adapter: it translates between the generated types and this
// package's own, and maps pgx.ErrNoRows onto ErrRefreshInvalid. All the
// safety lives in the SQL — see RotateRefreshToken in db/queries/auth.sql.
type PostgresSessionStore struct {
	q dbgen.Querier
}

func NewPostgresSessionStore(q dbgen.Querier) *PostgresSessionStore {
	return &PostgresSessionStore{q: q}
}

var _ SessionStore = (*PostgresSessionStore)(nil)

func (s *PostgresSessionStore) Create(ctx context.Context, in NewSession) (Session, error) {
	var userAgent *string
	if in.UserAgent != "" {
		userAgent = &in.UserAgent
	}

	row, err := s.q.CreateSession(ctx, dbgen.CreateSessionParams{
		UserID:      toPgUUID(in.UserID),
		FamilyID:    toPgUUID(in.FamilyID),
		RefreshHash: in.RefreshHash,
		UserAgent:   userAgent,
		IpHash:      in.IPHash,
		ExpiresAt:   in.ExpiresAt,
	})
	if err != nil {
		return Session{}, fmt.Errorf("auth: create session: %w", err)
	}
	return fromDB(row), nil
}

// Rotate runs the atomic check-and-update.
//
// pgx.ErrNoRows is the expected outcome for every unusable token —
// unknown, already used, revoked, expired — and the caller distinguishes
// them afterwards. Mapping it to ErrRefreshInvalid here keeps the SQL
// contract described in the interface honest.
func (s *PostgresSessionStore) Rotate(ctx context.Context, hash []byte) (Session, error) {
	row, err := s.q.RotateRefreshToken(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, ErrRefreshInvalid
		}
		// A real failure. Must NOT be reported as ErrRefreshInvalid, or
		// the rotator would go looking for reuse and could revoke a
		// family over a transient database error.
		return Session{}, fmt.Errorf("auth: rotate refresh token: %w", err)
	}
	return fromDB(row), nil
}

func (s *PostgresSessionStore) FindByHash(ctx context.Context, hash []byte) (Session, error) {
	row, err := s.q.FindSessionByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, ErrRefreshInvalid
		}
		return Session{}, fmt.Errorf("auth: find session: %w", err)
	}
	return fromDB(row), nil
}

// RevokeSession ends one session, scoped by owner.
//
// The query carries user_id in its WHERE clause, so a caller cannot
// revoke a session belonging to someone else even with a valid id.
func (s *PostgresSessionStore) RevokeSession(ctx context.Context, sessionID, userID uuid.UUID) error {
	// The row count is ignored here, deliberately. This path is logout:
	// a session that is already gone is the outcome the caller wanted,
	// and failing would show an error on a screen that has, for them,
	// already worked. The users handler DOES check it, because there the
	// count is the difference between 204 and 404.
	if _, err := s.q.RevokeSession(ctx, dbgen.RevokeSessionParams{
		ID:     toPgUUID(sessionID),
		UserID: toPgUUID(userID),
	}); err != nil {
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	return nil
}

func (s *PostgresSessionStore) RevokeFamily(ctx context.Context, familyID uuid.UUID) error {
	if err := s.q.RevokeTokenFamily(ctx, toPgUUID(familyID)); err != nil {
		return fmt.Errorf("auth: revoke token family: %w", err)
	}
	return nil
}

// ─── conversions ─────────────────────────────────────────────────────

func fromDB(row dbgen.Session) Session {
	return Session{
		ID:        fromPgUUID(row.ID),
		UserID:    fromPgUUID(row.UserID),
		FamilyID:  fromPgUUID(row.FamilyID),
		ExpiresAt: row.ExpiresAt,
		UsedAt:    fromPgTimestamp(row.UsedAt),
		RevokedAt: fromPgTimestamp(row.RevokedAt),
	}
}

func toPgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func fromPgUUID(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return uuid.UUID(id.Bytes)
}

// fromPgTimestamp returns nil for SQL NULL.
//
// The distinction is load-bearing: `used_at IS NULL` is what separates a
// live refresh token from a spent one, so collapsing NULL to the zero
// time would make every unused token look used.
func fromPgTimestamp(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}
