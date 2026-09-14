// Package ratelimit implements a sliding-window limiter on Redis.
//
// Non-optional on auth endpoints. An unlimited OTP request endpoint is
// an SMS bill and a spam cannon pointed at whoever's number is typed in;
// an unlimited verify endpoint turns a 6-digit code into a formality.
package ratelimit

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Rule is one limit: `Max` events per `Window`.
type Rule struct {
	// Name appears in logs and metrics. Never the identifier it applies
	// to — that would put a phone number in a metric label.
	Name   string
	Max    int
	Window time.Duration
}

// Result describes an allow/deny decision.
type Result struct {
	Allowed bool
	// Remaining is how many events are left in the current window.
	Remaining int
	// RetryAfter is how long until the oldest event leaves the window.
	// Zero when allowed.
	RetryAfter time.Duration
	// Rule names which limit was hit, for the log line.
	Rule string
}

// Limiter applies rules against Redis.
type Limiter struct {
	redis *goredis.Client
}

func New(client *goredis.Client) *Limiter {
	return &Limiter{redis: client}
}

// slidingWindowScript is a sorted-set sliding window.
//
// Why a sorted set rather than INCR with EXPIRE: a fixed window lets
// twice the limit through at a boundary. With "3 per 15 minutes", three
// requests at 14:59 and three more at 15:01 are six requests in two
// minutes, each window individually compliant. For OTP that is six SMS
// to someone who asked for none.
//
// Why Lua: the check and the insert must be one operation. Read-then-
// write in Go is a race, and a rate limiter is precisely where load — and
// therefore contention — shows up. The same reasoning as the OTP verify
// script, and the same demonstrated consequence.
//
// KEYS[1] window key
// ARGV[1] now, milliseconds
// ARGV[2] window size, milliseconds
// ARGV[3] max events
// ARGV[4] unique member for this event
//
// Returns {allowed, remaining, retry_after_ms}
var slidingWindowScript = goredis.NewScript(`
local key      = KEYS[1]
local now      = tonumber(ARGV[1])
local window   = tonumber(ARGV[2])
local max      = tonumber(ARGV[3])
local member   = ARGV[4]
local cutoff   = now - window

-- Drop everything that has aged out of the window.
redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)

local count = redis.call('ZCARD', key)

if count >= max then
  -- Denied. Do NOT record the attempt: counting rejected requests would
  -- let an attacker hold the window open indefinitely by continuing to
  -- hammer it, so the caller could never recover.
  local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
  local retry = window
  if oldest[2] then
    retry = (tonumber(oldest[2]) + window) - now
    if retry < 0 then retry = 0 end
  end
  return {0, 0, retry}
end

redis.call('ZADD', key, now, member)

-- Expire the key slightly after the window so an idle identifier does
-- not occupy memory forever. +1s of slack absorbs clock skew between
-- Redis and the caller.
redis.call('PEXPIRE', key, window + 1000)

return {1, max - count - 1, 0}
`)

// Allow records an event against a rule and reports whether it is
// permitted.
//
// `subject` is the thing being limited — a hashed IP, or an identifier.
// It is used only as part of a Redis key and never logged.
func (l *Limiter) Allow(ctx context.Context, rule Rule, subject string) (Result, error) {
	now := time.Now()

	// The member must be unique per event; two requests in the same
	// millisecond would otherwise collapse into one sorted-set entry and
	// the second would be free.
	member := fmt.Sprintf("%d-%s", now.UnixNano(), randomSuffix())

	res, err := slidingWindowScript.Run(ctx,
		l.redis,
		[]string{key(rule.Name, subject)},
		now.UnixMilli(),
		rule.Window.Milliseconds(),
		rule.Max,
		member,
	).Result()
	if err != nil {
		return Result{}, fmt.Errorf("ratelimit: run script: %w", err)
	}

	values, ok := res.([]any)
	if !ok || len(values) != 3 {
		return Result{}, fmt.Errorf("ratelimit: unexpected script result %T", res)
	}

	allowed, _ := values[0].(int64)
	remaining, _ := values[1].(int64)
	retryMS, _ := values[2].(int64)

	return Result{
		Allowed:    allowed == 1,
		Remaining:  int(remaining),
		RetryAfter: time.Duration(retryMS) * time.Millisecond,
		Rule:       rule.Name,
	}, nil
}

// AllowAll applies several rules, returning the first denial.
//
// Order matters to the caller: pass the narrowest rule first so the
// Retry-After reflects the limit actually hit rather than a broader one
// that happens to also be exhausted.
func (l *Limiter) AllowAll(ctx context.Context, subject string, rules ...Rule) (Result, error) {
	for _, rule := range rules {
		res, err := l.Allow(ctx, rule, subject)
		if err != nil {
			return Result{}, err
		}
		if !res.Allowed {
			return res, nil
		}
	}
	return Result{Allowed: true}, nil
}

// Reset clears a window. Used after a successful verification, so a user
// who fumbled a code is not still throttled once they get it right.
func (l *Limiter) Reset(ctx context.Context, rule Rule, subject string) error {
	if err := l.redis.Del(ctx, key(rule.Name, subject)).Err(); err != nil {
		return fmt.Errorf("ratelimit: reset: %w", err)
	}
	return nil
}

func key(ruleName, subject string) string {
	return fmt.Sprintf("rl:%s:%s", ruleName, subject)
}
