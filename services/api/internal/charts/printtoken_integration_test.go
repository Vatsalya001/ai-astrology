//go:build integration

package charts_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/charts"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/testsupport"
)

// The single-use guarantee, against real Redis.
//
// The unit tests use a fake store and prove the CONTRACT: given an
// atomic GetDel, exactly one redemption wins. They cannot prove the
// production path is atomic, because the fake stands in for Redis —
// and breaking the fake's atomicity did not fail them, which is how
// that limit was found rather than assumed.
//
// This is the test that covers it. It runs GETDEL on a real server,
// where the atomicity is Redis's rather than the fake's.

func startRedis(ctx context.Context, t *testing.T) (*goredis.Client, func()) {
	t.Helper()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForLog("Ready to accept connections").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		testsupport.ContainerUnavailable(t, "redis", err)
	}

	endpoint, err := container.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("redis endpoint: %v", err)
	}

	rdb := goredis.NewClient(&goredis.Options{Addr: endpoint})
	return rdb, func() {
		_ = rdb.Close()
		_ = container.Terminate(ctx)
	}
}

func TestRealRedisGivesExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	rdb, stop := startRedis(ctx, t)
	defer stop()

	tokens := charts.NewPrintTokens(charts.NewRedisPrintTokens(rdb))
	scope := charts.PrintScope{UserID: uuid.New(), ProfileID: uuid.New()}

	// Repeated, because a race that fires one time in twenty is still a
	// token that works twice.
	for round := range 20 {
		token, err := tokens.Mint(ctx, scope)
		if err != nil {
			t.Fatalf("round %d: mint: %v", round, err)
		}

		const racers = 16
		var wg sync.WaitGroup
		results := make(chan error, racers)

		for range racers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := tokens.Redeem(ctx, token)
				results <- err
			}()
		}
		wg.Wait()
		close(results)

		winners := 0
		for err := range results {
			switch {
			case err == nil:
				winners++
			case errors.Is(err, charts.ErrPrintTokenInvalid):
			default:
				t.Fatalf("round %d: unexpected error: %v", round, err)
			}
		}
		if winners != 1 {
			t.Fatalf("round %d: %d redemptions succeeded, want exactly 1", round, winners)
		}
	}
}

// The TTL is real, not decorative: a token that outlives its window is
// a credential sitting in a browser profile for as long as Redis holds
// it.
func TestATokenExpires(t *testing.T) {
	ctx := context.Background()
	rdb, stop := startRedis(ctx, t)
	defer stop()

	store := charts.NewRedisPrintTokens(rdb)
	tokens := charts.NewPrintTokens(store)

	token, err := tokens.Mint(ctx, charts.PrintScope{UserID: uuid.New(), ProfileID: uuid.New()})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	// Asserted on the server rather than by sleeping for five minutes.
	ttl, err := rdb.TTL(ctx, "print:"+token).Result()
	if err != nil {
		t.Fatalf("ttl: %v", err)
	}
	if ttl <= 0 || ttl > charts.PrintTokenTTL {
		t.Fatalf("ttl is %s, want (0, %s]", ttl, charts.PrintTokenTTL)
	}

	/*
	  PExpire, not Expire.

	  `EXPIRE` takes whole seconds, so go-redis truncated a 1ms argument
	  up to 1s and warned about it — and the 50ms sleep below then read
	  a token that was still perfectly alive. The test failed for the
	  right reason with the wrong cause.
	*/
	if err := rdb.PExpire(ctx, "print:"+token, 20*time.Millisecond).Err(); err != nil {
		t.Fatalf("pexpire: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	if _, err := tokens.Redeem(ctx, token); !errors.Is(err, charts.ErrPrintTokenInvalid) {
		t.Fatalf("redeem after expiry: got %v, want ErrPrintTokenInvalid", err)
	}
}
