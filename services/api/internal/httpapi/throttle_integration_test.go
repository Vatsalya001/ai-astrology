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

// ─── the playground's own limit, against real Redis ──────────────────

// PHASE-04 §13 asks for a Go INTEGRATION test that "rate limiting
// fires". The tests above cover the global backstop. They did not cover
// `AIPlaygroundPerAdmin`, which was added late and whose unit tests use
// a fake limiter — so they prove the handler calls a limiter and reacts
// to its answer, and prove nothing about whether the rule's Max and
// Window survive a round trip through Redis.
//
// That distinction has bitten this phase repeatedly: a green unit test
// over a fake, sitting on top of a real dependency nobody exercised.
func TestTheAIPlaygroundRuleFiresAgainstRealRedis(t *testing.T) {
	client, stop := startRedis(context.Background(), t)
	defer stop()

	limiter := ratelimit.New(client)
	rule := ratelimit.AIPlaygroundPerAdmin
	const admin = "11111111-2222-3333-4444-555555555555"

	var allowed, denied int
	for range rule.Max + 5 {
		res, err := limiter.Allow(context.Background(), rule, admin)
		if err != nil {
			t.Fatalf("limiter: %v", err)
		}
		if res.Allowed {
			allowed++
		} else {
			denied++
			if res.RetryAfter <= 0 {
				t.Error("a refusal with no RetryAfter leaves the caller guessing")
			}
		}
	}

	if allowed != rule.Max {
		t.Errorf("%d completions allowed, want %d — the declared Max is not what Redis enforces",
			allowed, rule.Max)
	}
	if denied != 5 {
		t.Errorf("%d refused, want 5", denied)
	}
}

// Keyed on the operator, not the address.
//
// Two SUPER_ADMINs behind one office NAT must not share a budget — an
// IP-keyed limit would make the second one's playground stop working
// because the first had used it.
func TestTheAIPlaygroundLimitIsPerAdminNotShared(t *testing.T) {
	client, stop := startRedis(context.Background(), t)
	defer stop()

	limiter := ratelimit.New(client)
	rule := ratelimit.AIPlaygroundPerAdmin

	// Exhaust the first admin entirely.
	for range rule.Max {
		if _, err := limiter.Allow(context.Background(), rule, "admin-one"); err != nil {
			t.Fatalf("limiter: %v", err)
		}
	}
	spent, err := limiter.Allow(context.Background(), rule, "admin-one")
	if err != nil {
		t.Fatalf("limiter: %v", err)
	}
	if spent.Allowed {
		t.Fatal("the first admin was not exhausted, so the rest of this test proves nothing")
	}

	fresh, err := limiter.Allow(context.Background(), rule, "admin-two")
	if err != nil {
		t.Fatalf("limiter: %v", err)
	}
	if !fresh.Allowed {
		t.Error("a second admin was refused because the first had spent the budget")
	}
}

// The window is what makes the Max mean anything.
//
// A rule with the right Max and a window of zero would pass the test
// above on the first burst and never limit anything afterwards.
func TestTheAIPlaygroundWindowIsNotDegenerate(t *testing.T) {
	if ratelimit.AIPlaygroundPerAdmin.Window <= 0 {
		t.Fatalf("window is %v; a non-positive window limits nothing",
			ratelimit.AIPlaygroundPerAdmin.Window)
	}
	// Tight enough to matter on a route that spends money, wide enough
	// not to obstruct a human comparing prompt versions by hand.
	if ratelimit.AIPlaygroundPerAdmin.Max > 60 {
		t.Errorf("Max is %d per %v — too generous for a route that bills per call",
			ratelimit.AIPlaygroundPerAdmin.Max, ratelimit.AIPlaygroundPerAdmin.Window)
	}
}
