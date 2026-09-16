package transits

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// Sade Sati phase names.
//
// These match astro-service's Literal["rising", "peak", "setting"]
// exactly. They are duplicated rather than imported because Go and Python
// cannot share a constant, and PhasesMatchAstroService in the test file
// asserts the two lists have not drifted.
const (
	PhaseRising  = "rising"  // Saturn in the 12th from the natal Moon
	PhasePeak    = "peak"    // the 1st — Saturn over the Moon itself
	PhaseSetting = "setting" // the 2nd
)

// Global is a transit with no natal frame: exactly what the shared table
// holds, and true for every person alive at that instant.
type Global struct {
	Planet       string    `json:"planet"`
	Sign         string    `json:"sign"`
	SignIndex    int       `json:"sign_index"`
	Degree       float64   `json:"degree"`
	Longitude    float64   `json:"longitude"`
	IsRetrograde bool      `json:"is_retrograde"`
	Timestamp    time.Time `json:"timestamp"`
}

// Position is a transit as one particular user sees it.
//
// A separate type from Global rather than a Global with a zero house.
// A caller holding a Position knows the house is meaningful; a caller
// holding a Global cannot read a house at all, which is better than
// reading a 0 that looks like an answer.
type Position struct {
	Global

	// HouseFromMoon is 1..12, counted inclusively from the natal Moon's
	// sign — the Vedic convention, in which the Moon's own sign is the
	// 1st house and not the 0th. Computed per request and never stored;
	// storing it is the bug ReferenceMoonSign documents.
	HouseFromMoon int `json:"house_from_moon"`
}

// SadeSati is Saturn's seven-and-a-half-year passage over the natal Moon.
type SadeSati struct {
	IsActive       bool    `json:"is_active"`
	Phase          *string `json:"phase"`
	SaturnSign     string  `json:"saturn_sign"`
	HousesFromMoon int     `json:"houses_from_moon"`
}

// Reader serves stored transits.
type Reader struct {
	q dbgen.Querier
}

func NewReader(q dbgen.Querier) *Reader { return &Reader{q: q} }

// ErrNoTransits means the table has nothing at or before the instant
// asked for.
//
// A distinct error rather than an empty slice: "we have not computed
// transits yet" and "no planets are transiting" are wildly different
// statements, and only one of them is ever true.
var ErrNoTransits = fmt.Errorf("transits: none stored")

// GlobalAt returns the most recent stored position of every planet at or
// before `at`, with no natal frame.
//
// Safe to cache across every user, and free of personal data, because
// that is exactly what the table holds.
func (r *Reader) GlobalAt(ctx context.Context, at time.Time) ([]Global, error) {
	rows, err := r.q.ListTransitsAt(ctx, dbgen.ListTransitsAtParams{
		Timestamp:         at.UTC(),
		CalculationSystem: SystemVedic,
		Ayanamsa:          AyanamsaLahiri,
	})
	if err != nil {
		return nil, fmt.Errorf("transits: load: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrNoTransits
	}

	out := make([]Global, 0, len(rows))
	for _, row := range rows {
		position, convErr := toGlobal(row)
		if convErr != nil {
			return nil, convErr
		}
		out = append(out, position)
	}
	return out, nil
}

// At returns the same positions rotated onto the caller's natal Moon.
func (r *Reader) At(ctx context.Context, at time.Time, natalMoonSign int) ([]Position, error) {
	if natalMoonSign < 0 || natalMoonSign >= SignCount {
		return nil, fmt.Errorf("transits: natal moon sign %d is outside 0..11", natalMoonSign)
	}

	globals, err := r.GlobalAt(ctx, at)
	if err != nil {
		return nil, err
	}

	out := make([]Position, 0, len(globals))
	for _, global := range globals {
		out = append(out, Position{
			Global:        global,
			HouseFromMoon: HouseFromMoon(global.SignIndex, natalMoonSign),
		})
	}
	return out, nil
}

// SadeSatiAt answers the question users actually ask, from stored rows
// alone — so it keeps answering while astro-service is down.
func (r *Reader) SadeSatiAt(ctx context.Context, at time.Time, natalMoonSign int) (SadeSati, error) {
	positions, err := r.At(ctx, at, natalMoonSign)
	if err != nil {
		return SadeSati{}, err
	}

	for _, position := range positions {
		if position.Planet != "Saturn" {
			continue
		}
		return sadeSatiFrom(position), nil
	}

	// Saturn missing from a non-empty table means a partial refresh, not
	// an absence of Saturn. Saying "no Sade Sati" here would be a
	// confident wrong answer to the most consequential question the
	// product answers.
	return SadeSati{}, fmt.Errorf("%w: Saturn is absent from the stored set", ErrNoTransits)
}

func sadeSatiFrom(saturn Position) SadeSati {
	result := SadeSati{
		SaturnSign:     saturn.Sign,
		HousesFromMoon: saturn.HouseFromMoon,
	}

	switch saturn.HouseFromMoon {
	case 12:
		phase := PhaseRising
		result.IsActive, result.Phase = true, &phase
	case 1:
		phase := PhasePeak
		result.IsActive, result.Phase = true, &phase
	case 2:
		phase := PhaseSetting
		result.IsActive, result.Phase = true, &phase
	}
	return result
}

// HouseFromMoon counts inclusively from the natal Moon's sign.
//
// Inclusive: the Moon's own sign is house 1. The +1 is the entire Vedic
// convention and dropping it shifts every transit reading by one house —
// which still produces a plausible-looking answer, which is why it gets
// its own exported function and its own table-driven test rather than
// living inline.
func HouseFromMoon(signIndex, natalMoonSign int) int {
	// Go's % keeps the sign of the dividend, so a planet behind the Moon
	// gives a negative remainder without the second modulo.
	return ((signIndex-natalMoonSign)%SignCount+SignCount)%SignCount + 1
}

func toGlobal(row dbgen.Transit) (Global, error) {
	var metadata struct {
		Longitude float64 `json:"longitude"`
		SignIndex int     `json:"sign_index"`
	}
	if err := json.Unmarshal(row.Metadata, &metadata); err != nil {
		return Global{}, fmt.Errorf("transits: decode metadata for %s: %w", row.Planet, err)
	}

	return Global{
		Planet:       row.Planet,
		Sign:         row.Sign,
		SignIndex:    metadata.SignIndex,
		Degree:       row.Degree,
		Longitude:    metadata.Longitude,
		IsRetrograde: row.IsRetrograde,
		Timestamp:    row.Timestamp,
	}, nil
}
