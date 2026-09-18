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

	// Phase 2. The most sensitive thing this product holds, and
	// therefore the part of an export that matters most.
	//
	// Every version, not only the active one: correcting a birth time
	// creates a new row and supersedes the old, and a copy of "your
	// data" that silently drops the superseded versions is not a copy.
	BirthProfiles []ExportBirthProfile `json:"birth_profiles"`
	Charts        []ExportChart        `json:"charts"`

	// Phase 3. Which links this person created, and what became of them.
	//
	// Included because "who can currently see my chart" is a question
	// only this answers, and an export that omits it hands somebody a
	// copy of their data with the sharing removed.
	ShareLinks []ExportShareLink `json:"share_links"`
}

// ExportShareLink is one share link as its owner receives it back.
//
// The TOKEN is absent, and so is its hash. The plaintext was shown once
// at creation and is not stored; the hash is not useful to a person and
// is the one field that, combined with our table, would identify a live
// credential. What the owner needs is which links exist, whether each is
// still working, and how much it has been used.
type ExportShareLink struct {
	ID        uuid.UUID  `json:"id"`
	ProfileID uuid.UUID  `json:"birth_profile_id"`
	Scope     string     `json:"scope"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	ViewCount int64      `json:"view_count"`
	CreatedAt time.Time  `json:"created_at"`
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

// ExportBirthProfile is a birth profile as its owner receives it back.
//
// Every stored field, including the derived ones. utc_instant and
// utc_offset_min are what the server worked out from the local time and
// the zone; omitting them would hand back less than was used to compute
// the charts alongside.
type ExportBirthProfile struct {
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

// ExportChart carries the computed chart and its provenance.
//
// engine_version and ayanamsa are not decoration: a chart is only
// explicable alongside the engine and the ayanamsa that produced it, and
// an export without them is a page of numbers nobody can reproduce.
type ExportChart struct {
	ID             uuid.UUID       `json:"id"`
	BirthProfileID uuid.UUID       `json:"birth_profile_id"`
	ChartType      string          `json:"chart_type"`
	Ayanamsa       string          `json:"ayanamsa"`
	HouseSystem    string          `json:"house_system"`
	EngineVersion  string          `json:"engine_version"`
	Data           json.RawMessage `json:"chart_data"`
	ComputedAt     time.Time       `json:"computed_at"`
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

	// Every version, active or superseded. ListActiveBirthProfiles would
	// drop the history, and the history is the part that explains an old
	// reading.
	profiles, err := e.q.ListAllBirthProfilesForUser(ctx, pgUser)
	if err != nil {
		return Export{}, fmt.Errorf("users: export birth profiles: %w", err)
	}

	/*
	   Share links.

	   Fails the whole export if it fails, like every other section here.
	   A partial export that looks complete is worse than none: somebody
	   checking "who can see my chart" would read an absent section as
	   "nobody", which is the opposite of what an unavailable read means.
	*/
	shareRows, err := e.q.ListChartSharesForUser(ctx, pgUser)
	if err != nil {
		return Export{}, fmt.Errorf("users: export share links: %w", err)
	}
	shareLinks := make([]ExportShareLink, 0, len(shareRows))
	for _, row := range shareRows {
		link := ExportShareLink{
			ID:        row.ID.Bytes,
			ProfileID: row.BirthProfileID.Bytes,
			Scope:     row.Scope,
			ExpiresAt: row.ExpiresAt,
			ViewCount: row.ViewCount,
			CreatedAt: row.CreatedAt,
		}
		if row.RevokedAt.Valid {
			revoked := row.RevokedAt.Time
			link.RevokedAt = &revoked
		}
		shareLinks = append(shareLinks, link)
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

		// Never nil. A null here reads as "we hold nothing", which is a
		// different and much stronger claim than "you have none".
		BirthProfiles: make([]ExportBirthProfile, 0, len(profiles)),
		Charts:        make([]ExportChart, 0, len(profiles)),
		ShareLinks:    shareLinks,
	}

	for _, profile := range profiles {
		out.BirthProfiles = append(out.BirthProfiles, toExportBirthProfile(profile))

		charts, chartErr := e.q.ListChartsForProfile(ctx, dbgen.ListChartsForProfileParams{
			BirthProfileID: profile.ID,
			UserID:         pgUser,
		})
		if chartErr != nil {
			return Export{}, fmt.Errorf("users: export charts: %w", chartErr)
		}
		for _, chart := range charts {
			out.Charts = append(out.Charts, ExportChart{
				ID:             uuid.UUID(chart.ID.Bytes),
				BirthProfileID: uuid.UUID(chart.BirthProfileID.Bytes),
				ChartType:      chart.ChartType,
				Ayanamsa:       chart.Ayanamsa,
				HouseSystem:    chart.HouseSystem,
				EngineVersion:  chart.EngineVersion,
				Data:           chart.ChartData,
				ComputedAt:     chart.ComputedAt,
			})
		}
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

func toExportBirthProfile(row dbgen.BirthProfile) ExportBirthProfile {
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

	return ExportBirthProfile{
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
