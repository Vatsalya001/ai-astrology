package clients

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients/astroclient"
)

// ErrAstroUnavailable means the compute service could not be reached.
//
// Separate from a validation failure on purpose. The chart service
// branches on it: unavailable means "serve the cached chart if there is
// one", whereas a 422 means we sent something wrong and no cache will
// help.
var ErrAstroUnavailable = errors.New("clients: astro-service unavailable")

// ErrAstroRejected means astro-service refused the request.
//
// A bug on our side — bad coordinates, a naive timestamp, an unknown
// ayanamsa. Never retried, and never masked by a cache.
var ErrAstroRejected = errors.New("clients: astro-service rejected the request")

// ChartInput is what api-service knows about a birth.
//
// A UTC instant rather than a local date and zone: Go resolved the
// historical offset once when the profile was created, and re-deriving
// it in Python would be a second implementation of the hardest part of
// the problem, free to disagree with the first.
type ChartInput struct {
	UTCInstant   time.Time
	Latitude     float64
	Longitude    float64
	TimeAccuracy string
	Ayanamsa     string
	HouseSystem  string
}

// ComputeChart asks astro-service for a full chart.
func (a *Astro) ComputeChart(ctx context.Context, in ChartInput) (*astroclient.ChartResponse, error) {
	// Idempotent by topology, not by hope: astro-service has no database
	// at all, so replaying this cannot duplicate a write. Stated at the
	// call site so a reviewer adding a stateful endpoint sees what they
	// would be claiming. See MarkIdempotent.
	ctx = MarkIdempotent(ctx)

	body := astroclient.ComputeChartV1ChartsComputePostJSONRequestBody{
		Birth: astroclient.BirthData{
			UtcInstant: in.UTCInstant,
			Latitude:   in.Latitude,
			Longitude:  in.Longitude,
		},
	}
	applyChartOptions(&body, in)

	resp, err := a.api.ComputeChartV1ChartsComputePostWithResponse(ctx, body)
	if err != nil {
		return nil, unavailable("compute chart", err)
	}

	if resp.JSON200 == nil {
		return nil, statusFailure("compute chart", resp.StatusCode(), resp.Body)
	}
	return resp.JSON200, nil
}

// ComputeTransits asks for current positions against a natal chart.
func (a *Astro) ComputeTransits(
	ctx context.Context,
	at time.Time,
	natalMoonSign int,
	natalAscendantSign *int,
) (*astroclient.TransitResponse, error) {
	ctx = MarkIdempotent(ctx)

	body := astroclient.ComputeTransitV1TransitsComputePostJSONRequestBody{
		At:            at,
		NatalMoonSign: natalMoonSign,
	}
	if natalAscendantSign != nil {
		body.NatalAscendantSign = natalAscendantSign
	}

	resp, err := a.api.ComputeTransitV1TransitsComputePostWithResponse(ctx, body)
	if err != nil {
		return nil, unavailable("compute transits", err)
	}
	if resp.JSON200 == nil {
		return nil, statusFailure("compute transits", resp.StatusCode(), resp.Body)
	}
	return resp.JSON200, nil
}

// unavailable classifies a transport-level failure.
//
// A tripped breaker is folded into the same error as a timeout or a
// refused connection: from the caller's point of view they are the same
// situation — astro cannot answer, so fall back to the cache.
func unavailable(operation string, err error) error {
	if errors.Is(err, ErrCircuitOpen) {
		return fmt.Errorf("%s: %w: circuit open", operation, ErrAstroUnavailable)
	}
	return fmt.Errorf("%s: %w: %v", operation, ErrAstroUnavailable, err)
}

// statusFailure classifies a non-200.
//
// The split is what makes the cache fallback correct. A 5xx is astro
// failing and a stale chart is better than an error page. A 4xx is US
// failing, and serving a cached chart would hide a bug that will produce
// wrong charts for every new profile.
func statusFailure(operation string, code int, body []byte) error {
	if code >= 500 {
		return fmt.Errorf("%s: %w: status %d", operation, ErrAstroUnavailable, code)
	}

	// The body is included for a 4xx because it is our own validation
	// error and the detail is actionable. It contains no user data —
	// astro-service returns field names and constraints, never the values
	// it was given, which matters because those values are birth data.
	return fmt.Errorf("%s: %w: status %d: %s", operation, ErrAstroRejected, code, truncate(body, 300))
}

func truncate(body []byte, limit int) string {
	if len(body) <= limit {
		return string(body)
	}
	return string(body[:limit]) + "…"
}

func applyChartOptions(body *astroclient.ComputeChartV1ChartsComputePostJSONRequestBody, in ChartInput) {
	if in.TimeAccuracy != "" {
		accuracy := astroclient.BirthDataTimeAccuracy(in.TimeAccuracy)
		body.Birth.TimeAccuracy = &accuracy
	}
	if in.Ayanamsa != "" {
		ayanamsa := astroclient.BirthDataAyanamsa(in.Ayanamsa)
		body.Birth.Ayanamsa = &ayanamsa
	}
	if in.HouseSystem != "" {
		system := astroclient.BirthDataHouseSystem(in.HouseSystem)
		body.Birth.HouseSystem = &system
	}
}

// IsUnavailable reports whether a chart can be served from cache instead.
func IsUnavailable(err error) bool {
	return errors.Is(err, ErrAstroUnavailable)
}

// IsRejected reports whether the request itself was bad.
func IsRejected(err error) bool {
	return errors.Is(err, ErrAstroRejected)
}
