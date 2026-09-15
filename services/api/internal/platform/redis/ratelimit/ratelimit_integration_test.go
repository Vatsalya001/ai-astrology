//go:build integration

package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/testsupport"
)

// Against real Redis. The window arithmetic lives in a Lua script that
// Redis executes; a mock would test my mental model of ZREMRANGEBYSCORE
// rather than Redis's.

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

	client := goredis.NewClient(&goredis.Options{
		Addr: fmt.Sprintf("%s:%s", host, port.Port()),
	})
	return client, func() {
		_ = client.Close()
		_ = container.Terminate(context.Background())
	}
}

func TestAllowsUpToTheLimitThenDenies(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	limiter := New(client)
	rule := Rule{Name: "test", Max: 3, Window: time.Minute}

	for i := 1; i <= 3; i++ {
		res, err := limiter.Allow(ctx, rule, "subject-a")
		if err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
		if !res.Allowed {
			t.Fatalf("attempt %d was denied; the limit is 3", i)
		}
		if want := 3 - i; res.Remaining != want {
			t.Errorf("attempt %d: Remaining = %d, want %d", i, res.Remaining, want)
		}
	}

	res, err := limiter.Allow(ctx, rule, "subject-a")
	if err != nil {
		t.Fatalf("fourth attempt: %v", err)
	}
	if res.Allowed {
		t.Fatal("the fourth attempt was allowed; the limit is 3")
	}
	if res.RetryAfter <= 0 {
		t.Error("a denial carried no RetryAfter; the UI has nothing to count down")
	}
	if res.RetryAfter > time.Minute {
		t.Errorf("RetryAfter = %v, longer than the window itself", res.RetryAfter)
	}
	if res.Rule != "test" {
		t.Errorf("Rule = %q, want the rule that was hit", res.Rule)
	}
}

// Limits must not bleed between subjects. Without this, one abusive
// caller would lock out everyone else — a denial of service delivered by
// the thing meant to prevent one.
func TestSubjectsAreIsolated(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	limiter := New(client)
	rule := Rule{Name: "test", Max: 2, Window: time.Minute}

	for range 2 {
		if _, err := limiter.Allow(ctx, rule, "noisy"); err != nil {
			t.Fatalf("allow: %v", err)
		}
	}
	if res, _ := limiter.Allow(ctx, rule, "noisy"); res.Allowed {
		t.Fatal("the noisy subject was not limited")
	}

	res, err := limiter.Allow(ctx, rule, "quiet")
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	if !res.Allowed {
		t.Error("an unrelated subject was denied because another was throttled")
	}
}

func TestRulesAreIsolated(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	limiter := New(client)
	tight := Rule{Name: "tight", Max: 1, Window: time.Minute}
	loose := Rule{Name: "loose", Max: 5, Window: time.Minute}

	if _, err := limiter.Allow(ctx, tight, "s"); err != nil {
		t.Fatalf("allow: %v", err)
	}
	if res, _ := limiter.Allow(ctx, tight, "s"); res.Allowed {
		t.Fatal("the tight rule did not deny")
	}
	if res, _ := limiter.Allow(ctx, loose, "s"); !res.Allowed {
		t.Error("exhausting one rule also exhausted an unrelated one")
	}
}

// The reason this is a sorted set rather than INCR with EXPIRE.
//
// A fixed window lets twice the limit through at a boundary: with "2 per
// second", two requests at 0.9s and two at 1.1s are four in 200ms, each
// window individually compliant. A sliding window sees all four.
func TestWindowSlidesRatherThanResetting(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	limiter := New(client)
	rule := Rule{Name: "sliding", Max: 2, Window: time.Second}

	// Fill the window.
	for i := range 2 {
		if res, err := limiter.Allow(ctx, rule, "s"); err != nil || !res.Allowed {
			t.Fatalf("initial attempt %d: allowed=%v err=%v", i, res.Allowed, err)
		}
	}

	// Just before the window would have rolled over. A fixed window
	// would already have reset; a sliding one has not.
	time.Sleep(600 * time.Millisecond)
	if res, _ := limiter.Allow(ctx, rule, "s"); res.Allowed {
		t.Error("a request was allowed 0.6s into a 1s window that was already full — " +
			"this is a fixed window, not a sliding one")
	}

	// Once the earliest events genuinely age out, capacity returns.
	time.Sleep(600 * time.Millisecond)
	if res, err := limiter.Allow(ctx, rule, "s"); err != nil || !res.Allowed {
		t.Errorf("the window never recovered: allowed=%v err=%v", res.Allowed, err)
	}
}

// Denied attempts must not be recorded. Counting them would let an
// attacker hold the window open indefinitely by continuing to hammer it,
// so a legitimate user could never recover.
func TestDeniedAttemptsDoNotExtendTheWindow(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	limiter := New(client)
	rule := Rule{Name: "nonext", Max: 1, Window: 800 * time.Millisecond}

	if res, _ := limiter.Allow(ctx, rule, "s"); !res.Allowed {
		t.Fatal("the first attempt was denied")
	}

	// Hammer it while denied.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if res, _ := limiter.Allow(ctx, rule, "s"); res.Allowed {
			t.Fatal("an attempt was allowed while the window was full")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The original event should age out on schedule regardless.
	time.Sleep(400 * time.Millisecond)
	if res, err := limiter.Allow(ctx, rule, "s"); err != nil || !res.Allowed {
		t.Errorf("the window never recovered after sustained denied attempts: "+
			"allowed=%v err=%v", res.Allowed, err)
	}
}

// The reason the script is Lua.
//
// Under concurrency a read-then-write limiter lets more through than the
// limit — and a rate limiter is precisely where concurrency shows up.
func TestConcurrentRequestsNeverExceedTheLimit(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	limiter := New(client)
	const max = 5
	rule := Rule{Name: "concurrent", Max: max, Window: time.Minute}

	const goroutines = 60
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		allowed int
		release = make(chan struct{})
	)

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-release
			res, err := limiter.Allow(ctx, rule, "s")
			if err == nil && res.Allowed {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	close(release)
	wg.Wait()

	if allowed != max {
		t.Fatalf("%d of %d concurrent requests were allowed; exactly %d must be — "+
			"the check-and-insert is not atomic", allowed, goroutines, max)
	}
}

// Two events in the same nanosecond must both count. Sharing a
// sorted-set member would silently give one away, under exactly the
// concurrent load that matters.
func TestEventsInTheSameInstantAreCountedSeparately(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	limiter := New(client)
	rule := Rule{Name: "sameinstant", Max: 100, Window: time.Minute}

	const n = 50
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = limiter.Allow(ctx, rule, "s")
		}()
	}
	wg.Wait()

	count, err := client.ZCard(ctx, key(rule.Name, "s")).Result()
	if err != nil {
		t.Fatalf("ZCard: %v", err)
	}
	if count != n {
		t.Errorf("%d of %d events were recorded — members collided, so some "+
			"requests were free", count, n)
	}
}

func TestAllowAllReturnsTheFirstDenial(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	limiter := New(client)
	tight := Rule{Name: "first", Max: 1, Window: time.Minute}
	loose := Rule{Name: "second", Max: 100, Window: time.Minute}

	if res, err := limiter.AllowAll(ctx, "s", tight, loose); err != nil || !res.Allowed {
		t.Fatalf("first call: allowed=%v err=%v", res.Allowed, err)
	}

	res, err := limiter.AllowAll(ctx, "s", tight, loose)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if res.Allowed {
		t.Fatal("AllowAll allowed a request that the narrowest rule denies")
	}
	// The reported rule must be the one actually hit, so Retry-After
	// reflects the right window.
	if res.Rule != "first" {
		t.Errorf("Rule = %q, want the narrowest rule that denied", res.Rule)
	}
}

// After a successful verification the window is cleared, so someone who
// fumbled a code twice is not still throttled once they get it right.
func TestResetClearsTheWindow(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	limiter := New(client)
	rule := Rule{Name: "reset", Max: 1, Window: time.Minute}

	if _, err := limiter.Allow(ctx, rule, "s"); err != nil {
		t.Fatalf("allow: %v", err)
	}
	if res, _ := limiter.Allow(ctx, rule, "s"); res.Allowed {
		t.Fatal("the second attempt was not denied")
	}

	if err := limiter.Reset(ctx, rule, "s"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if res, err := limiter.Allow(ctx, rule, "s"); err != nil || !res.Allowed {
		t.Errorf("after Reset: allowed=%v err=%v", res.Allowed, err)
	}
}

// An idle subject must not occupy memory forever. Without the PEXPIRE,
// every identifier ever seen would persist for the life of the Redis
// instance.
func TestWindowKeysExpire(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	limiter := New(client)
	rule := Rule{Name: "ttl", Max: 5, Window: 2 * time.Second}

	if _, err := limiter.Allow(ctx, rule, "s"); err != nil {
		t.Fatalf("allow: %v", err)
	}

	ttl, err := client.PTTL(ctx, key(rule.Name, "s")).Result()
	if err != nil {
		t.Fatalf("PTTL: %v", err)
	}
	if ttl <= 0 {
		t.Fatal("the window key has no TTL; idle subjects would accumulate forever")
	}
	if ttl > rule.Window+2*time.Second {
		t.Errorf("TTL %v is much longer than the %v window", ttl, rule.Window)
	}
}
