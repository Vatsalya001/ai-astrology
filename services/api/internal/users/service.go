// Package users owns the account record and its preferences.
//
// It implements auth.UserCreator. The dependency runs users → auth, never
// the other way: `auth` declares the interface it consumes, which is what
// keeps the graph acyclic.
package users

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

var ErrNotFound = errors.New("users: not found")

// Service is the users business logic.
type Service struct {
	q dbgen.Querier
}

func NewService(q dbgen.Querier) *Service {
	return &Service{q: q}
}

var _ auth.UserCreator = (*Service)(nil)

// FindOrCreateByIdentity returns the account for a verified identifier,
// creating one on first login.
//
// The identifier arrives already verified — the OTP was consumed — so the
// corresponding `*_verified` column is set true at creation. Setting it
// later would leave a window where a real account exists with an
// unverified contact, which is exactly the state an attacker wants.
func (s *Service) FindOrCreateByIdentity(
	ctx context.Context,
	p auth.FindOrCreateParams,
) (auth.User, bool, error) {
	existing, err := s.findByIdentifier(ctx, p.Channel, p.Identifier)
	switch {
	case err == nil:
		return existing, false, nil
	case errors.Is(err, ErrNotFound):
		// Fall through to creation.
	default:
		return auth.User{}, false, err
	}

	created, err := s.create(ctx, p.Channel, p.Identifier)
	if err != nil {
		// Two concurrent first logins for the same identifier: one wins
		// the unique index, the other must find the winner's row rather
		// than failing. Without this, tapping "verify" twice quickly on a
		// new account returns an error on the second.
		if isUniqueViolation(err) {
			recovered, findErr := s.findByIdentifier(ctx, p.Channel, p.Identifier)
			if findErr != nil {
				return auth.User{}, false, fmt.Errorf("users: recover from race: %w", findErr)
			}
			return recovered, false, nil
		}
		return auth.User{}, false, err
	}

	// Preferences are created alongside the account so every later read
	// can assume a row exists, rather than every caller handling absence.
	if _, err := s.q.CreateDefaultPreferences(ctx, toPgUUID(created.ID)); err != nil {
		return auth.User{}, false, fmt.Errorf("users: create default preferences: %w", err)
	}

	if _, err := s.q.LinkIdentity(ctx, dbgen.LinkIdentityParams{
		UserID:         toPgUUID(created.ID),
		Provider:       dbgen.AuthProvider(p.Channel),
		ProviderUserID: p.Identifier,
	}); err != nil {
		return auth.User{}, false, fmt.Errorf("users: link identity: %w", err)
	}

	return created, true, nil
}

func (s *Service) findByIdentifier(ctx context.Context, channel, identifier string) (auth.User, error) {
	var (
		row dbgen.User
		err error
	)

	switch channel {
	case auth.ChannelEmail:
		row, err = s.q.FindUserByEmail(ctx, &identifier)
	case auth.ChannelPhone:
		row, err = s.q.FindUserByPhone(ctx, &identifier)
	default:
		return auth.User{}, fmt.Errorf("users: unknown channel %q", channel)
	}

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.User{}, ErrNotFound
		}
		return auth.User{}, fmt.Errorf("users: find by %s: %w", channel, err)
	}
	return toAuthUser(row), nil
}

func (s *Service) create(ctx context.Context, channel, identifier string) (auth.User, error) {
	var (
		row dbgen.User
		err error
	)

	switch channel {
	case auth.ChannelEmail:
		row, err = s.q.CreateUserWithEmail(ctx, &identifier)
	case auth.ChannelPhone:
		row, err = s.q.CreateUserWithPhone(ctx, &identifier)
	default:
		return auth.User{}, fmt.Errorf("users: unknown channel %q", channel)
	}

	if err != nil {
		return auth.User{}, fmt.Errorf("users: create with %s: %w", channel, err)
	}
	return toAuthUser(row), nil
}

// TouchLastLogin records a successful sign-in.
//
// Best-effort by design: a failure here must not fail the login. The
// field drives analytics and dormancy reports, neither of which is worth
// refusing a user entry over.
func (s *Service) TouchLastLogin(ctx context.Context, id uuid.UUID) error {
	if err := s.q.TouchLastLogin(ctx, toPgUUID(id)); err != nil {
		return fmt.Errorf("users: touch last login: %w", err)
	}
	return nil
}

// ─── conversions ─────────────────────────────────────────────────────

func toAuthUser(row dbgen.User) auth.User {
	return auth.User{
		ID:     uuid.UUID(row.ID.Bytes),
		Role:   string(row.Role),
		Status: row.Status,
	}
}

func toPgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

// isUniqueViolation reports a Postgres 23505.
//
// Matched on the SQLSTATE rather than the message: message text is
// localised and changes between versions, so string matching works until
// someone upgrades Postgres.
func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23505"
}
