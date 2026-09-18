// Package shares issues and resolves shareable links to a chart.
//
// ── The one constraint everything here serves ──
//
// From the phase spec's security checklist: "Share links resolve
// server-side against the viewer's permissions — they do not embed birth
// details."
//
// Both halves matter, and the second is the easy one to get wrong. A
// link that carried the chart — signed, encoded, however cleverly — would
// be a permanent, unrevocable publication of somebody's birth data the
// moment it left their phone. So the URL carries one opaque token and
// nothing else, and every decision about what a viewer may see is made
// here, on each request, against rows that the owner can kill.
package shares

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/analytics"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// ErrNotFound covers "no such link", "expired", "revoked" and "not
// yours".
//
// Deliberately one error. Distinguishing them would tell a caller
// whether a token was ever real, which turns guessing into probing —
// and would tell one user that another user's link exists.
var ErrNotFound = errors.New("shares: not found")

// ErrTooManyLive is returned when the per-user cap is reached.
//
// Distinct from ErrNotFound because this one the owner can act on: the
// remedy is to revoke a link they no longer need, and the message says
// so.
var ErrTooManyLive = errors.New("shares: too many live links")

const (
	// DefaultTTL is how long a new link works.
	//
	// Thirty days. A share is an act with a moment attached — "look at
	// this" — and a link that works forever is one its owner has no
	// reason ever to revisit. Long enough that a relative who opens it
	// next weekend is not annoyed; short enough that a link forwarded
	// into a group chat stops working long before anybody excavates it.
	DefaultTTL = 30 * 24 * time.Hour

	// MaxTTL bounds what a caller may ask for.
	MaxTTL = 90 * 24 * time.Hour

	// MinTTL likewise. A link that expires in a minute is not a share.
	MinTTL = time.Hour

	// MaxLivePerUser caps concurrent live links per account.
	//
	// Not a rate limit — it is a comprehensibility limit. These are
	// bearer credentials to somebody's birth chart, and an owner cannot
	// meaningfully reason about forty of them. Hitting the cap is a
	// prompt to revoke, and the error says that.
	MaxLivePerUser = 20

	// SweepGrace is how long a dead row is kept before the worker
	// removes it.
	//
	// Seven days, so an owner opening the share screen can still see
	// that a link existed and has lapsed. A row that vanishes the
	// instant it expires reads as "I never made that link".
	SweepGrace = 7 * 24 * time.Hour
)

// ScopeChart is the only scope today: the diagram and the planetary
// positions, without the birth time or place.
const ScopeChart = "chart"

// Share is one link, as its owner sees it.
//
// Token is populated ONLY by Create, and only once — it is the plaintext,
// which is never stored and can never be recovered. Every read path
// leaves it empty, which is what makes "we cannot show you that link
// again, make a new one" a property of the type rather than a promise.
type Share struct {
	ID        uuid.UUID  `json:"id"`
	ProfileID uuid.UUID  `json:"birth_profile_id"`
	Scope     string     `json:"scope"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	ViewCount int64      `json:"view_count"`
	CreatedAt time.Time  `json:"created_at"`

	// Token is the plaintext, present only in the response to Create.
	Token string `json:"token,omitempty"`
}

// Resolved is what a viewer's token buys.
//
// Identifiers only — no birth data at all. The caller uses these to load
// the chart through the normal, already-scoped path, which means the
// share route cannot become a second way of reading charts with its own
// forgotten predicate.
type Resolved struct {
	ShareID   uuid.UUID
	UserID    uuid.UUID
	ProfileID uuid.UUID
	Scope     string
}

// ProfileOwnership is the check that a profile belongs to a user,
// declared by this consumer so the package does not import birthprofiles
// for one method.
type ProfileOwnership interface {
	Owns(ctx context.Context, userID, profileID uuid.UUID) error
}

type Service struct {
	q      dbgen.Querier
	owner  ProfileOwnership
	events analytics.Emitter
	logger *slog.Logger
	now    func() time.Time
}

func NewService(q dbgen.Querier, owner ProfileOwnership, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		q: q, owner: owner,
		events: analytics.Nop{}, logger: logger, now: time.Now,
	}
}

func (s *Service) WithAnalytics(events analytics.Emitter) *Service {
	if events != nil {
		s.events = events
	}
	return s
}

// WithClock replaces the clock, so a test can expire a link without
// waiting thirty days.
func (s *Service) WithClock(now func() time.Time) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

// Create issues a link for a profile the caller owns.
func (s *Service) Create(
	ctx context.Context,
	userID, profileID uuid.UUID,
	ttl time.Duration,
) (Share, error) {
	/*
	   Ownership is checked HERE as well as by the middleware above.

	   The route sits behind RequireProfileOwnership, so this is the
	   second check of the same fact — and it stays because what is being
	   created is a bearer credential to a birth chart. The cost is one
	   indexed lookup; the failure it guards against is a future caller
	   reaching this method from somewhere the middleware does not cover,
	   and minting a working link to a stranger's chart.

	   A nil owner FAILS CLOSED rather than skipping the check. The
	   worker constructs this service with no owner, because sweeping
	   dead rows needs none — and a service that cannot verify ownership
	   must not be able to mint credentials. Without this, that
	   construction would be one call away from issuing links to any
	   chart in the database.
	*/
	if s.owner == nil {
		return Share{}, fmt.Errorf(
			"shares: refusing to create a link without an ownership check")
	}
	if err := s.owner.Owns(ctx, userID, profileID); err != nil {
		return Share{}, ErrNotFound
	}

	if ttl == 0 {
		ttl = DefaultTTL
	}
	if ttl < MinTTL || ttl > MaxTTL {
		return Share{}, fmt.Errorf("shares: ttl must be between %s and %s", MinTTL, MaxTTL)
	}

	live, err := s.q.CountLiveChartShares(ctx, toPgUUID(userID))
	if err != nil {
		return Share{}, fmt.Errorf("shares: count live: %w", err)
	}
	if live >= MaxLivePerUser {
		return Share{}, ErrTooManyLive
	}

	token, hash, err := MintToken()
	if err != nil {
		return Share{}, err
	}

	row, err := s.q.CreateChartShare(ctx, dbgen.CreateChartShareParams{
		UserID:         toPgUUID(userID),
		BirthProfileID: toPgUUID(profileID),
		TokenHash:      hash,
		Scope:          ScopeChart,
		ExpiresAt:      s.now().Add(ttl).UTC(),
	})
	if err != nil {
		return Share{}, fmt.Errorf("shares: create: %w", err)
	}

	share := toShare(row)
	// The one moment the plaintext exists outside the caller's hand.
	share.Token = token
	return share, nil
}

// List returns every link for one profile, live or not.
func (s *Service) List(ctx context.Context, userID, profileID uuid.UUID) ([]Share, error) {
	rows, err := s.q.ListChartShares(ctx, dbgen.ListChartSharesParams{
		UserID:         toPgUUID(userID),
		BirthProfileID: toPgUUID(profileID),
	})
	if err != nil {
		return nil, fmt.Errorf("shares: list: %w", err)
	}

	out := make([]Share, 0, len(rows))
	for _, row := range rows {
		// toShare never copies the hash, so a listing cannot leak a
		// token even if a future handler serialises the whole struct.
		out = append(out, toShare(row))
	}
	return out, nil
}

// Revoke kills a link. Idempotent.
func (s *Service) Revoke(ctx context.Context, userID, shareID uuid.UUID) (Share, error) {
	row, err := s.q.RevokeChartShare(ctx, dbgen.RevokeChartShareParams{
		ID:     toPgUUID(shareID),
		UserID: toPgUUID(userID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Includes "somebody else's link". 404, never 403.
		return Share{}, ErrNotFound
	}
	if err != nil {
		return Share{}, fmt.Errorf("shares: revoke: %w", err)
	}
	return toShare(row), nil
}

// RevokeForProfile kills every live link for one profile.
//
// Called when birth details are corrected. A correction creates a new
// profile version, and a link pointing at the old one would keep serving
// a chart its owner has already decided was wrong — to people they can
// no longer reach.
func (s *Service) RevokeForProfile(ctx context.Context, userID, profileID uuid.UUID) error {
	err := s.q.RevokeChartSharesForProfile(ctx, dbgen.RevokeChartSharesForProfileParams{
		BirthProfileID: toPgUUID(profileID),
		UserID:         toPgUUID(userID),
	})
	if err != nil {
		return fmt.Errorf("shares: revoke for profile: %w", err)
	}
	return nil
}

// Resolve turns a viewer's token into the scope it authorises.
//
// Liveness is the query's WHERE clause, not a check here — see
// shares.sql. An expired or revoked link returns no row, so this method
// has no branch that could forget to look.
func (s *Service) Resolve(ctx context.Context, token string) (Resolved, error) {
	token = NormaliseToken(token)
	if token == "" {
		return Resolved{}, ErrNotFound
	}

	row, err := s.q.ResolveChartShare(ctx, HashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return Resolved{}, ErrNotFound
	}
	if err != nil {
		return Resolved{}, fmt.Errorf("shares: resolve: %w", err)
	}

	/*
	   The view counter is best-effort.

	   A failure to record that a link was opened must not stop the
	   viewer seeing the chart — the count is a convenience for the
	   owner, and trading a working share for an accurate tally is the
	   wrong way round.

	   Logged rather than emitted as an analytics event: this is an
	   operational fact about our database, not something about a
	   person's behaviour, and the analytics vocabulary is deliberately
	   small. The share id is safe to log — it names a link, not a user.
	*/
	if err := s.q.TouchChartShare(ctx, row.ID); err != nil {
		s.logger.WarnContext(ctx, "shares: could not record a view",
			slog.String("share_id", fromPgUUID(row.ID).String()),
			slog.Any("err", err))
	}

	return Resolved{
		ShareID:   fromPgUUID(row.ID),
		UserID:    fromPgUUID(row.UserID),
		ProfileID: fromPgUUID(row.BirthProfileID),
		Scope:     row.Scope,
	}, nil
}

// Sweep removes rows that have been dead longer than the grace period.
func (s *Service) Sweep(ctx context.Context) (int64, error) {
	cutoff := s.now().Add(-SweepGrace).UTC()
	removed, err := s.q.DeleteExpiredChartShares(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("shares: sweep: %w", err)
	}
	return removed, nil
}

// toShare maps a row, and deliberately never copies TokenHash.
//
// The hash is not secret in the way the token is, but there is no reason
// for it to travel and every reason for it not to: a struct that cannot
// carry it cannot accidentally serialise it.
func toShare(row dbgen.ChartShare) Share {
	share := Share{
		ID:        fromPgUUID(row.ID),
		ProfileID: fromPgUUID(row.BirthProfileID),
		Scope:     row.Scope,
		ExpiresAt: row.ExpiresAt,
		ViewCount: row.ViewCount,
		CreatedAt: row.CreatedAt,
	}
	if row.RevokedAt.Valid {
		revoked := row.RevokedAt.Time
		share.RevokedAt = &revoked
	}
	return share
}

func toPgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func fromPgUUID(id pgtype.UUID) uuid.UUID {
	return id.Bytes
}
