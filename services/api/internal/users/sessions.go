package users

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/analytics"
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
	q      dbgen.Querier
	events analytics.Emitter
}

func NewSessionDirectory(q dbgen.Querier) *SessionDirectory {
	return &SessionDirectory{q: q, events: analytics.Nop{}}
}

// WithAnalytics attaches an emitter. See users.Service.WithAnalytics.
func (d *SessionDirectory) WithAnalytics(events analytics.Emitter) *SessionDirectory {
	if events != nil {
		d.events = events
	}
	return d
}

var _ SessionReader = (*SessionDirectory)(nil)

// ListActive returns the user's signed-in DEVICES, newest first.
//
// One entry per rotation family, not per session row. Every refresh
// issues a new row sharing its predecessor's family_id, so a browser
// left open for an hour accumulates a dozen — listing those as devices
// shows the user twelve sign-ins they do not recognise, and revoking one
// kills a spent link rather than the device.
//
// The ID returned is therefore the FAMILY id, and Revoke takes the same
// thing. Never includes refresh_hash: a credential-shaped value rendered
// into a settings page ends up in a screenshot and a support ticket.
func (d *SessionDirectory) ListActive(ctx context.Context, userID uuid.UUID) ([]SessionView, error) {
	rows, err := d.q.ListActiveDevices(ctx, toPgUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("users: list devices: %w", err)
	}

	views := make([]SessionView, 0, len(rows))
	for _, row := range rows {
		userAgent := ""
		if row.UserAgent != nil {
			userAgent = *row.UserAgent
		}
		views = append(views, SessionView{
			// The family, not the session row — see above.
			ID:        uuid.UUID(row.FamilyID.Bytes),
			UserAgent: userAgent,
			CreatedAt: row.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			ExpiresAt: row.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	return views, nil
}

// Revoke signs one device out.
//
// Takes a FAMILY id and ends the whole lineage, which is what "sign this
// device out" means: revoking a single row in a rotation chain leaves
// the device's current token working.
//
// The query is scoped by user_id, so revoking someone else's device
// affects no rows and returns ErrNotFound — never 403, which would
// confirm the device exists and belongs to another account.
func (d *SessionDirectory) Revoke(ctx context.Context, familyID, userID uuid.UUID) error {
	affected, err := d.q.RevokeDeviceFamily(ctx, dbgen.RevokeDeviceFamilyParams{
		FamilyID: toPgUUID(familyID),
		UserID:   toPgUUID(userID),
	})
	if err != nil {
		return fmt.Errorf("users: revoke device: %w", err)
	}
	if affected == 0 {
		// Either the device does not exist, belongs to someone else, or
		// was already revoked. All three are ErrNotFound: reporting 403
		// for "belongs to someone else" would confirm it exists, and a
		// separate code for "already revoked" leaks that it once did.
		return ErrNotFound
	}

	// Only on a real revocation. Emitting on the not-found path would put
	// a count of failed cross-user probes into the product funnel, where
	// it reads as users revoking devices.
	d.events.Emit(ctx, analytics.SessionRevoked, &userID, map[string]any{
		"count": int(affected),
	})

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
