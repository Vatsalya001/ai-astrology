// Package audit records security-relevant events.
//
// The one rule: IDs, actions, enums and buckets. Never PII. An audit
// trail is retained far longer than anything else in the system and is
// read by people who never saw this file — once an email address is in
// it, it is effectively permanent.
//
// The metadata column cannot enforce that. This package can, and does.
package audit

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// allowedKeys is what may appear in an audit metadata payload.
//
// An allowlist, not a denylist. A denylist passes the first time someone
// adds a key nobody thought to forbid, and "just this once, for
// debugging" is how that happens.
var allowedKeys = map[string]bool{
	"channel":     true, // email | phone | google | apple
	"reason":      true, // a short enum, never free text
	"provider":    true,
	"role":        true,
	"session_id":  true,
	"device_kind": true,
	"count":       true,
	"outcome":     true,
	"grace_hours": true,

	// Phase 4 — the AI admin surface. §14: "Admin AI routes SUPER_ADMIN
	// only, audit-logged in Go." The role half was enforced at the
	// router and the audit half was simply absent, so the endpoint that
	// spends real money against the production provider left no record
	// of who ran it.
	//
	// Every one of these is an ID, an enum or a count. `job` and `tier`
	// are closed sets; `message_chars` is a LENGTH rather than the
	// message, because an audit row outlives everything else in the
	// system and a question about somebody's marriage must not be in it.
	"job":           true,
	"tier":          true,
	"overrides":     true, // count of jobs changed, never their values
	"reset":         true,
	"message_chars": true,
	"window_days":   true,
	"limit":         true,
}

// Recorder writes audit rows.
type Recorder struct {
	q      dbgen.Querier
	logger *slog.Logger
}

func NewRecorder(q dbgen.Querier, logger *slog.Logger) *Recorder {
	if logger == nil {
		logger = slog.Default()
	}
	return &Recorder{q: q, logger: logger}
}

// Record writes one event.
//
// Deliberately returns nothing. An audit write that fails must not fail
// the operation it describes — refusing a login because the audit table
// is full turns a logging problem into an outage. Failures are logged
// loudly instead.
func (r *Recorder) Record(
	ctx context.Context,
	userID *uuid.UUID,
	action string,
	metadata map[string]any,
	ipHash []byte,
) {
	clean, dropped := sanitise(metadata)

	if len(dropped) > 0 {
		// Loud, because this is a bug in the caller: a key that is not on
		// the allowlist was very likely PII. The KEYS are logged, never
		// their values — logging the value would leak the thing that was
		// just refused.
		r.logger.WarnContext(ctx, "audit metadata keys dropped; not on the allowlist",
			slog.String("action", action),
			slog.Any("dropped_keys", dropped),
		)
	}

	payload, err := marshalMetadata(clean)
	if err != nil {
		r.logger.ErrorContext(ctx, "encode audit metadata", slog.Any("err", err))
		return
	}

	var pgUser pgtype.UUID
	if userID != nil {
		pgUser = pgtype.UUID{Bytes: *userID, Valid: true}
	}

	if err := r.q.WriteAuditLog(ctx, dbgen.WriteAuditLogParams{
		UserID:   pgUser,
		Action:   action,
		Metadata: payload,
		IpHash:   ipHash,
	}); err != nil {
		r.logger.ErrorContext(ctx, "write audit log",
			slog.String("action", action),
			slog.Any("err", err),
		)
	}
}

// sanitise drops keys that are not on the allowlist and returns their
// names so the caller's mistake is visible.
func sanitise(metadata map[string]any) (map[string]any, []string) {
	if len(metadata) == 0 {
		return map[string]any{}, nil
	}

	clean := make(map[string]any, len(metadata))
	var dropped []string

	for key, value := range metadata {
		if !allowedKeys[key] {
			dropped = append(dropped, key)
			continue
		}
		// Values are constrained too. A nested object could smuggle PII
		// under an allowed key — `{"reason": {"email": "..."}}` passes a
		// key check and defeats the point.
		switch value.(type) {
		case string, int, int64, float64, bool, nil:
			clean[key] = value
		default:
			dropped = append(dropped, key+" (non-scalar)")
		}
	}
	return clean, dropped
}

func marshalMetadata(m map[string]any) ([]byte, error) {
	if len(m) == 0 {
		// `{}` rather than null: the column is NOT NULL DEFAULT '{}', and
		// a JSON null would read as "unknown" instead of "nothing to say".
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}
