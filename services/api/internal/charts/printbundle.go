package charts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/birthprofiles"
)

// Everything the printed document renders, in one response.
//
// ── Why one response and not five ──
//
// The print token is single use by design: redeeming it deletes it, so a
// token that leaks from a browser profile or a proxy log is already
// spent. That property and "the print page makes five API calls" are
// incompatible — the second call would 404.
//
// The alternative is a token with a use count, which is a worse trade:
// "valid five times" is far harder to reason about than "valid once",
// and the count would have to be tuned every time the print page grows a
// section. So the route returns a bundle and the page renders from it.
//
// ── Assembled server-side, never computed here ──
//
// Every field below is read from Postgres, where astro-service put it.
// Nothing in this file derives a position, a house or a date. See the
// first invariant.
type PrintBundle struct {
	Profile birthprofiles.Profile `json:"birth_profile"`

	// Keyed by chart type — "D1", "D9", "D10". A map rather than three
	// named fields so a chart type that fails to load is absent rather
	// than present-and-zero, which the page would render as an empty
	// twelve-house grid with no indication anything was wrong.
	Charts map[string]Chart `json:"charts"`

	// Level 1 only. The printed timeline shows the mahadasha sequence;
	// 819 rows of subdivision is not a thing anybody prints.
	Mahadashas []Period `json:"mahadashas"`

	// Nil when the profile has no birth time, because a chart with no
	// time has no dasha tree at all — the Moon cannot be pinned to a
	// pada without one. Nil rather than zero so the page can say so.
	Current *CurrentPeriods `json:"current_dasha"`

	// The server's instant, so the footer's date and the "you are here"
	// marker agree with each other and with the rest of the product.
	// A worker's headless Chrome has whatever clock the container has.
	GeneratedAt time.Time `json:"generated_at"`
}

// PrintBundle assembles one document's worth of chart data.
//
// `userID` and `profileID` come from a redeemed print token, never from
// the request — see printhandler.go. Every read below is scoped by both,
// so a token carrying a mismatched pair returns nothing rather than
// somebody else's chart.
func (s *Service) PrintBundle(
	ctx context.Context,
	userID, profileID uuid.UUID,
	at time.Time,
) (PrintBundle, error) {
	profile, err := s.profileFor(ctx, userID, profileID)
	if err != nil {
		// Includes "not yours" — birthprofiles.Get scopes by user and
		// returns not-found rather than forbidden for a stranger.
		return PrintBundle{}, fmt.Errorf("charts: print bundle profile: %w", err)
	}

	bundle := PrintBundle{
		Profile:     profile,
		Charts:      make(map[string]Chart, 3),
		GeneratedAt: at,
	}

	/*
	  D1 is required; D9 and D10 are not.

	  A missing divisional chart costs the document one page. A missing
	  rasi chart means there is no document, and rendering a PDF with an
	  empty main grid is worse than failing the job — the user keeps the
	  file and finds out later.
	*/
	rasi, err := s.GetOrStale(ctx, userID, Key{ProfileID: profileID, ChartType: ChartTypeRasi})
	if err != nil {
		return PrintBundle{}, fmt.Errorf("charts: print bundle rasi: %w", err)
	}
	bundle.Charts[ChartTypeRasi] = rasi

	for _, chartType := range []string{ChartTypeNavamsa, ChartTypeDasamsa} {
		divisional, err := s.GetOrStale(ctx, userID, Key{ProfileID: profileID, ChartType: chartType})
		if err != nil {
			// Logged, not returned. The document renders without it.
			s.logger.Warn("print bundle: divisional chart unavailable",
				"chart_type", chartType, "error", err)
			continue
		}
		bundle.Charts[chartType] = divisional
	}

	/*
	  Dashas are optional in exactly one case, and it is a real one: a
	  profile with `time_accuracy: unknown` has no stored tree, and
	  ErrNoDashas is how the service says so. Treating that as a failure
	  would mean nobody without a birth time could ever download a PDF.

	  Any OTHER error still fails the bundle — a database problem must
	  not silently produce a document missing its timeline.
	*/
	mahadashas, err := s.Dashas(ctx, userID, rasi.ID, 1)
	switch {
	case err == nil:
		bundle.Mahadashas = mahadashas
	case errors.Is(err, ErrNoDashas):
		// Left nil. The page renders the "no birth time" note.
	default:
		return PrintBundle{}, fmt.Errorf("charts: print bundle dashas: %w", err)
	}

	if bundle.Mahadashas != nil {
		current, err := s.Current(ctx, userID, rasi.ID, at)
		switch {
		case err == nil:
			bundle.Current = &current
		case errors.Is(err, ErrNoDashas):
			// The tree exists but does not cover `at` — a chart whose
			// 120-year cycle has run out. Rare, and not a failure.
		default:
			return PrintBundle{}, fmt.Errorf("charts: print bundle current: %w", err)
		}
	}

	return bundle, nil
}
