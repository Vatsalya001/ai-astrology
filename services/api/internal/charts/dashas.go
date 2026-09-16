package charts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients/astroclient"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// The dasha tree is stored as rows with parent_id rather than as the
// JSON blob it arrives in, even though the blob is already inside
// charts.chart_data.
//
// The reason is one query: "which Mahadasha, Antardasha and
// Pratyantardasha are running right now?" That is the single most-asked
// question about a dasha, it is asked on every page load of the dasha
// screen, and against JSON it means fetching the whole tree and walking
// three levels in application code. Against rows it is one indexed
// range scan returning three rows — which is what FindDashaAt does.
//
// The blob stays too. It is the provenance record: what astro-service
// actually said, byte for byte.

// SystemVimshottari is the only dasha system in Phase 2. Named because
// the column has a default and a silent default is how a second system
// arrives mislabelled.
const SystemVimshottari = "vimshottari"

// MaxDashaLevel matches the CHECK constraint on the column.
const MaxDashaLevel = 3

// Period is a dasha as callers see it.
type Period struct {
	ID        uuid.UUID  `json:"id"`
	Planet    string     `json:"planet"`
	Start     time.Time  `json:"start"`
	End       time.Time  `json:"end"`
	Level     int        `json:"level"`
	ParentID  *uuid.UUID `json:"parent_id,omitempty"`
	ElapsedPc float64    `json:"elapsed_percent,omitempty"`
}

// CurrentPeriods is the answer to the question the dasha screen asks.
type CurrentPeriods struct {
	Maha       *Period   `json:"mahadasha"`
	Antar      *Period   `json:"antardasha"`
	Pratyantar *Period   `json:"pratyantardasha"`
	At         time.Time `json:"at"`
}

// ErrNoDashas means the chart has no stored tree.
//
// Distinct from an empty list, and it has a real cause: a chart computed
// without a birth time has no dashas at all, because the Moon's position
// cannot be pinned to a nakshatra pada without one. The API turns this
// into a message saying so rather than an empty screen.
var ErrNoDashas = errors.New("charts: no dasha tree stored")

// storeDashaTree replaces a chart's dasha rows.
//
// Delete-then-insert rather than upsert. The tree's shape can change
// between engine versions — a boundary moves, a period subdivides
// differently — and merging two shapes produces a tree that is neither.
// Replacing is the only operation whose result is still a tree.
//
// Runs inside the caller's transaction, because a half-written tree is
// worse than no tree at all: FindDashaAt would return a Mahadasha with
// no Antardasha beneath it, and the screen would render that as fact.
func storeDashaTree(
	ctx context.Context,
	q *dbgen.Queries,
	chartID uuid.UUID,
	periods []astroclient.DashaPeriodOut,
) (int, error) {
	if _, err := q.DeleteDashasForChart(ctx, toPgUUID(chartID)); err != nil {
		return 0, fmt.Errorf("charts: clear dasha tree: %w", err)
	}
	return insertDashaLevel(ctx, q, chartID, nil, periods)
}

// insertDashaLevel writes one level and recurses into its children.
func insertDashaLevel(
	ctx context.Context,
	q *dbgen.Queries,
	chartID uuid.UUID,
	parentID *uuid.UUID,
	periods []astroclient.DashaPeriodOut,
) (int, error) {
	written := 0

	for _, period := range periods {
		if period.Level < 1 || period.Level > MaxDashaLevel {
			// The CHECK constraint would catch this, but as a 500 naming a
			// constraint. Caught here it names the planet and the level.
			return written, fmt.Errorf(
				"charts: dasha %s has level %d, outside 1..%d",
				period.Planet, period.Level, MaxDashaLevel)
		}
		if !period.End.After(period.Start) {
			// Also a CHECK constraint, and also worth a better message:
			// this is precisely the failure mode float accumulation across
			// three levels of subdivision produces, and it looks plausible
			// in a UI.
			return written, fmt.Errorf(
				"charts: dasha %s ends at %s, which is not after its start %s",
				period.Planet, period.End.Format(time.RFC3339), period.Start.Format(time.RFC3339))
		}

		row, err := q.InsertDasha(ctx, dbgen.InsertDashaParams{
			ChartID:   toPgUUID(chartID),
			System:    SystemVimshottari,
			Planet:    period.Planet,
			StartDate: period.Start,
			EndDate:   period.End,
			Level:     int16(period.Level),
			ParentID:  optionalPgUUID(parentID),
			Metadata:  []byte(`{}`),
		})
		if err != nil {
			return written, fmt.Errorf("charts: insert dasha %s level %d: %w",
				period.Planet, period.Level, err)
		}
		written++

		if period.Children == nil {
			continue
		}
		childID := uuid.UUID(row.ID.Bytes)
		childCount, err := insertDashaLevel(ctx, q, chartID, &childID, *period.Children)
		if err != nil {
			return written + childCount, err
		}
		written += childCount
	}

	return written, nil
}

// MoonSign returns the natal Moon's sign name for a profile.
//
// It satisfies transits.NatalMoon. The sign NAME rather than an index:
// this package knows the shape of chart JSON, the transits package owns
// the zodiac ordering, and neither needs to learn the other's job.
//
// GetOrStale rather than Get, so a natal transit reading keeps working
// while astro-service is down — which is the whole point of that
// endpoint, since the transit table it reads from is already stored.
func (s *Service) MoonSign(ctx context.Context, userID, profileID uuid.UUID) (string, error) {
	chart, err := s.GetOrStale(ctx, userID, Key{ProfileID: profileID})
	if err != nil {
		return "", err
	}

	// Only the summary is decoded. The full chart is large, it is decoded
	// on every transit request, and every field but this one would be
	// thrown away.
	var envelope struct {
		Summary struct {
			MoonSign string `json:"moon_sign"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(chart.Data, &envelope); err != nil {
		return "", fmt.Errorf("charts: decode summary for the natal moon: %w", err)
	}
	if envelope.Summary.MoonSign == "" {
		// The Moon's SIGN does not need a birth time — only the
		// ascendant and the houses do — so an empty one here means a
		// malformed stored chart rather than an unknown-time profile.
		return "", fmt.Errorf("charts: stored chart has no moon sign")
	}
	return envelope.Summary.MoonSign, nil
}

// Dashas returns one level of a chart's tree.
func (s *Service) Dashas(ctx context.Context, userID uuid.UUID, chartID uuid.UUID, level int) ([]Period, error) {
	if level < 1 || level > MaxDashaLevel {
		return nil, fmt.Errorf("charts: level %d is outside 1..%d", level, MaxDashaLevel)
	}

	rows, err := s.q.ListDashasByLevel(ctx, dbgen.ListDashasByLevelParams{
		ChartID: toPgUUID(chartID),
		Level:   int16(level),
		UserID:  toPgUUID(userID),
	})
	if err != nil {
		return nil, fmt.Errorf("charts: list dashas: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrNoDashas
	}

	out := make([]Period, 0, len(rows))
	for _, row := range rows {
		out = append(out, toPeriod(row))
	}
	return out, nil
}

// Current returns the Maha, Antar and Pratyantar running at an instant.
//
// One round trip, because FindDashaAt returns one row per level. The
// alternative — three queries, or one fetch of the whole tree walked in
// Go — is the thing storing rows was for.
func (s *Service) Current(
	ctx context.Context, userID, chartID uuid.UUID, at time.Time,
) (CurrentPeriods, error) {
	rows, err := s.q.FindDashaAt(ctx, dbgen.FindDashaAtParams{
		ChartID: toPgUUID(chartID),
		UserID:  toPgUUID(userID),
		At:      at,
	})
	if err != nil {
		return CurrentPeriods{}, fmt.Errorf("charts: find dasha at %s: %w",
			at.Format(time.RFC3339), err)
	}
	if len(rows) == 0 {
		return CurrentPeriods{}, ErrNoDashas
	}

	result := CurrentPeriods{At: at}
	for _, row := range rows {
		period := toPeriod(row)
		period.ElapsedPc = elapsedPercent(period, at)

		switch row.Level {
		case 1:
			result.Maha = &period
		case 2:
			result.Antar = &period
		case 3:
			result.Pratyantar = &period
		}
	}
	return result, nil
}

// elapsedPercent is how far through a period the instant is.
//
// The dasha screen's progress bar. Computed here rather than in the UI
// because "how far through" is arithmetic on two timestamps, and doing it
// in three clients gives three answers on the boundary.
func elapsedPercent(period Period, at time.Time) float64 {
	total := period.End.Sub(period.Start)
	if total <= 0 {
		return 0
	}
	elapsed := at.Sub(period.Start)
	if elapsed <= 0 {
		return 0
	}
	if elapsed >= total {
		return 100
	}
	return float64(elapsed) / float64(total) * 100
}

func toPeriod(row dbgen.Dasha) Period {
	period := Period{
		ID:     uuid.UUID(row.ID.Bytes),
		Planet: row.Planet,
		Start:  row.StartDate,
		End:    row.EndDate,
		Level:  int(row.Level),
	}
	if row.ParentID.Valid {
		parent := uuid.UUID(row.ParentID.Bytes)
		period.ParentID = &parent
	}
	return period
}

func optionalPgUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *id, Valid: true}
}
