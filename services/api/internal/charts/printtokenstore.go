package charts

import (
	"context"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// RedisPrintTokens is the real PrintTokenStore.
//
// Two operations, both chosen for atomicity rather than convenience:
//
//	SET key value NX PX ttl   stores only if absent, in one round trip.
//	                          A GET-then-SET pair lets two mints collide
//	                          and one silently revoke the other.
//
//	GETDEL key                reads and deletes as one operation. This
//	                          is the entire single-use guarantee. With
//	                          GET followed by DEL, two concurrent
//	                          redemptions both read a value before
//	                          either deletes, and a token that must work
//	                          once works twice — which is exactly the
//	                          situation a stolen token is in.
//
// GETDEL needs Redis 6.2+. Compose pins redis:7.
type RedisPrintTokens struct {
	rdb goredis.UniversalClient
}

func NewRedisPrintTokens(rdb goredis.UniversalClient) *RedisPrintTokens {
	return &RedisPrintTokens{rdb: rdb}
}

func (r *RedisPrintTokens) SetNX(
	ctx context.Context,
	key, value string,
	ttl time.Duration,
) (bool, error) {
	stored, err := r.rdb.SetNX(ctx, key, value, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("charts: setnx %s: %w", key, err)
	}
	return stored, nil
}

func (r *RedisPrintTokens) GetDel(ctx context.Context, key string) (string, error) {
	value, err := r.rdb.GetDel(ctx, key).Result()
	if errors.Is(err, goredis.Nil) {
		// Absent, expired, or already spent — all the same answer, and
		// deliberately not distinguished. See ErrPrintTokenInvalid.
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("charts: getdel %s: %w", key, err)
	}
	return value, nil
}
