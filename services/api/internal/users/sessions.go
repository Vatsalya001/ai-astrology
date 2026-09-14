package users

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// SessionDirectory is the device list a user sees in settings.
//
// It lives here rather than in `auth` because `auth` cannot import
// `users` — users already imports auth for the UserCreator contract, and
// the reverse would cycle. The split is also the right one on its
// merits: `auth` owns rotation and revocation *mechanics*; this owns the
// user-facing view of which devices are signed in.
type SessionDirectory struct {
	q dbgen.Querier
}

func NewSessionDirectory(q dbgen.Querier) *SessionDirectory {
	return &SessionDirectory{q: q}
}

var _ SessionReader = (*SessionDirectory)(nil)

// ListActive returns the user's live sessions, newest first.
//
// Never includes refresh_hash. A credential-shaped value rendered into a
// settings page ends up in a screenshot and a support ticket.
func (d *SessionDirectory) ListActive(ctx context.Context, userID uuid.UUID) ([]SessionView, error) {
	rows, err := d.q.ListActiveSessions(ctx, toPgUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("users: list sessions: %w", err)
	}

	views := make([]SessionView, 0, len(rows))
	for _, row := range rows {
		userAgent := ""
		if row.UserAgent != nil {
			userAgent = *row.UserAgent
		}
		views = append(views, SessionView{
			ID:        uuid.UUID(row.ID.Bytes),
			UserAgent: userAgent,
			CreatedAt: row.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			ExpiresAt: row.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	return views, nil
}

// Revoke ends one session.
//
// The query is scoped by user_id, so revoking someone else's session
// affects no rows and returns ErrNotFound — never 403, which would
// confirm the session exists and belongs to another account.
func (d *SessionDirectory) Revoke(ctx context.Context, sessionID, userID uuid.UUID) error {
	affected, err := d.q.RevokeSession(ctx, dbgen.RevokeSessionParams{
		ID:     toPgUUID(sessionID),
		UserID: toPgUUID(userID),
	})
	if err != nil {
		return fmt.Errorf("users: revoke session: %w", err)
	}
	if affected == 0 {
		// Either the session does not exist, belongs to someone else, or
		// was already revoked. All three are ErrNotFound: reporting 403
		// for "belongs to someone else" would confirm the session exists,
		// and a separate code for "already revoked" leaks that it once
		// did.
		return ErrNotFound
	}
	return nil
}

// RevokeAll ends every session for a user. Used by logout-all and by
// account deletion.
func (d *SessionDirectory) RevokeAll(ctx context.Context, userID uuid.UUID) error {
	if err := d.q.RevokeAllUserSessions(ctx, toPgUUID(userID)); err != nil {
		return fmt.Errorf("users: revoke all sessions: %w", err)
	}
	return nil
}
