//go:build integration

package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis/ratelimit"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/testsupport"
)

// The per-IP backstop.
//
// GlobalPerIP was declared with the comment "applies to every request
// regardless of route" and was wired to nothing at all — which is the
// same class of bug as a guard that is never observed to fire, except
// the guard did not exist. These tests are what makes the comment true.

const testSalt = "integration-test-salt-not-a-secret"

func startRedis(ctx context.Context, t *testing.T) (*goredis.Client, func()) {
	t.Helper()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor: wait.ForLog("Ready to accept connections").
				WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		testsupport.ContainerUnavailable(t, "Redis", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatalf("mapped port: %v", err)
	}

	client := goredis.NewClient(&goredis.Options{Addr: host + ":" + port.Port()})
	return client, func() {
		_ = client.Close()
		_ = container.Terminate(ctx)
	}
}

func throttled(t *testing.T) (http.Handler, *int, func()) {
	t.Helper()

	client, stop := startRedis(context.Background(), t)

	reached := 0
	handler := GlobalThrottle(ratelimit.New(client), false, testSalt)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			reached++
			w.WriteHeader(http.StatusOK)
		}))

	return handler, &reached, stop
}

func fromIP(ip string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil)
	r.RemoteAddr = ip + ":51234"
	return r
}

// An endpoint with no rule of its own — logout, providers, the read-only
// profile routes — is still limited, because this sits above all of them.
func TestGlobalThrottleLimitsAnUnlistedRoute(t *testing.T) {
	handler, reached, stop := throttled(t)
	defer stop()

	var allowed, denied int
	for range ratelimit.GlobalPerIP.Max + 20 {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, fromIP("203.0.113.7"))

		switch rec.Code {
		case http.StatusOK:
			allowed++
		case http.StatusTooManyRequests:
			denied++
			if rec.Header().Get("Retry-After") == "" {
				t.Error("a 429 with no Retry-After leaves the client guessing")
			}
		default:
			t.Fatalf("unexpected status %d", rec.Code)
		}
	}

	if allowed != ratelimit.GlobalPerIP.Max {
		t.Errorf("%d requests were allowed, want %d", allowed, ratelimit.GlobalPerIP.Max)
	}
	if denied != 20 {
		t.Errorf("%d requests were denied, want 20", denied)
	}
	// The handler behind it must never run for a denied request.
	if *reached != ratelimit.GlobalPerIP.Max {
		t.Errorf("the wrapped handler ran %d times, want %d", *reached, ratelimit.GlobalPerIP.Max)
	}
}

// One noisy address must not lock out everybody, which is what a single
// shared window would do.
func TestGlobalThrottleIsPerAddress(t *testing.T) {
	handler, _, stop := throttled(t)
	defer stop()

	for range ratelimit.GlobalPerIP.Max + 1 {
		handler.ServeHTTP(httptest.NewRecorder(), fromIP("203.0.113.7"))
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, fromIP("198.51.100.4"))

	if rec.Code != http.StatusOK {
		t.Fatalf("a second address got %d; one client's flood must not affect another", rec.Code)
	}
}

// The Redis key must not contain the address. A raw IP is PII under the
// project's rules and a Redis key is persistence.
func TestGlobalThrottleStoresNoRawAddress(t *testing.T) {
	client, stop := startRedis(context.Background(), t)
	defer stop()

	handler := GlobalThrottle(ratelimit.New(client), false, testSalt)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))

	const ip = "203.0.113.7"
	handler.ServeHTTP(httptest.NewRecorder(), fromIP(ip))

	keys, err := client.Keys(context.Background(), "*").Result()
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	if len(keys) == 0 {
		t.Fatal("no key was written; the limiter did not record the request")
	}
	for _, key := range keys {
		if strings.Contains(key, ip) {
			t.Errorf("the Redis key contains the raw address: %s", key)
		}
	}
}

// CORS preflight carries no credentials and does no work. Counting it
// would mean a page making N cross-origin calls burns 2N of its budget.
func TestGlobalThrottleIgnoresPreflight(t *testing.T) {
	handler, _, stop := throttled(t)
	defer stop()

	for range ratelimit.GlobalPerIP.Max + 50 {
		rec := httptest.NewRecorder()
		r := fromIP("203.0.113.7")
		r.Method = http.MethodOptions
		handler.ServeHTTP(rec, r)

		if rec.Code == http.StatusTooManyRequests {
			t.Fatal("a preflight was rate limited")
		}
	}

	// And the budget is untouched, so a real request still gets through.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, fromIP("203.0.113.7"))
	if rec.Code != http.StatusOK {
		t.Fatalf("preflights consumed the budget: a real request got %d", rec.Code)
	}
}

// Redis being down must not take authentication with it. The narrow
// per-identifier limits are the ones that matter for abuse and they fail
// closed at their own call sites; a backstop that hard-fails on a cache
// blip is a worse outage than the one it prevents.
func TestGlobalThrottleFailsOpenWhenRedisIsGone(t *testing.T) {
	client, stop := startRedis(context.Background(), t)

	handler := GlobalThrottle(ratelimit.New(client), false, testSalt)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

	// Take Redis away.
	stop()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, fromIP("203.0.113.7"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; the backstop must fail open, not take the site down", rec.Code)
	}
}
