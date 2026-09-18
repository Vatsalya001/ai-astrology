// Package transits keeps the gochara table fresh and reads it back
// rotated onto a user's natal Moon.
//
// The table has no user_id, and that is the design. A planet's sign and
// degree at an instant are the same for every person alive; what differs
// between users is only which HOUSE that sign falls in, counted from
// their natal Moon — and that is an integer subtraction, not a
// computation. Storing it per user would multiply the row count by the
// user count in order to save a subtraction, and would put birth-derived
// data into a table that is currently free of it.
//
// Two things follow, and both are load-bearing:
//
//   - The table is safe to share across all users and safe to cache
//     globally, because it contains nothing personal.
//   - Sade Sati — the most-asked-about transit in Indian astrology — can
//     be answered for any user from stored rows alone, with no call to
//     astro-service. It keeps working during an outage, like a stored
//     chart does.
package transits

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

const (
	SystemVedic    = "vedic"
	AyanamsaLahiri = "lahiri"

	// SignCount is the zodiac. Named because the rotation below reads as
	// arbitrary modular arithmetic otherwise.
	SignCount = 12
)

// ReferenceMoonSign is the natal Moon the refresher claims when it calls
// astro-service. Aries, index 0.
//
// astro's transit endpoint requires a natal Moon sign because it returns
// houses-from-Moon alongside the raw positions. The refresher has no user
// in hand, so it passes a fixed reference and DISCARDS every house field
// in the response.
//
// Persisting those houses would be the bug this package exists to avoid:
// every user whose Moon is not in Aries would be reading somebody else's
// gochara, and it would look completely plausible. TestStoredRowsAreGlobal
// pins it by refreshing under two different reference signs and requiring
// byte-identical rows.
const ReferenceMoonSign = 0

// Refresher recomputes the global transit table.
type Refresher struct {
	q        dbgen.Querier
	astro    *clients.Astro
	interval time.Duration
	logger   *slog.Logger
}

func NewRefresher(q dbgen.Querier, astro *clients.Astro, interval time.Duration, logger *slog.Logger) *Refresher {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	return &Refresher{q: q, astro: astro, interval: interval, logger: logger}
}

// Slot rounds an instant down to the refresh boundary.
//
// The worker computes positions AT the slot boundary rather than at
// whatever moment it happened to wake up. That makes every stored row
// reproducible from its own key: ask astro for `timestamp` again and you
// get the same numbers back. It also makes a retry — or two replicas that
// both got through — an upsert of the same row rather than a second row
// three minutes later, which is what the UNIQUE constraint is for.
func (r *Refresher) Slot(at time.Time) time.Time {
	return at.UTC().Truncate(r.interval)
}

// Refresh computes and stores every planet's position for the slot
// containing `at`, and returns how many rows it wrote.
func (r *Refresher) Refresh(ctx context.Context, at time.Time) (int, error) {
	slot := r.Slot(at)

	reference := ReferenceMoonSign
	response, err := r.astro.ComputeTransits(ctx, slot, reference, nil)
	if err != nil {
		// No partial write and no prune: the caller must be able to tell
		// "astro is down" from "there are no transits", because the second
		// is a much worse thing to show a user and the two look identical
		// once the table is empty.
		return 0, fmt.Errorf("transits: refresh %s: %w", slot.Format(time.RFC3339), err)
	}

	written := 0
	for _, position := range response.Transits {
		// Only the global fields. HouseFromMoon and HouseFromAscendant are
		// deliberately absent — see ReferenceMoonSign.
		metadata, marshalErr := json.Marshal(map[string]any{
			"longitude":  position.Longitude,
			"sign_index": position.SignIndex,
		})
		if marshalErr != nil {
			return written, fmt.Errorf("transits: encode %s: %w", position.Planet, marshalErr)
		}

		if _, upsertErr := r.q.UpsertTransit(ctx, dbgen.UpsertTransitParams{
			Planet:            position.Planet,
			Sign:              position.Sign,
			Degree:            position.Degree,
			IsRetrograde:      position.IsRetrograde,
			Timestamp:         slot,
			CalculationSystem: SystemVedic,
			Ayanamsa:          response.Ayanamsa,
			Metadata:          metadata,
		}); upsertErr != nil {
			return written, fmt.Errorf("transits: store %s: %w", position.Planet, upsertErr)
		}
		written++
	}

	/*
	   The Sade Sati windows, in the same pass.

	   Best-effort, and deliberately after the positions are safely
	   stored. A failure here costs the END DATES; a failure that took
	   the positions down with it would cost the whole transits screen,
	   and "Saturn is in your 12th" without "until March 2027" is far
	   better than nothing at all.

	   Logged at error level because a stale window is not obviously
	   wrong on screen — it is a date that is quietly a few hours out of
	   step with the positions beside it, and nothing else would notice.
	*/
	if err := r.refreshSadeSatiWindows(ctx, slot); err != nil {
		r.logger.ErrorContext(ctx, "sade sati windows not refreshed",
			slog.Time("slot", slot),
			slog.String("consequence", "end dates stay as they were; positions are current"),
			slog.Any("err", err))
	}

	r.logger.InfoContext(ctx, "transits refreshed",
		slog.Time("slot", slot),
		slog.Int("planets", written),
		slog.String("ayanamsa", response.Ayanamsa))

	return written, nil
}

// refreshSadeSatiWindows stores one row per natal Moon sign.
//
// Twelve rows, upserted on the sign, so a retry or a second replica
// rewrites rather than duplicates — the same reasoning as UpsertTransit.
func (r *Refresher) refreshSadeSatiWindows(ctx context.Context, slot time.Time) error {
	response, err := r.astro.ComputeSadeSatiWindows(ctx, slot)
	if err != nil {
		return fmt.Errorf("transits: sade sati windows %s: %w", slot.Format(time.RFC3339), err)
	}

	if len(response.Windows) != SignCount {
		// A short response would silently leave some signs on stale rows
		// while others moved, and nothing downstream could tell.
		return fmt.Errorf("transits: astro returned %d sade sati windows, want %d",
			len(response.Windows), SignCount)
	}

	for _, window := range response.Windows {
		params := dbgen.UpsertSadeSatiWindowParams{
			MoonSignIndex: int16(window.MoonSignIndex),
			MoonSign:      window.MoonSign,
			ComputedFor:   slot,
		}

		/*
		   Both dates or neither.

		   The column CHECK enforces the pair, so a half-populated row is
		   refused by Postgres rather than stored. Mapping them together
		   here means that refusal never fires for a reason this code
		   could have prevented — and if it ever does, it is astro
		   disagreeing with itself, which is worth failing loudly for.
		*/
		if window.StartedAt != nil && window.EndsAt != nil {
			params.StartedAt = pgtype.Timestamptz{Time: *window.StartedAt, Valid: true}
			params.EndsAt = pgtype.Timestamptz{Time: *window.EndsAt, Valid: true}
		}

		if _, err := r.q.UpsertSadeSatiWindow(ctx, params); err != nil {
			return fmt.Errorf("transits: store sade sati window for sign %d: %w",
				window.MoonSignIndex, err)
		}
	}

	return nil
}

// Prune deletes rows older than the retention window.
//
// Called only after a successful refresh. The other order — prune, then
// discover astro is down — empties the table during exactly the outage
// the table exists to survive.
func (r *Refresher) Prune(ctx context.Context, now time.Time, retention time.Duration) (int64, error) {
	if retention <= 0 {
		return 0, fmt.Errorf("transits: prune needs a positive retention, got %s", retention)
	}

	cutoff := now.UTC().Add(-retention)
	deleted, err := r.q.DeleteTransitsBefore(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("transits: prune before %s: %w", cutoff.Format(time.RFC3339), err)
	}
	if deleted > 0 {
		r.logger.InfoContext(ctx, "pruned old transits",
			slog.Time("cutoff", cutoff), slog.Int64("rows", deleted))
	}
	return deleted, nil
}
