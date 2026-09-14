//go:build integration

package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Against a real Redis, not a mock.
//
// The properties under test — atomic check-and-increment, single use,
// TTL preservation across failed attempts — live in a Lua script
// executed by Redis. A mock would be asserting that the mock behaves as
// I imagined, which is the failure mode `.claude/rules/testing.md` calls
// out by name.

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
		t.Skipf("could not start Redis (is Docker running?): %v", err)
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

func TestOTPRoundTrip(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	store := NewOTPStore(client, 5*time.Minute, 5)

	code, err := store.Issue(ctx, "email", "a@example.com", 6)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("code %q is not 6 digits", code)
	}

	// The plaintext code must never be in Redis.
	raw, err := client.Get(ctx, redisKey("email", "a@example.com")).Result()
	if err != nil {
		t.Fatalf("read stored record: %v", err)
	}
	if contains(raw, code) {
		t.Fatalf("the plaintext code is stored in Redis: %s", raw)
	}

	if err := store.Verify(ctx, "email", "a@example.com", code); err != nil {
		t.Fatalf("the correct code was rejected: %v", err)
	}
}

// Single use. A code that still works after being spent is a code an
// attacker can replay from a shoulder-surfed screen or an intercepted
// SMS, long after the legitimate login finished.
func TestCorrectCodeCannotBeUsedTwice(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	store := NewOTPStore(client, 5*time.Minute, 5)

	code, err := store.Issue(ctx, "phone", "+919876543210", 6)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if err := store.Verify(ctx, "phone", "+919876543210", code); err != nil {
		t.Fatalf("first use failed: %v", err)
	}

	err = store.Verify(ctx, "phone", "+919876543210", code)
	if !errors.Is(err, ErrCodeNotFound) {
		t.Fatalf("replaying a consumed code returned %v, want ErrCodeNotFound", err)
	}
}

// Five wrong guesses burn the code. Without this a 6-digit secret is
// brute-forceable in a million requests, which is an afternoon.
func TestWrongCodeBurnsAfterMaxAttempts(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	const maxAttempts = 5
	store := NewOTPStore(client, 5*time.Minute, maxAttempts)

	code, err := store.Issue(ctx, "email", "burn@example.com", 6)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	wrong := wrongVariant(code)

	for i := 1; i < maxAttempts; i++ {
		err := store.Verify(ctx, "email", "burn@example.com", wrong)
		if !errors.Is(err, ErrCodeIncorrect) {
			t.Fatalf("attempt %d returned %v, want ErrCodeIncorrect", i, err)
		}
	}

	// The attempt that exhausts the budget.
	if err := store.Verify(ctx, "email", "burn@example.com", wrong); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("final attempt returned %v, want ErrTooManyAttempts", err)
	}

	// And now even the CORRECT code must fail — this is the property
	// that matters. Burning only wrong guesses would let an attacker
	// exhaust the budget and still use a code they later observed.
	if err := store.Verify(ctx, "email", "burn@example.com", code); errors.Is(err, nil) {
		t.Fatal("the correct code still worked after the budget was exhausted")
	}
}

// Failed attempts must not extend the window. If each wrong guess reset
// the TTL, an attacker could keep a code alive indefinitely by guessing
// slowly — turning a 5-minute secret into a permanent one.
func TestFailedAttemptsDoNotExtendTTL(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	store := NewOTPStore(client, 5*time.Minute, 5)

	code, err := store.Issue(ctx, "email", "ttl@example.com", 6)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	key := redisKey("email", "ttl@example.com")
	before, err := client.PTTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("read ttl: %v", err)
	}

	time.Sleep(120 * time.Millisecond)
	_ = store.Verify(ctx, "email", "ttl@example.com", wrongVariant(code))

	after, err := client.PTTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("read ttl after attempt: %v", err)
	}

	if after >= before {
		t.Errorf("TTL went from %v to %v across a failed attempt — the window was extended", before, after)
	}
}

func TestExpiredCodeIsIndistinguishableFromNeverIssued(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	// A very short TTL so expiry is observable without a slow test.
	store := NewOTPStore(client, 150*time.Millisecond, 5)

	code, err := store.Issue(ctx, "email", "expiring@example.com", 6)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	expired := store.Verify(ctx, "email", "expiring@example.com", code)
	never := store.Verify(ctx, "email", "never-issued@example.com", "123456")

	if !errors.Is(expired, ErrCodeNotFound) {
		t.Errorf("expired code returned %v, want ErrCodeNotFound", expired)
	}
	// Same error for both, so the response cannot reveal whether an
	// identifier ever had a code in flight.
	if !errors.Is(never, ErrCodeNotFound) {
		t.Errorf("never-issued identifier returned %v, want ErrCodeNotFound", never)
	}
}

// Reissuing must invalidate the previous code. Two live codes doubles
// the guessing surface, and a user who taps "resend" expects the newest
// one to be the one that works.
func TestReissueInvalidatesThePreviousCode(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	store := NewOTPStore(client, 5*time.Minute, 5)

	first, err := store.Issue(ctx, "email", "resend@example.com", 6)
	if err != nil {
		t.Fatalf("issue first: %v", err)
	}
	second, err := store.Issue(ctx, "email", "resend@example.com", 6)
	if err != nil {
		t.Fatalf("issue second: %v", err)
	}
	if first == second {
		t.Skip("the two codes collided by chance; rerun")
	}

	if err := store.Verify(ctx, "email", "resend@example.com", first); errors.Is(err, nil) {
		t.Error("the superseded code still verified")
	}
}

// The reason the check-and-increment is a Lua script.
//
// Run under -race with many goroutines presenting the SAME correct code:
// exactly one must succeed. In Go this would be read-then-write, and two
// concurrent verifies could both observe an unconsumed code and both
// succeed — defeating single use at precisely the moment an attacker
// would exploit it.
func TestConcurrentVerifyConsumesTheCodeExactlyOnce(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	store := NewOTPStore(client, 5*time.Minute, 5)

	code, err := store.Issue(ctx, "email", "race@example.com", 6)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	const goroutines = 24
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		successes int
	)

	start := make(chan struct{})
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release them together
			if err := store.Verify(ctx, "email", "race@example.com", code); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if successes != 1 {
		t.Fatalf("%d of %d concurrent verifications succeeded; exactly 1 must",
			successes, goroutines)
	}
}

// Concurrent WRONG guesses must not let the attempt budget be exceeded.
// A read-then-write increment loses updates under contention, which is
// how a 5-attempt limit becomes a 50-attempt one.
func TestConcurrentWrongGuessesStillBurnTheCode(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	const maxAttempts = 5
	store := NewOTPStore(client, 5*time.Minute, maxAttempts)

	code, err := store.Issue(ctx, "email", "flood@example.com", 6)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	wrong := wrongVariant(code)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = store.Verify(ctx, "email", "flood@example.com", wrong)
		}()
	}
	close(start)
	wg.Wait()

	// However the interleaving fell out, the code must be gone.
	if err := store.Verify(ctx, "email", "flood@example.com", code); errors.Is(err, nil) {
		t.Fatal("the correct code survived 40 concurrent wrong guesses")
	}
}

func TestDiscardRemovesAnInFlightCode(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	store := NewOTPStore(client, 5*time.Minute, 5)

	code, err := store.Issue(ctx, "phone", "+911111111111", 6)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := store.Discard(ctx, "phone", "+911111111111"); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if err := store.Verify(ctx, "phone", "+911111111111", code); !errors.Is(err, ErrCodeNotFound) {
		t.Errorf("after discard, verify returned %v, want ErrCodeNotFound", err)
	}

	// Discarding nothing is not an error — the flow may be abandoned
	// twice, or after expiry.
	if err := store.Discard(ctx, "phone", "+912222222222"); err != nil {
		t.Errorf("discarding a non-existent code errored: %v", err)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────

// wrongVariant returns a code guaranteed to differ from the one given,
// so a test cannot accidentally "fail" by guessing right.
func wrongVariant(code string) string {
	b := []byte(code)
	if b[0] == '0' {
		b[0] = '1'
	} else {
		b[0] = '0'
	}
	return string(b)
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}()
}
