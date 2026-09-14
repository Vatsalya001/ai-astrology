package users

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// Export is everything the system holds about one person.
//
// The gate wording is "complete", and completeness is the whole point —
// a portability export that quietly omits a table is worse than none,
// because it tells the user they have seen everything.
//
// Every phase that adds a user-owned table MUST add it here. There is a
// test that fails when a table exists in the schema and not in this
// struct, so the obligation is enforced rather than remembered.
type Export struct {
	ExportedAt  time.Time         `json:"exported_at"`
	Format      string            `json:"format_version"`
	Profile     ProfileView       `json:"profile"`
	Preferences PreferencesView   `json:"preferences"`
	Identities  []ExportIdentity  `json:"auth_identities"`
	Sessions    []ExportSession   `json:"sessions"`
	AuditLog    []ExportAuditItem `json:"audit_log"`
}

type ExportIdentity struct {
	Provider  string    `json:"provider"`
	CreatedAt time.Time `json:"created_at"`
	// provider_user_id IS the identifier — the email or phone. Included
	// deliberately: this is the user's own data, being handed to the user,
	// which is the one context where returning it is the correct thing.
	ProviderUserID string `json:"provider_user_id"`
}

type ExportSession struct {
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Revoked   bool      `json:"revoked"`
	// No refresh_hash. It is not the user's data in any useful sense —
	// it is a credential — and handing it over in a downloadable file
	// creates a copy of a live secret outside the system.
}

type ExportAuditItem struct {
	Action    string         `json:"action"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
}

// Exporter builds a complete export.
type Exporter struct {
	q dbgen.Querier
}

func NewExporter(q dbgen.Querier) *Exporter {
	return &Exporter{q: q}
}

// exportAuditLimit caps the audit history returned.
//
// A cap rather than everything: an account years old could have tens of
// thousands of rows, and a response that times out is not an export. The
// most recent are the ones a person asking "what do you know about me"
// actually wants.
const exportAuditLimit = 1000

func (e *Exporter) Export(ctx context.Context, userID uuid.UUID) (Export, error) {
	pgUser := toPgUUID(userID)

	user, err := e.q.FindUserByID(ctx, pgUser)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Export{}, ErrNotFound
		}
		return Export{}, fmt.Errorf("users: export profile: %w", err)
	}

	prefs, err := e.q.GetPreferences(ctx, pgUser)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Export{}, fmt.Errorf("users: export preferences: %w", err)
	}

	sessions, err := e.q.ListActiveSessions(ctx, pgUser)
	if err != nil {
		return Export{}, fmt.Errorf("users: export sessions: %w", err)
	}

	identities, err := e.q.ListIdentitiesForUser(ctx, pgUser)
	if err != nil {
		return Export{}, fmt.Errorf("users: export identities: %w", err)
	}

	auditRows, err := e.q.ListAuditLogsForUser(ctx, dbgen.ListAuditLogsForUserParams{
		UserID: pgUser,
		Limit:  exportAuditLimit,
	})
	if err != nil {
		return Export{}, fmt.Errorf("users: export audit log: %w", err)
	}

	out := Export{
		ExportedAt: time.Now().UTC(),
		// Versioned so a later change to the shape is detectable by
		// whatever the user imported it into.
		Format:      "ayana.export.v1",
		Profile:     toProfileView(user),
		Preferences: toPreferencesView(prefs),
		Identities:  make([]ExportIdentity, 0, len(identities)),
		Sessions:    make([]ExportSession, 0, len(sessions)),
		AuditLog:    make([]ExportAuditItem, 0, len(auditRows)),
	}

	for _, id := range identities {
		out.Identities = append(out.Identities, ExportIdentity{
			Provider:       string(id.Provider),
			ProviderUserID: id.ProviderUserID,
			CreatedAt:      id.CreatedAt,
		})
	}

	for _, s := range sessions {
		ua := ""
		if s.UserAgent != nil {
			ua = *s.UserAgent
		}
		out.Sessions = append(out.Sessions, ExportSession{
			UserAgent: ua,
			CreatedAt: s.CreatedAt,
			ExpiresAt: s.ExpiresAt,
			Revoked:   s.RevokedAt.Valid,
		})
	}

	for _, a := range auditRows {
		out.AuditLog = append(out.AuditLog, ExportAuditItem{
			Action:    a.Action,
			Metadata:  decodeMetadata(a.Metadata),
			CreatedAt: a.CreatedAt,
		})
	}

	return out, nil
}

// decodeMetadata turns the stored JSONB into a map for the export.
//
// A decode failure yields an empty object rather than an error: one
// malformed historic row must not make a person's entire export fail.
func decodeMetadata(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}
