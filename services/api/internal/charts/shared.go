package charts

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

/*
The chart as a stranger holding a share link sees it.

── What is deliberately absent ──

No birth date. No birth time. No birth place, latitude, longitude or
timezone. Not because a viewer could not infer roughly when someone was
born from a chart — a competent astrologer can — but because inference
from a diagram and a machine-readable record are different things. The
security rules say birth date plus time plus place is, in combination,
close to a unique identifier and must be treated exactly like an email
address. A share link ends up in group chats; it must not be a way to
harvest that field triple.

What IS present is the chart itself and the label its owner gave the
profile. The owner chose both and chose to send them.

── Why this lives in `charts` and not in `shares` ──

Because it is a chart, and every read of a chart in this service is
scoped by user_id in the query. Building a second reader inside `shares`
would be a second path to the same data with its own predicate to
forget. `shares` resolves a token to (user, profile) and then asks here,
through the same door as everybody else.
*/
type SharedView struct {
	// Label is what the owner called this profile — "self", "amma", a
	// name. Their choice, and they are the one sharing it.
	Label string `json:"label"`

	ChartType string `json:"chart_type"`

	// The diagram's data. Positions, houses, nakshatras — the same
	// payload the owner's own screen renders.
	Data json.RawMessage `json:"chart_data"`

	Ayanamsa      string `json:"ayanamsa"`
	HouseSystem   string `json:"house_system"`
	EngineVersion string `json:"engine_version"`
}

// SharedChart loads the reduced view for a resolved share link.
//
// `userID` and `profileID` come from a resolved share row, never from a
// request. The read below is scoped by both, so a share row carrying a
// mismatched pair returns nothing rather than somebody else's chart —
// the same second line of defence the print bundle has.
func (s *Service) SharedChart(
	ctx context.Context,
	userID, profileID uuid.UUID,
) (SharedView, error) {
	profile, err := s.profileFor(ctx, userID, profileID)
	if err != nil {
		return SharedView{}, fmt.Errorf("charts: shared chart profile: %w", err)
	}

	/*
	   GetOrStale, not Get.

	   A shared link must keep working while astro-service is down, for
	   the same reason the owner's own screen does — and more so: the
	   person opening it has no idea our compute service exists and no
	   way to try again later in any informed sense.
	*/
	chart, err := s.GetOrStale(ctx, userID, Key{ProfileID: profileID, ChartType: ChartTypeRasi})
	if err != nil {
		return SharedView{}, fmt.Errorf("charts: shared chart: %w", err)
	}

	return SharedView{
		Label:         profile.Label,
		ChartType:     chart.ChartType,
		Data:          chart.Data,
		Ayanamsa:      chart.Ayanamsa,
		HouseSystem:   chart.HouseSystem,
		EngineVersion: chart.EngineVersion,
	}, nil
}
