// Package charts computes, persists and serves charts.
//
// The single property that shapes this whole file: a user must be able to
// view their existing Kundli when astro-service is down. A chart is a
// pure function of its inputs and changes only when the engine changes,
// so a stored one is never stale in the way a cached API response is.
package charts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/birthprofiles"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/analytics"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients/astroclient"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

var (
	ErrNotFound = errors.New("charts: not found")

	// ErrUncomputable means there is no chart and none can be made right
	// now. Distinct from ErrNotFound: the profile exists, we simply
	// cannot answer yet.
	ErrUncomputable = errors.New("charts: cannot compute and nothing cached")
)

// Defaults, matching the database columns.
const (
	SystemVedic      = "vedic"
	AyanamsaLahiri   = "lahiri"
	HouseWholeSign   = "whole_sign"
	ChartTypeRasi    = "D1"
	ChartTypeNavamsa = "D9"
)

// Key identifies a chart.
//
// Every field affects the output, and every field is therefore part of
// both the database's UNIQUE constraint and the cache key. Omitting the
// ayanamsa from a cache key is the classic bug the database rules call
// out by name: change a user's preference and they are served somebody
// else's chart.
type Key struct {
	ProfileID   uuid.UUID
	ChartType   string
	System      string
	Ayanamsa    string
	HouseSystem string
}

func (k Key) withDefaults() Key {
	if k.ChartType == "" {
		k.ChartType = ChartTypeRasi
	}
	if k.System == "" {
		k.System = SystemVedic
	}
	if k.Ayanamsa == "" {
		k.Ayanamsa = AyanamsaLahiri
	}
	if k.HouseSystem == "" {
		k.HouseSystem = HouseWholeSign
	}
	return k
}

// CacheKey is the Redis key, and it names every input.
func (k Key) CacheKey() string {
	k = k.withDefaults()
	return strings.Join([]string{
		"chart", k.ProfileID.String(), k.ChartType, k.System, k.Ayanamsa, k.HouseSystem,
	}, ":")
}

// Chart is a stored chart with its provenance.
type Chart struct {
	ID            uuid.UUID       `json:"id"`
	ProfileID     uuid.UUID       `json:"birth_profile_id"`
	ChartType     string          `json:"chart_type"`
	Ayanamsa      string          `json:"ayanamsa"`
	HouseSystem   string          `json:"house_system"`
	EngineVersion string          `json:"engine_version"`
	Data          json.RawMessage `json:"chart_data"`
	ComputedAt    time.Time       `json:"computed_at"`

	// FromCache tells the caller the chart was not recomputed. The UI
	// uses it for nothing; the tests use it to prove the cache was hit
	// without astro being called.
	FromCache bool `json:"-"`

	// Stale means astro-service was unavailable and this is a previously
	// stored chart. Surfaced so the UI can say so rather than implying
	// freshness it cannot vouch for.
	Stale bool `json:"stale,omitempty"`
}

// TxBeginner starts a transaction.
//
// Declared by the consumer, so this package names only what it uses —
// *pgxpool.Pool satisfies it, and so does a pgx.Conn.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Service struct {
	q        dbgen.Querier
	db       TxBeginner
	astro    *clients.Astro
	profiles *birthprofiles.Service
	logger   *slog.Logger
	events   analytics.Emitter
}

func NewService(
	q dbgen.Querier,
	db TxBeginner,
	astro *clients.Astro,
	profiles *birthprofiles.Service,
	logger *slog.Logger,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		q: q, db: db, astro: astro, profiles: profiles,
		logger: logger, events: analytics.Nop{},
	}
}

func (s *Service) WithAnalytics(events analytics.Emitter) *Service {
	if events != nil {
		s.events = events
	}
	return s
}

// Get returns a chart, computing it only if it is not already stored.
//
// The order is deliberate and is the whole design:
//
//  1. Stored chart? Return it. No HTTP call at all.
//  2. Not stored? Compute, persist, return.
//  3. Compute failed because astro is DOWN? There is nothing stored —
//     this is a new profile — so fail with a clear message.
//
// A chart is a pure function of its inputs, so step 1 is not a staleness
// trade: the stored answer is the same answer astro would give, unless
// the engine version has changed, which `Recompute` handles explicitly.
func (s *Service) Get(ctx context.Context, userID uuid.UUID, key Key) (Chart, error) {
	key = key.withDefaults()

	stored, err := s.load(ctx, userID, key)
	switch {
	case err == nil:
		stored.FromCache = true
		return stored, nil
	case errors.Is(err, ErrNotFound):
		// Fall through to compute.
	default:
		return Chart{}, err
	}

	profile, err := s.profiles.Get(ctx, userID, key.ProfileID)
	if err != nil {
		return Chart{}, err
	}

	chart, err := s.computeAndStore(ctx, userID, profile, key)
	if err != nil {
		return Chart{}, err
	}
	return chart, nil
}

// GetOrStale returns the stored chart even when astro is unavailable.
//
// This is the spec's property in a function. Viewing an existing Kundli
// must work during an astro outage; only creating a NEW profile fails,
// and it fails with a message that says so.
func (s *Service) GetOrStale(ctx context.Context, userID uuid.UUID, key Key) (Chart, error) {
	key = key.withDefaults()

	chart, err := s.Get(ctx, userID, key)
	if err == nil {
		return chart, nil
	}

	// Only an availability failure justifies falling back. A 422 means we
	// sent something invalid, and serving a stored chart would hide a bug
	// that is producing wrong charts for every new profile.
	if !clients.IsUnavailable(err) {
		return Chart{}, err
	}

	stored, loadErr := s.load(ctx, userID, key)
	if loadErr != nil {
		// Nothing stored and astro is down: a new profile during an
		// outage. Fails clearly rather than pretending.
		return Chart{}, fmt.Errorf("%w: astro-service is unavailable and no chart is stored",
			ErrUncomputable)
	}

	s.logger.Warn("serving a stored chart; astro-service is unavailable",
		slog.String("birth_profile_id", key.ProfileID.String()),
		slog.String("chart_type", key.ChartType))

	stored.FromCache = true
	stored.Stale = true
	return stored, nil
}

func (s *Service) load(ctx context.Context, userID uuid.UUID, key Key) (Chart, error) {
	row, err := s.q.GetChart(ctx, dbgen.GetChartParams{
		BirthProfileID:    toPgUUID(key.ProfileID),
		ChartType:         key.ChartType,
		CalculationSystem: key.System,
		Ayanamsa:          key.Ayanamsa,
		HouseSystem:       key.HouseSystem,
		UserID:            toPgUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Chart{}, ErrNotFound
		}
		return Chart{}, fmt.Errorf("charts: load: %w", err)
	}
	return toChart(row), nil
}

func (s *Service) computeAndStore(
	ctx context.Context,
	userID uuid.UUID,
	profile birthprofiles.Profile,
	key Key,
) (Chart, error) {
	started := time.Now()

	response, err := s.astro.ComputeChart(ctx, clients.ChartInput{
		UTCInstant:   profile.UTCInstant,
		Latitude:     profile.Latitude,
		Longitude:    profile.Longitude,
		TimeAccuracy: profile.TimeAccuracy,
		Ayanamsa:     key.Ayanamsa,
		HouseSystem:  key.HouseSystem,
	})
	if err != nil {
		// An enum, never the message: astro's 4xx detail can name fields,
		// and a failure reason is not worth risking birth data in an
		// analytics payload for.
		s.events.Emit(ctx, analytics.ChartGenerationFailed, &userID, map[string]any{
			"error_code": failureCode(err),
		})
		return Chart{}, err
	}

	// ONE response, BOTH charts.
	//
	// astro-service returns the navamsa alongside the rasi, because a D9
	// is a ninth-part division OF the D1 — it is not a second
	// computation. Deriving it here rather than asking again means:
	//
	//   - one HTTP call instead of two, and
	//   - the two can never disagree. A second call lands on a different
	//     instant, and a chart whose D9 came from a slightly different
	//     ephemeris state is wrong in a way no field records.
	//
	// The earlier version sent no chart type to astro at all and stored
	// the identical payload under both labels, so a client asking for the
	// navamsa was served the rasi with the real navamsa buried in a field
	// it was not reading. TestTheStoredD9IsTheNavamsaAndNotACopyOfTheD1
	// is what found that.
	payloads, err := splitByChartType(response)
	if err != nil {
		return Chart{}, err
	}

	// Both rows and the dasha tree are written in ONE transaction.
	//
	// Separately, a chart whose tree failed to write is served happily
	// forever — every read finds the chart, no read notices the missing
	// dashas, and the only symptom is an empty dasha screen that looks
	// like "this chart has no dashas", which is a real and different
	// thing (see ErrNoDashas).
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Chart{}, fmt.Errorf("charts: begin: %w", err)
	}
	// Rollback after a successful Commit is a no-op, so this needs no
	// flag to track whether the commit happened.
	defer func() { _ = tx.Rollback(ctx) }()

	txq := dbgen.New(tx)

	rows := make(map[string]dbgen.Chart, len(payloads))
	for chartType, data := range payloads {
		row, upsertErr := txq.UpsertChart(ctx, dbgen.UpsertChartParams{
			BirthProfileID:    toPgUUID(key.ProfileID),
			ChartType:         chartType,
			CalculationSystem: key.System,
			Ayanamsa:          key.Ayanamsa,
			HouseSystem:       key.HouseSystem,
			EngineVersion:     response.Meta.EngineVersion,
			ChartData:         data,
		})
		if upsertErr != nil {
			return Chart{}, fmt.Errorf("charts: persist %s: %w", chartType, upsertErr)
		}
		rows[chartType] = row
	}

	// Dashas hang off the RASI only. They are a property of the birth
	// moment, not of a divisional chart, and a second tree under the D9
	// would give FindDashaAt two rows per level to choose between.
	//
	// A chart without a birth time has no dashas at all — the Moon cannot
	// be pinned to a nakshatra pada without one — so an absent tree is
	// expected rather than a failure.
	if response.Dashas != nil {
		rasi := rows[ChartTypeRasi]
		if _, err := storeDashaTree(ctx, txq, uuid.UUID(rasi.ID.Bytes), *response.Dashas); err != nil {
			return Chart{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Chart{}, fmt.Errorf("charts: commit: %w", err)
	}

	s.events.Emit(ctx, analytics.ChartGenerated, &userID, map[string]any{
		"chart_type":  key.ChartType,
		"duration_ms": int(time.Since(started).Milliseconds()),
		"cached":      false,
	})

	wanted, ok := rows[key.ChartType]
	if !ok {
		// Asked for a divisional chart this response does not carry. The
		// only way here is include_navamsa having been switched off, which
		// nothing does — but returning the rasi under a D9 label is the
		// exact bug this function was just fixed for.
		return Chart{}, fmt.Errorf("charts: astro-service returned no %s for this birth",
			key.ChartType)
	}
	return toChart(wanted), nil
}

// splitByChartType turns one astro response into one payload per stored
// chart.
//
// The D1 keeps the whole response — it is the provenance record, and the
// AI layer reads it from Phase 5. The D9 is projected out as a chart in
// its own right, carrying the same meta so that every stored chart
// answers "which engine produced this?" on its own, without a join.
func splitByChartType(response *astroclient.ChartResponse) (map[string][]byte, error) {
	rasi, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("charts: encode rasi: %w", err)
	}
	payloads := map[string][]byte{ChartTypeRasi: rasi}

	if response.Navamsa == nil {
		return payloads, nil
	}

	navamsa, err := json.Marshal(struct {
		Meta      astroclient.ChartMeta          `json:"meta"`
		Ascendant *astroclient.AscendantPosition `json:"ascendant"`
		Houses    *[]astroclient.HousePosition   `json:"houses"`
		Planets   []astroclient.PlanetPosition   `json:"planets"`
	}{
		Meta:      response.Meta,
		Ascendant: response.Navamsa.Ascendant,
		Houses:    response.Navamsa.Houses,
		Planets:   response.Navamsa.Planets,
	})
	if err != nil {
		return nil, fmt.Errorf("charts: encode navamsa: %w", err)
	}
	payloads[ChartTypeNavamsa] = navamsa

	return payloads, nil
}

// failureCode reduces an error to an enum for analytics.
func failureCode(err error) string {
	switch {
	case clients.IsUnavailable(err):
		return "astro_unavailable"
	case clients.IsRejected(err):
		return "astro_rejected"
	default:
		return "internal"
	}
}

func toChart(row dbgen.Chart) Chart {
	return Chart{
		ID:            uuid.UUID(row.ID.Bytes),
		ProfileID:     uuid.UUID(row.BirthProfileID.Bytes),
		ChartType:     row.ChartType,
		Ayanamsa:      row.Ayanamsa,
		HouseSystem:   row.HouseSystem,
		EngineVersion: row.EngineVersion,
		Data:          row.ChartData,
		ComputedAt:    row.ComputedAt,
	}
}

func toPgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

// Recompute discards the stored chart and asks astro-service again.
//
// The one operation in this package that deliberately ignores storage.
// Everything else treats a stored chart as authoritative, because a chart
// is a pure function of its inputs; this exists for the case where the
// FUNCTION changed — a new ephemeris, a corrected ayanamsa, a fixed
// navamsa formula — and the stored answer is now the old engine's.
//
// It does not delete first. The upsert inside computeAndStore replaces
// the row in the same transaction that writes the new dasha tree, so a
// failed recompute leaves the previous chart intact rather than leaving
// the user with nothing while astro-service is unreachable.
func (s *Service) Recompute(ctx context.Context, userID uuid.UUID, key Key) (Chart, error) {
	key = key.withDefaults()

	profile, err := s.profiles.Get(ctx, userID, key.ProfileID)
	if err != nil {
		return Chart{}, err
	}

	chart, err := s.computeAndStore(ctx, userID, profile, key)
	if err != nil {
		return Chart{}, err
	}
	return chart, nil
}
