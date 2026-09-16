// Package birthprofiles owns the most sensitive table in the product.
//
// Birth date plus birth time plus birth place is, in combination, close
// to a unique identifier. The project's security rules say to treat it
// exactly like an email address: never logged, never in an analytics
// payload, never in a URL query string, never in a push notification.
//
// Nothing in this package logs a profile. IDs only.
package birthprofiles

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/analytics"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

var (
	ErrNotFound     = errors.New("birthprofiles: not found")
	ErrInvalidInput = errors.New("birthprofiles: invalid input")
	ErrUnknownPlace = errors.New("birthprofiles: unknown place")
)

// TimeAccuracy values, matching the CHECK constraint on the column.
const (
	AccuracyExact       = "exact"
	AccuracyApproximate = "approximate"
	AccuracyUnknown     = "unknown"
)

// CreateInput is what the API layer collected.
//
// The LOCAL date and clock time, plus a place. The UTC instant is derived
// here rather than accepted from the client: a browser cannot be trusted
// to resolve a 1943 Indian wartime offset, and if it could, two clients
// would eventually disagree.
type CreateInput struct {
	UserID uuid.UUID
	Label  string

	BirthDate time.Time
	// BirthTime is the local clock time. Ignored when TimeAccuracy is
	// "unknown", which is the only case where it may be absent.
	BirthTime    time.Duration
	HasBirthTime bool
	TimeAccuracy string

	PlaceName string
	Latitude  float64
	Longitude float64
	Timezone  string
}

// Profile is a birth profile as callers see it.
type Profile struct {
	ID           uuid.UUID  `json:"id"`
	Label        string     `json:"label"`
	BirthDate    string     `json:"birth_date"`
	BirthTime    *string    `json:"birth_time"`
	TimeAccuracy string     `json:"time_accuracy"`
	BirthPlace   string     `json:"birth_place"`
	Latitude     float64    `json:"latitude"`
	Longitude    float64    `json:"longitude"`
	Timezone     string     `json:"timezone"`
	UTCOffsetMin int32      `json:"utc_offset_min"`
	UTCInstant   time.Time  `json:"utc_instant"`
	Version      int32      `json:"version"`
	IsActive     bool       `json:"is_active"`
	SupersededBy *uuid.UUID `json:"superseded_by"`
	CreatedAt    time.Time  `json:"created_at"`
}

type Service struct {
	q      dbgen.Querier
	events analytics.Emitter
}

func NewService(q dbgen.Querier) *Service {
	return &Service{q: q, events: analytics.Nop{}}
}

func (s *Service) WithAnalytics(events analytics.Emitter) *Service {
	if events != nil {
		s.events = events
	}
	return s
}

// Create stores a new profile at version 1.
func (s *Service) Create(ctx context.Context, in CreateInput) (Profile, error) {
	resolved, err := s.resolve(in)
	if err != nil {
		return Profile{}, err
	}

	row, err := s.q.CreateBirthProfile(ctx, s.params(in, resolved, 1))
	if err != nil {
		return Profile{}, fmt.Errorf("birthprofiles: create: %w", err)
	}

	// The accuracy enum, never the date, the time or the place. Those
	// three together identify a person.
	s.events.Emit(ctx, analytics.BirthProfileCreated, &in.UserID, map[string]any{
		"outcome": in.TimeAccuracy,
	})

	return toProfile(row), nil
}

// Update creates a NEW version and supersedes the old one.
//
// Never an UPDATE of the existing row. A reading given last month was
// based on a specific chart, and "which chart was that?" has to stay
// answerable — so correcting a birth time creates version 2 and leaves
// version 1 readable with is_active false.
//
// This is also why charts are keyed on birth_profile_id rather than on a
// user: the old chart stays attached to the old version and remains the
// correct explanation for the old reading.
func (s *Service) Update(ctx context.Context, userID, profileID uuid.UUID, in CreateInput) (Profile, error) {
	existing, err := s.q.GetBirthProfile(ctx, dbgen.GetBirthProfileParams{
		ID:     toPgUUID(profileID),
		UserID: toPgUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// 404, not 403 — a 403 confirms the row exists.
			return Profile{}, ErrNotFound
		}
		return Profile{}, fmt.Errorf("birthprofiles: load for update: %w", err)
	}

	in.UserID = userID
	resolved, err := s.resolve(in)
	if err != nil {
		return Profile{}, err
	}

	created, err := s.q.CreateBirthProfile(ctx, s.params(in, resolved, existing.Version+1))
	if err != nil {
		return Profile{}, fmt.Errorf("birthprofiles: create version: %w", err)
	}

	// Supersede AFTER the new version exists. The other order would leave
	// a user with no active profile if the insert failed.
	if _, err := s.q.SupersedeBirthProfile(ctx, dbgen.SupersedeBirthProfileParams{
		ID:           existing.ID,
		SupersededBy: created.ID,
		UserID:       toPgUUID(userID),
	}); err != nil {
		return Profile{}, fmt.Errorf("birthprofiles: supersede v%d: %w", existing.Version, err)
	}

	s.events.Emit(ctx, analytics.BirthProfileEdited, &userID, map[string]any{
		"count": int(created.Version),
	})

	return toProfile(created), nil
}

func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Profile, error) {
	rows, err := s.q.ListActiveBirthProfiles(ctx, toPgUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("birthprofiles: list: %w", err)
	}

	out := make([]Profile, 0, len(rows))
	for _, row := range rows {
		out = append(out, toProfile(row))
	}
	return out, nil
}

// Get returns one profile, scoped to its owner.
func (s *Service) Get(ctx context.Context, userID, profileID uuid.UUID) (Profile, error) {
	row, err := s.q.GetBirthProfile(ctx, dbgen.GetBirthProfileParams{
		ID:     toPgUUID(profileID),
		UserID: toPgUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Profile{}, ErrNotFound
		}
		return Profile{}, fmt.Errorf("birthprofiles: get: %w", err)
	}
	return toProfile(row), nil
}

// Owns reports whether the user owns the profile, and nothing else.
//
// It exists so the HTTP ownership middleware can gate a whole subtree
// without becoming a data source — see httpapi.RequireProfileOwnership.
// The query is scoped by user_id in SQL, so a foreign profile is
// indistinguishable from a missing one right down at the row level, not
// merely in the response.
func (s *Service) Owns(ctx context.Context, userID, profileID uuid.UUID) error {
	_, err := s.Get(ctx, userID, profileID)
	return err
}

// Versions returns the history behind a profile, newest first.
func (s *Service) Versions(ctx context.Context, userID, profileID uuid.UUID) ([]Profile, error) {
	rows, err := s.q.ListBirthProfileVersions(ctx, dbgen.ListBirthProfileVersionsParams{
		ID:     toPgUUID(profileID),
		UserID: toPgUUID(userID),
	})
	if err != nil {
		return nil, fmt.Errorf("birthprofiles: versions: %w", err)
	}

	out := make([]Profile, 0, len(rows))
	for _, row := range rows {
		// The recursive CTE produces its own row type rather than
		// BirthProfile, so the fields are copied across explicitly.
		out = append(out, toProfile(dbgen.BirthProfile(row)))
	}
	return out, nil
}

// Deactivate soft-deletes a profile.
//
// Soft, because the charts and readings that reference it must stay
// explicable. Hard deletion happens only when the whole account is
// deleted, where the cascade removes profiles, charts and dashas
// together.
func (s *Service) Deactivate(ctx context.Context, userID, profileID uuid.UUID) error {
	affected, err := s.q.DeactivateBirthProfile(ctx, dbgen.DeactivateBirthProfileParams{
		ID:     toPgUUID(profileID),
		UserID: toPgUUID(userID),
	})
	if err != nil {
		return fmt.Errorf("birthprofiles: deactivate: %w", err)
	}
	if affected == 0 {
		// Not found, not yours, or already inactive — all 404, because
		// distinguishing them tells a caller whether a row exists.
		return ErrNotFound
	}
	return nil
}

// resolve turns the local birth details into an absolute instant.
func (s *Service) resolve(in CreateInput) (ResolvedInstant, error) {
	switch in.TimeAccuracy {
	case AccuracyExact, AccuracyApproximate:
		if !in.HasBirthTime {
			return ResolvedInstant{}, fmt.Errorf(
				"%w: time_accuracy %q requires a birth time", ErrInvalidInput, in.TimeAccuracy)
		}
		return ResolveInstant(in.BirthDate, in.BirthTime, in.Timezone)

	case AccuracyUnknown:
		return ResolveUnknownTime(in.BirthDate, in.Timezone)

	default:
		return ResolvedInstant{}, fmt.Errorf(
			"%w: unknown time_accuracy %q", ErrInvalidInput, in.TimeAccuracy)
	}
}

func (s *Service) params(in CreateInput, resolved ResolvedInstant, version int32) dbgen.CreateBirthProfileParams {
	label := in.Label
	if label == "" {
		label = "self"
	}

	var birthTime pgtype.Time
	if in.HasBirthTime && in.TimeAccuracy != AccuracyUnknown {
		birthTime = pgtype.Time{Microseconds: in.BirthTime.Microseconds(), Valid: true}
	}

	return dbgen.CreateBirthProfileParams{
		UserID:       toPgUUID(in.UserID),
		Label:        label,
		BirthDate:    pgtype.Date{Time: in.BirthDate, Valid: true},
		BirthTime:    birthTime,
		TimeAccuracy: in.TimeAccuracy,
		BirthPlace:   in.PlaceName,
		Latitude:     in.Latitude,
		Longitude:    in.Longitude,
		Timezone:     in.Timezone,
		UtcOffsetMin: int32(resolved.OffsetMinutes),
		UtcInstant:   resolved.UTCInstant,
		Source:       "user",
		Version:      version,
	}
}

func toProfile(row dbgen.BirthProfile) Profile {
	var birthTime *string
	if row.BirthTime.Valid {
		d := time.Duration(row.BirthTime.Microseconds) * time.Microsecond
		formatted := fmt.Sprintf("%02d:%02d", int(d.Hours()), int(d.Minutes())%60)
		birthTime = &formatted
	}

	var supersededBy *uuid.UUID
	if row.SupersededBy.Valid {
		id := uuid.UUID(row.SupersededBy.Bytes)
		supersededBy = &id
	}

	return Profile{
		ID:           uuid.UUID(row.ID.Bytes),
		Label:        row.Label,
		BirthDate:    row.BirthDate.Time.Format("2006-01-02"),
		BirthTime:    birthTime,
		TimeAccuracy: row.TimeAccuracy,
		BirthPlace:   row.BirthPlace,
		Latitude:     row.Latitude,
		Longitude:    row.Longitude,
		Timezone:     row.Timezone,
		UTCOffsetMin: row.UtcOffsetMin,
		UTCInstant:   row.UtcInstant,
		Version:      row.Version,
		IsActive:     row.IsActive,
		SupersededBy: supersededBy,
		CreatedAt:    row.CreatedAt,
	}
}

func toPgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}
