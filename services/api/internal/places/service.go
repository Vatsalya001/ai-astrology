// Package places serves the birth-place lookup.
//
// Self-hosted GeoNames: free, offline, no rate limit, and faster than any
// API call. That last point is the product argument — place selection
// sits at the highest drop-off point in the whole funnel, so a network
// round trip there is a conversion cost.
package places

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// MaxQueryLength caps the search term.
//
// An unbounded ILIKE against 200k rows is cheap to abuse: the attacker
// sends a megabyte, Postgres scans, and the cost is entirely ours. Long
// place names exist — "Llanfairpwllgwyngyll..." is 58 characters — so the
// cap is generous rather than tight.
const MaxQueryLength = 80

// MinQueryLength is where prefix search starts being useful.
//
// A single character matches tens of thousands of rows and ranks them by
// population, which returns the same handful of megacities for "a", "b"
// and "c". Two characters is the point at which the answer relates to
// what was typed.
const MinQueryLength = 2

var (
	ErrQueryTooShort = errors.New("places: query too short")
	ErrQueryTooLong  = errors.New("places: query too long")
)

// Place is a birth-place candidate.
type Place struct {
	ID          int32   `json:"id"`
	Name        string  `json:"name"`
	Admin1      string  `json:"admin1"`
	CountryCode string  `json:"country_code"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Timezone    string  `json:"timezone"`
	Population  int32   `json:"population"`
}

type Service struct {
	q dbgen.Querier
}

func NewService(q dbgen.Querier) *Service {
	return &Service{q: q}
}

// Search returns places whose ASCII name starts with the query, ranked by
// population.
//
// Population ranking is the whole feature. "jaip" must return Jaipur,
// Rajasthan — population 3 million — ahead of Jaipur, Odisha, a village
// of a few hundred. Alphabetical or insertion order would put the village
// first about as often as not, and a user who does not see their city in
// the first three results types something else or gives up.
func (s *Service) Search(ctx context.Context, query string, limit int32) ([]Place, error) {
	normalised, err := NormaliseQuery(query)
	if err != nil {
		return nil, err
	}

	rows, err := s.q.SearchPlaces(ctx, dbgen.SearchPlacesParams{
		Column1: &normalised,
		Limit:   limit,
	})
	if err != nil {
		return nil, fmt.Errorf("places: search: %w", err)
	}

	out := make([]Place, 0, len(rows))
	for _, row := range rows {
		out = append(out, toPlace(row))
	}
	return out, nil
}

// Get resolves a place the user selected, by its GeoNames id.
//
// Selection goes through this rather than trusting coordinates from the
// client: a browser could send any latitude it liked, and the chart would
// be computed for wherever that is.
func (s *Service) Get(ctx context.Context, id int32) (Place, error) {
	row, err := s.q.GetPlace(ctx, id)
	if err != nil {
		return Place{}, fmt.Errorf("places: get %d: %w", id, err)
	}
	return toPlace(row), nil
}

// NormaliseQuery validates and cleans a search term.
//
// Exported because the cache key must be built from the SAME normalised
// form the query uses. Caching on the raw input would give "Jaipur",
// "jaipur" and " jaipur " three separate entries for one answer — and,
// worse, would let a caller fill the cache with case variations.
func NormaliseQuery(query string) (string, error) {
	trimmed := strings.TrimSpace(query)

	// Count runes, not bytes. Devanagari place names are three bytes per
	// character, so a byte cap would reject a short Hindi query while
	// accepting a long English one.
	if utf8Len(trimmed) < MinQueryLength {
		return "", ErrQueryTooShort
	}
	if utf8Len(trimmed) > MaxQueryLength {
		return "", ErrQueryTooLong
	}

	// Collapse internal whitespace: "new   delhi" and "new delhi" are the
	// same search and must not be two cache entries.
	fields := strings.FieldsFunc(trimmed, unicode.IsSpace)
	return strings.ToLower(strings.Join(fields, " ")), nil
}

func utf8Len(s string) int {
	return len([]rune(s))
}

func toPlace(row dbgen.Place) Place {
	admin1 := ""
	if row.Admin1 != nil {
		admin1 = *row.Admin1
	}
	return Place{
		ID:          row.ID,
		Name:        row.Name,
		Admin1:      admin1,
		CountryCode: row.CountryCode,
		Latitude:    row.Latitude,
		Longitude:   row.Longitude,
		Timezone:    row.Timezone,
		Population:  row.Population,
	}
}
