package users

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

var (
	ErrDeletionAlreadyRequested = errors.New("users: deletion already requested")
	ErrNoDeletionPending        = errors.New("users: no deletion pending")
)

// Deleter owns account deletion.
//
// Two-step with a grace window, then real removal. "Real" is the
// operative word: the Phase 1 gate says no row anywhere may reference
// the user afterwards, and a status flag is not deletion — it is a
// promise to forget that the database has not kept.
type Deleter struct {
	q        dbgen.Querier
	sessions *SessionDirectory
	grace    time.Duration
	logger   *slog.Logger
	events   analytics.Emitter
	now      func() time.Time
}

func NewDeleter(q dbgen.Querier, sessions *SessionDirectory, grace time.Duration, logger *slog.Logger) *Deleter {
	if logger == nil {
		logger = slog.Default()
	}
	return &Deleter{
		q: q, sessions: sessions, grace: grace,
		logger: logger, events: analytics.Nop{}, now: time.Now,
	}
}

// WithAnalytics attaches an emitter. See users.Service.WithAnalytics.
func (d *Deleter) WithAnalytics(events analytics.Emitter) *Deleter {
	if events != nil {
		d.events = events
	}
	return d
}

// Request starts the grace window.
//
// The account is marked deleted and every session revoked immediately —
// the user asked to be gone, and leaving them signed in on four devices
// for a week while "deletion is pending" is not what they asked for.
// The rows survive until the worker runs, so the window is genuinely
// cancellable.
func (d *Deleter) Request(ctx context.Context, userID uuid.UUID) (time.Time, error) {
	row, err := d.q.RequestUserDeletion(ctx, toPgUUID(userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The predicate is `deletion_requested_at IS NULL`, so no row
			// means either the user is gone or deletion is already pending.
			// Both are "nothing further to do".
			return time.Time{}, ErrDeletionAlreadyRequested
		}
		return time.Time{}, fmt.Errorf("users: request deletion: %w", err)
	}

	if err := d.sessions.RevokeAll(ctx, userID); err != nil {
		// Not fatal to the request itself — the account is already marked
		// — but it must be visible, because it means someone who asked to
		// be deleted is still signed in somewhere.
		d.logger.ErrorContext(ctx, "revoke sessions after deletion request",
			slog.String("user_id", userID.String()), slog.Any("err", err))
	}

	if !row.DeletionRequestedAt.Valid {
		return time.Time{}, fmt.Errorf("users: deletion timestamp was not set")
	}

	// The grace window in hours, which is a configuration value rather
	// than anything about the person.
	d.events.Emit(ctx, analytics.AccountDeletionRequested, &userID, map[string]any{
		"count": int(d.grace.Hours()),
	})

	return row.DeletionRequestedAt.Time.Add(d.grace), nil
}

// Cancel stops a pending deletion.
//
// Only reachable while the grace window is open; once the worker has
// run there is no account left to cancel with.
func (d *Deleter) Cancel(ctx context.Context, userID uuid.UUID) error {
	if _, err := d.q.CancelUserDeletion(ctx, toPgUUID(userID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoDeletionPending
		}
		return fmt.Errorf("users: cancel deletion: %w", err)
	}
	return nil
}

// RunHardDeletes removes every account whose grace window has closed.
//
// Returns how many were deleted, so the caller can log a number rather
// than "it ran".
func (d *Deleter) RunHardDeletes(ctx context.Context) (int, error) {
	cutoff := pgtype.Timestamptz{Time: d.now().Add(-d.grace), Valid: true}

	pending, err := d.q.ListUsersPastDeletionGrace(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("users: list pending deletions: %w", err)
	}

	var deleted int
	for _, row := range pending {
		userID := uuid.UUID(row.ID.Bytes)

		// One failure must not stop the rest. A single wedged account
		// should not mean nobody else's deletion runs.
		if err := d.q.HardDeleteUser(ctx, row.ID); err != nil {
			d.logger.ErrorContext(ctx, "hard delete user",
				slog.String("user_id", userID.String()), slog.Any("err", err))
			continue
		}
		deleted++

		// The audit row survives the deletion deliberately: audit_logs.user_id
		// is not a foreign key, so the record that an account was deleted
		// outlives the account. It holds an ID and an action, never PII.
		d.logger.InfoContext(ctx, "account hard deleted",
			slog.String("user_id", userID.String()))

		// Emitted AFTER the row is gone, and carrying only the id — which
		// by then references nothing. That is the point: the event says an
		// account was deleted without preserving anything about who it
		// belonged to.
		d.events.Emit(ctx, analytics.AccountDeleted, &userID, nil)
	}

	return deleted, nil
}
