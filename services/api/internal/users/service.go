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
	"time"

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

// ─── Profile ─────────────────────────────────────────────────────────

var ErrInvalidPreference = errors.New("users: unsupported preference value")

// Profile returns the account as its owner sees it.
func (s *Service) Profile(ctx context.Context, id uuid.UUID) (ProfileView, error) {
	row, err := s.q.FindUserByID(ctx, toPgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProfileView{}, ErrNotFound
		}
		return ProfileView{}, fmt.Errorf("users: find by id: %w", err)
	}
	return toProfileView(row), nil
}

// UpdateProfile writes the fields a user may change about themselves.
//
// Name and gender only. Email and phone are contact methods, not profile
// fields: changing one has to go through verification, or a stolen access
// token becomes permanent account takeover by moving the address the
// codes are sent to.
func (s *Service) UpdateProfile(ctx context.Context, id uuid.UUID, name, gender *string) (ProfileView, error) {
	if gender != nil && !validGenders[*gender] {
		return ProfileView{}, ErrInvalidPreference
	}
	if name != nil && len(*name) > 100 {
		// A length cap rather than a character allowlist. Names contain
		// apostrophes, hyphens, spaces and every script there is;
		// rejecting those breaks real people's names for no security
		// gain, since the value is parameterised and escaped at render.
		return ProfileView{}, ErrInvalidPreference
	}

	row, err := s.q.UpdateUserProfile(ctx, dbgen.UpdateUserProfileParams{
		ID:     toPgUUID(id),
		Name:   name,
		Gender: gender,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProfileView{}, ErrNotFound
		}
		return ProfileView{}, fmt.Errorf("users: update profile: %w", err)
	}
	return toProfileView(row), nil
}

// ─── Preferences ─────────────────────────────────────────────────────

type PreferenceUpdate struct {
	PreferredLanguage *string
	AstrologySystem   *string
	ChartStyle        *string
	Theme             *string
}

// Allowed values, validated in the application rather than by a database
// CHECK constraint: adding a language should not need a migration, and
// the set changes per phase (chart styles matter from Phase 3).
var (
	validLanguages = map[string]bool{"en": true, "hi": true, "hinglish": true}
	validSystems   = map[string]bool{"vedic": true, "western": true}
	validStyles    = map[string]bool{"north": true, "south": true, "east": true}
	validThemes    = map[string]bool{"dark": true, "light": true, "system": true}
	validGenders   = map[string]bool{"male": true, "female": true, "other": true, "prefer_not_to_say": true}
)

func (s *Service) Preferences(ctx context.Context, userID uuid.UUID) (PreferencesView, error) {
	row, err := s.q.GetPreferences(ctx, toPgUUID(userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PreferencesView{}, ErrNotFound
		}
		return PreferencesView{}, fmt.Errorf("users: get preferences: %w", err)
	}
	return toPreferencesView(row), nil
}

func (s *Service) UpdatePreferences(ctx context.Context, userID uuid.UUID, in PreferenceUpdate) (PreferencesView, error) {
	// Validate every supplied field before writing any of them, so a
	// partially-applied update is impossible.
	for _, check := range []struct {
		value   *string
		allowed map[string]bool
	}{
		{in.PreferredLanguage, validLanguages},
		{in.AstrologySystem, validSystems},
		{in.ChartStyle, validStyles},
		{in.Theme, validThemes},
	} {
		if check.value != nil && !check.allowed[*check.value] {
			return PreferencesView{}, ErrInvalidPreference
		}
	}

	row, err := s.q.UpdatePreferences(ctx, dbgen.UpdatePreferencesParams{
		UserID:            toPgUUID(userID),
		PreferredLanguage: in.PreferredLanguage,
		AstrologySystem:   in.AstrologySystem,
		ChartStyle:        in.ChartStyle,
		Theme:             in.Theme,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PreferencesView{}, ErrNotFound
		}
		return PreferencesView{}, fmt.Errorf("users: update preferences: %w", err)
	}
	return toPreferencesView(row), nil
}

// ─── views ───────────────────────────────────────────────────────────

type ProfileView struct {
	ID            uuid.UUID `json:"id"`
	Email         *string   `json:"email"`
	EmailVerified bool      `json:"email_verified"`
	Phone         *string   `json:"phone"`
	PhoneVerified bool      `json:"phone_verified"`
	Name          *string   `json:"name"`
	Gender        *string   `json:"gender"`
	Role          string    `json:"role"`
	CreatedAt     time.Time `json:"created_at"`
}

type PreferencesView struct {
	PreferredLanguage string `json:"preferred_language"`
	AstrologySystem   string `json:"astrology_system"`
	ChartStyle        string `json:"chart_style"`
	Theme             string `json:"theme"`
}

func toProfileView(row dbgen.User) ProfileView {
	return ProfileView{
		ID:            uuid.UUID(row.ID.Bytes),
		Email:         row.Email,
		EmailVerified: row.EmailVerified,
		Phone:         row.Phone,
		PhoneVerified: row.PhoneVerified,
		Name:          row.Name,
		Gender:        row.Gender,
		Role:          string(row.Role),
		CreatedAt:     row.CreatedAt,
	}
}

func toPreferencesView(row dbgen.UserPreference) PreferencesView {
	return PreferencesView{
		PreferredLanguage: row.PreferredLanguage,
		AstrologySystem:   row.AstrologySystem,
		ChartStyle:        row.ChartStyle,
		Theme:             row.Theme,
	}
}
