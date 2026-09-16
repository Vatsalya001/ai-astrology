package clients_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients"
)

// The Go side of the astro call: retries, the breaker, and the error
// classification the cache fallback depends on.
//
// astro-service is stubbed with httptest rather than run for real. What
// is under test is how this client behaves when the far side misbehaves —
// timing out, 500ing, 422ing — and a real service is hard to make do
// those things on demand.

const chartOK = `{
	"meta": {"schema_version":1,"calculation_system":"vedic","ayanamsa":"lahiri",
	         "ayanamsa_value":23.8,"house_system":"whole_sign",
	         "engine_version":"skyfield-1.55+de421+schema1",
	         "computed_at":"2026-01-01T00:00:00Z","time_accuracy":"exact"},
	"ascendant": null, "houses": null, "dashas": null, "navamsa": null,
	"planets": [], "yogas": [],
	"summary": {"sun_sign":"Leo","moon_sign":"Sagittarius","ascendant_sign":null,
	            "moon_nakshatra":"Mula","moon_nakshatra_pada":4}
}`

func jaipur() clients.ChartInput {
	return clients.ChartInput{
		UTCInstant: time.Date(1994, 8, 17, 9, 5, 0, 0, time.UTC),
		Latitude:   26.9124,
		Longitude:  75.7873,
	}
}

func astroStub(t *testing.T, handler http.HandlerFunc) *clients.Astro {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := clients.NewAstro(server.URL, "test-token", 2*time.Second)
	if err != nil {
		t.Fatalf("NewAstro: %v", err)
	}
	return client
}

func TestAChartComesBack(t *testing.T) {
	client := astroStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Token") != "test-token" {
			t.Errorf("the internal token was not sent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chartOK))
	})

	chart, err := client.ComputeChart(context.Background(), jaipur())
	if err != nil {
		t.Fatalf("ComputeChart: %v", err)
	}
	if chart.Summary.MoonSign != "Sagittarius" {
		t.Errorf("moon_sign = %q", chart.Summary.MoonSign)
	}
}

// THE reason MarkIdempotent exists.
//
// Every astro route is a POST — birth data is too large for a query
// string — and the shared retry transport excludes POST by default,
// correctly: a replayed POST is a duplicated write for most services.
//
// astro-service has no database at all, so it has no writes to
// duplicate. The client opts in explicitly, and this asserts the opt-in
// actually reaches the transport. Without it a 500 would fail on the
// first attempt and the retry policy would be decorative.
func TestAPostIsRetriedBecauseTheCallerMarkedItIdempotent(t *testing.T) {
	var attempts atomic.Int32

	client := astroStub(t, func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chartOK))
	})

	if _, err := client.ComputeChart(context.Background(), jaipur()); err != nil {
		t.Fatalf("ComputeChart: %v", err)
	}

	if got := attempts.Load(); got != 3 {
		t.Errorf("the request was attempted %d times, want 3 — is the POST being retried?", got)
	}
}

// The complement, and the more important half.
//
// Retrying a 4xx is pointless — the request is wrong and will be wrong
// again — and doing it would triple the load on a service while it
// rejects us.
func TestAFourHundredIsNotRetried(t *testing.T) {
	var attempts atomic.Int32

	client := astroStub(t, func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"detail":"latitude must be between -90 and 90"}`))
	})

	_, err := client.ComputeChart(context.Background(), jaipur())
	if err == nil {
		t.Fatal("a 422 was reported as success")
	}
	if attempts.Load() != 1 {
		t.Errorf("a 422 was attempted %d times; it must not be retried", attempts.Load())
	}
}

// The classification the whole cache fallback rests on.
//
// A 5xx is astro failing, and a stale chart beats an error page. A 4xx is
// US failing, and serving a cached chart would hide a bug that produces
// wrong charts for every new profile from then on.
func TestFailuresAreClassifiedForTheCacheFallback(t *testing.T) {
	t.Run("a 500 is unavailable, so the cache may answer", func(t *testing.T) {
		client := astroStub(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})

		_, err := client.ComputeChart(context.Background(), jaipur())
		if !clients.IsUnavailable(err) {
			t.Errorf("a 500 was not classified as unavailable: %v", err)
		}
		if clients.IsRejected(err) {
			t.Error("a 500 was also classified as rejected; the two must not overlap")
		}
	})

	t.Run("a 422 is rejected, so the cache must NOT answer", func(t *testing.T) {
		client := astroStub(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
		})

		_, err := client.ComputeChart(context.Background(), jaipur())
		if !clients.IsRejected(err) {
			t.Errorf("a 422 was not classified as rejected: %v", err)
		}
		if clients.IsUnavailable(err) {
			t.Error("a 422 was classified as unavailable; a cached chart would hide the bug")
		}
	})

	t.Run("a dead connection is unavailable", func(t *testing.T) {
		// A server that is closed before the call: the transport-level
		// failure a real outage produces.
		server := httptest.NewServer(http.NotFoundHandler())
		url := server.URL
		server.Close()

		client, err := clients.NewAstro(url, "t", 500*time.Millisecond)
		if err != nil {
			t.Fatalf("NewAstro: %v", err)
		}

		_, err = client.ComputeChart(context.Background(), jaipur())
		if !clients.IsUnavailable(err) {
			t.Errorf("a refused connection was not classified as unavailable: %v", err)
		}
	})
}

// The breaker, which is what makes an outage cheap.
//
// Without it every request during an outage spends its full timeout and
// both retries — ten seconds times three — while holding a goroutine and
// a connection. The breaker turns that into an immediate failure so the
// caller can reach for the cache.
func TestTheBreakerOpensAfterRepeatedFailures(t *testing.T) {
	var attempts atomic.Int32

	client := astroStub(t, func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})

	// Enough calls to exceed the threshold. Each is retried, so the
	// breaker sees several failures per call.
	for range 10 {
		_, _ = client.ComputeChart(context.Background(), jaipur())
	}

	before := attempts.Load()

	// Once open, further calls must not reach the network at all.
	for range 5 {
		_, err := client.ComputeChart(context.Background(), jaipur())
		if !clients.IsUnavailable(err) {
			t.Fatalf("expected an unavailable error while the circuit is open, got %v", err)
		}
	}

	if after := attempts.Load(); after != before {
		t.Errorf("%d requests reached the server after the circuit opened; expected 0",
			after-before)
	}
}

// A 4xx must not trip the breaker.
//
// Our own bad request taking astro down for every other caller would turn
// one caller's bug into a total outage.
func TestAFourHundredDoesNotTripTheBreaker(t *testing.T) {
	var attempts atomic.Int32

	client := astroStub(t, func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusUnprocessableEntity)
	})

	for range 20 {
		_, _ = client.ComputeChart(context.Background(), jaipur())
	}

	// Still reaching the network: 20 calls, no retries on 4xx.
	if got := attempts.Load(); got != 20 {
		t.Errorf("%d requests reached the server; the breaker tripped on 4xx", got)
	}
}

// A cancelled context must abandon the call rather than burning retries.
//
// If a user closes the tab mid-computation, continuing to retry spends
// astro-service's capacity on an answer nobody will read.
func TestACancelledContextStopsImmediately(t *testing.T) {
	var attempts atomic.Int32

	client := astroStub(t, func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusInternalServerError)
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.ComputeChart(ctx, jaipur())
	if err == nil {
		t.Fatal("a cancelled context produced a chart")
	}
	if !errors.Is(err, context.Canceled) && !clients.IsUnavailable(err) {
		t.Errorf("unexpected error for a cancelled context: %v", err)
	}
	if got := attempts.Load(); got > 1 {
		t.Errorf("a cancelled request was attempted %d times", got)
	}
}

// The nullability that survived from Python, checked once more where it
// is actually consumed.
func TestAnUnknownTimeChartArrivesWithNilAscendant(t *testing.T) {
	client := astroStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chartOK))
	})

	chart, err := client.ComputeChart(context.Background(), jaipur())
	if err != nil {
		t.Fatalf("ComputeChart: %v", err)
	}

	if chart.Ascendant != nil {
		t.Error("a null ascendant became non-nil crossing the client")
	}
	if chart.Summary.AscendantSign != nil {
		t.Error("a null ascendant_sign became non-nil")
	}
}

// Coordinates must not be narrowed on the wire.
//
// The generated client used float32 until the contract declared
// format: double. The narrowing measured at 24 milliarcseconds — harmless
// for degrees, but the same default would have silently destroyed a
// Julian day, which needs seven digits before the decimal point alone.
func TestCoordinatesSurviveAsDoubles(t *testing.T) {
	var received struct {
		Birth struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"birth"`
	}

	client := astroStub(t, func(w http.ResponseWriter, r *http.Request) {
		if err := decodeJSON(r, &received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chartOK))
	})

	in := jaipur()
	if _, err := client.ComputeChart(context.Background(), in); err != nil {
		t.Fatalf("ComputeChart: %v", err)
	}

	if received.Birth.Latitude != in.Latitude {
		t.Errorf("latitude arrived as %.17g, sent %.17g — narrowed in transit",
			received.Birth.Latitude, in.Latitude)
	}
	if received.Birth.Longitude != in.Longitude {
		t.Errorf("longitude arrived as %.17g, sent %.17g — narrowed in transit",
			received.Birth.Longitude, in.Longitude)
	}
}

func decodeJSON(r *http.Request, into any) error {
	defer func() { _ = r.Body.Close() }()
	return json.NewDecoder(r.Body).Decode(into)
}

// The breaker sits OUTSIDE the retry transport, and this is what makes
// that observable.
//
// Counting CALLS to open, not failures. With the breaker outside, one
// logical call — request plus its retries — is one observation, so it
// takes `threshold` calls. Inside, each retry is its own observation and
// the breaker trips about three times sooner than configured.
//
// Five consecutive failures is what an operator means by the threshold,
// and they mean five failed operations, not five failed packets. Without
// this test the ordering was only a comment: swapping it passed the
// entire suite, which is how I found out the claim was unpinned.
func TestTheBreakerCountsCallsNotRetries(t *testing.T) {
	var attempts atomic.Int32

	client := astroStub(t, func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})

	callsToOpen := 0
	for call := 1; call <= 12; call++ {
		before := attempts.Load()
		_, _ = client.ComputeChart(context.Background(), jaipur())

		// A call that never reached the network is a call the breaker
		// refused — so the circuit opened on the previous one.
		if attempts.Load() == before {
			callsToOpen = call - 1
			break
		}
	}

	if callsToOpen == 0 {
		t.Fatal("the circuit never opened in twelve calls")
	}

	// With the breaker outside retry: one observation per call, so it
	// opens on the 5th. Inside: three observations per call, so it opens
	// on the 2nd. The assertion is deliberately tight enough to tell
	// those apart.
	if callsToOpen < 4 {
		t.Errorf("the circuit opened after %d calls; with a threshold of 5 that means "+
			"retries are being counted individually — is the breaker inside the retry "+
			"transport?", callsToOpen)
	}
}
