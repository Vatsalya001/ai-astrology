package jobs_test

import (
	"testing"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/jobs"
)

// Two constants have to agree and nothing connects them in code: the
// cron spec that decides WHEN the refresh runs, and the interval that
// Refresher.Slot truncates to when it decides WHICH slot the result is
// stamped with.
//
// Change the cron to every four hours and leave the interval at six, and
// the job runs six times a day into four slots — two of every three runs
// overwriting a slot that was already correct, for no benefit and triple
// the load on astro-service. Nothing fails. Nothing logs. So this test
// parses the real spec and checks the fire times land exactly on the
// boundaries Slot produces.
func TestTheCronFiresExactlyOnSlotBoundaries(t *testing.T) {
	schedule, err := cron.ParseStandard(jobs.TransitRefreshCron)
	if err != nil {
		t.Fatalf("TransitRefreshCron %q does not parse: %v", jobs.TransitRefreshCron, err)
	}

	// Walk a full day from an awkward starting point — not midnight, not
	// on a boundary — and collect every fire time.
	at := time.Date(2026, 9, 16, 3, 17, 42, 0, time.UTC)
	end := at.Add(24 * time.Hour)

	var fires []time.Time
	for next := schedule.Next(at); next.Before(end); next = schedule.Next(next) {
		fires = append(fires, next)
	}

	wantPerDay := int(24 * time.Hour / jobs.TransitRefreshInterval)
	if len(fires) != wantPerDay {
		t.Fatalf("the cron fires %d times a day but the interval is %s, which is %d slots — "+
			"runs and slots have drifted apart", len(fires), jobs.TransitRefreshInterval, wantPerDay)
	}

	for _, fire := range fires {
		if truncated := fire.Truncate(jobs.TransitRefreshInterval); !truncated.Equal(fire) {
			t.Fatalf("the cron fires at %s, which is not a %s boundary — "+
				"Refresher.Slot would stamp that run with an earlier slot",
				fire.Format(time.RFC3339), jobs.TransitRefreshInterval)
		}
	}
}

// Retention has to outlast a plausible outage, because stored transits
// are the fallback served while astro-service is unreachable. A window
// measured in hours would delete the fallback during exactly the event
// it exists for.
func TestRetentionOutlastsManyMissedRuns(t *testing.T) {
	if jobs.TransitRetention <= 10*jobs.TransitRefreshInterval {
		t.Fatalf("retention %s is only %.1f refresh intervals; "+
			"a weekend-long outage would leave nothing to fall back on",
			jobs.TransitRetention,
			float64(jobs.TransitRetention)/float64(jobs.TransitRefreshInterval))
	}
}

// Retrying matters here in a way it does not for a job that runs every
// minute: the next scheduled attempt is six hours away, so a task that
// gives up on the first refused connection leaves the table stale for
// the whole window.
func TestTheRefreshTaskRetries(t *testing.T) {
	if jobs.TransitRefreshMaxRetry < 3 {
		t.Fatalf("max retry is %d; with six hours to the next scheduled run, "+
			"a brief astro restart would cost a full window of freshness",
			jobs.TransitRefreshMaxRetry)
	}
}
