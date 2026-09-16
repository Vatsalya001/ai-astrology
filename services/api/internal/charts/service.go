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

type Service struct {
	q        dbgen.Querier
	astro    *clients.Astro
	profiles *birthprofiles.Service
	logger   *slog.Logger
	events   analytics.Emitter
}

func NewService(
	q dbgen.Querier,
	astro *clients.Astro,
	profiles *birthprofiles.Service,
	logger *slog.Logger,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{q: q, astro: astro, profiles: profiles, logger: logger, events: analytics.Nop{}}
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

	data, err := json.Marshal(response)
	if err != nil {
		return Chart{}, fmt.Errorf("charts: encode: %w", err)
	}

	row, err := s.q.UpsertChart(ctx, dbgen.UpsertChartParams{
		BirthProfileID:    toPgUUID(key.ProfileID),
		ChartType:         key.ChartType,
		CalculationSystem: key.System,
		Ayanamsa:          key.Ayanamsa,
		HouseSystem:       key.HouseSystem,
		EngineVersion:     response.Meta.EngineVersion,
		ChartData:         data,
	})
	if err != nil {
		return Chart{}, fmt.Errorf("charts: persist: %w", err)
	}

	s.events.Emit(ctx, analytics.ChartGenerated, &userID, map[string]any{
		"chart_type":  key.ChartType,
		"duration_ms": int(time.Since(started).Milliseconds()),
		"cached":      false,
	})

	return toChart(row), nil
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
