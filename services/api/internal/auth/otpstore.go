package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// OTPStore holds in-flight codes in Redis.
//
// Redis only, never Postgres. Codes are short-lived secrets: persisting
// them buys nothing and creates a breach liability that outlives their
// usefulness by years.
type OTPStore struct {
	redis       *goredis.Client
	ttl         time.Duration
	maxAttempts int
}

func NewOTPStore(client *goredis.Client, ttl time.Duration, maxAttempts int) *OTPStore {
	return &OTPStore{redis: client, ttl: ttl, maxAttempts: maxAttempts}
}

// Issue stores the hash of a freshly generated code and returns the code
// for delivery.
//
// Overwrites any code already in flight for this identifier. That is
// deliberate: a user who taps "resend" expects the newest code to be the
// one that works, and leaving both valid doubles the guessing surface
// for no benefit.
func (s *OTPStore) Issue(ctx context.Context, channel, identifier string, length int) (string, error) {
	code, err := generateCode(length)
	if err != nil {
		return "", err
	}

	payload, err := json.Marshal(record{
		Hash:      hashCode(code),
		Attempts:  0,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return "", fmt.Errorf("auth: marshal otp record: %w", err)
	}

	if err := s.redis.Set(ctx, redisKey(channel, identifier), payload, s.ttl).Err(); err != nil {
		return "", fmt.Errorf("auth: store otp: %w", err)
	}

	return code, nil
}

// verifyScript performs the whole verification in one Redis round trip.
//
// Check, increment and delete must be atomic. Done in Go it is a
// read-then-write race: two concurrent verifications both read attempts
// = 4, both decide one attempt remains, and the limit becomes a
// suggestion. Worse, a correct code read concurrently could be consumed
// twice, which is precisely the single-use property this exists to
// guarantee.
//
// Returns:
//
//	{-1}            no active code (never issued, or expired)
//	{-2}            attempts already exhausted; code deleted
//	{0, attempts}   wrong code; attempts now used
//	{1}             correct; code consumed and deleted
var verifyScript = goredis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then
  return {-1}
end

local rec = cjson.decode(raw)

if rec.attempts >= tonumber(ARGV[2]) then
  redis.call('DEL', KEYS[1])
  return {-2}
end

if rec.hash == ARGV[1] then
  -- Single use: delete on success so a replay finds nothing.
  redis.call('DEL', KEYS[1])
  return {1}
end

rec.attempts = rec.attempts + 1

if rec.attempts >= tonumber(ARGV[2]) then
  -- Burn it. Leaving an exhausted code in place lets an attacker keep
  -- probing it until the TTL runs out.
  redis.call('DEL', KEYS[1])
  return {-2}
end

-- Preserve the remaining TTL: refreshing it on every wrong guess would
-- let an attacker extend the window indefinitely.
local ttl = redis.call('PTTL', KEYS[1])
redis.call('SET', KEYS[1], cjson.encode(rec), 'PX', ttl)
return {0, rec.attempts}
`)

// Verify consumes the code if it matches.
//
// The hash is computed here rather than in Lua: Redis has no sha256 in
// the scripting sandbox, and sending the plaintext code to compare
// server-side would put it in the Redis command log.
func (s *OTPStore) Verify(ctx context.Context, channel, identifier, presented string) error {
	res, err := verifyScript.Run(ctx,
		s.redis,
		[]string{redisKey(channel, identifier)},
		hashCode(presented),
		s.maxAttempts,
	).Result()
	if err != nil {
		return fmt.Errorf("auth: verify otp: %w", err)
	}

	values, ok := res.([]any)
	if !ok || len(values) == 0 {
		return fmt.Errorf("auth: unexpected verify result %T", res)
	}

	code, ok := values[0].(int64)
	if !ok {
		return fmt.Errorf("auth: unexpected verify status %T", values[0])
	}

	switch code {
	case 1:
		return nil
	case 0:
		return ErrCodeIncorrect
	case -1:
		return ErrCodeNotFound
	case -2:
		return ErrTooManyAttempts
	default:
		return fmt.Errorf("auth: unknown verify status %d", code)
	}
}

// Discard removes an in-flight code.
//
// Used when a flow is abandoned deliberately — changing the phone number
// on the OTP screen, for instance. Not an error if nothing is there.
func (s *OTPStore) Discard(ctx context.Context, channel, identifier string) error {
	if err := s.redis.Del(ctx, redisKey(channel, identifier)).Err(); err != nil &&
		!errors.Is(err, goredis.Nil) {
		return fmt.Errorf("auth: discard otp: %w", err)
	}
	return nil
}
