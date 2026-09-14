// Package auth owns authentication: OTP, tokens, and the middleware
// that guards every other route.
//
// The consumer declares the interfaces it needs (see UserStore), so this
// package never imports another domain's internals and the dependency
// graph stays acyclic.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

var (
	// ErrCodeNotFound covers both "never issued" and "expired". They are
	// deliberately indistinguishable to the caller: telling an attacker
	// which one it was reveals whether an identifier is in use.
	ErrCodeNotFound = errors.New("auth: no active code")

	// ErrCodeIncorrect is returned until attempts are exhausted.
	ErrCodeIncorrect = errors.New("auth: incorrect code")

	// ErrTooManyAttempts means the code was burned. A new one must be
	// requested — retrying the same code forever is how a 6-digit secret
	// becomes a 6-digit formality.
	ErrTooManyAttempts = errors.New("auth: too many attempts")
)

// Channel is how a code reaches a person.
//
// An interface so email and phone exercise the identical code path:
// everything from generation to verification is channel-agnostic, and
// only delivery differs. Email is built first because it is free end to
// end, in development and in early production.
type Channel interface {
	// ID names the channel for logs and config. Never the recipient.
	ID() string
	Send(ctx context.Context, identifier, code, locale string) error
}

// generateCode returns a cryptographically random decimal code.
//
// crypto/rand, not math/rand. A predictable OTP is not a second factor,
// and math/rand seeded from the clock is predictable to anyone who knows
// roughly when the code was issued.
//
// Digits are drawn individually with rejection-free uniform sampling via
// rand.Int rather than by taking a big number modulo 10^n — modulo
// introduces a small bias toward low digits, which is exactly the sort
// of thing that is invisible in testing and quoted in a write-up later.
func generateCode(length int) (string, error) {
	if length < 4 || length > 10 {
		return "", fmt.Errorf("auth: refusing to generate a %d-digit code", length)
	}

	var sb strings.Builder
	sb.Grow(length)

	for range length {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			// Exhausted entropy is not recoverable and must never fall
			// back to a weaker source.
			return "", fmt.Errorf("auth: read random digit: %w", err)
		}
		sb.WriteByte(byte('0' + n.Int64()))
	}

	return sb.String(), nil
}

// hashCode returns the hex sha256 of a code.
//
// Codes are stored hashed for the same reason passwords are: a Redis
// dump, a debug log of a key, or an errant MONITOR should not hand over
// live credentials. They are short-lived, which reduces the window but
// does not make plaintext acceptable.
//
// No salt and no KDF, deliberately. The code lives for five minutes and
// is rate-limited to five attempts; the threat a KDF defends against —
// offline brute force of a long-lived secret — does not apply, and
// bcrypt on every verify would add latency to the hottest path in the
// funnel.
func hashCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// codesMatch compares in constant time.
//
// A byte-by-byte compare returns early on the first mismatch, so the
// time it takes leaks how many leading characters were right. Over
// enough samples that recovers the code one digit at a time. The
// comparison is on hashes, which already blunts this, but the habit is
// cheap and the exception is not worth arguing about at review time.
func codesMatch(storedHash, presented string) bool {
	return subtle.ConstantTimeCompare([]byte(storedHash), []byte(hashCode(presented))) == 1
}

// redisKey is the storage key for an in-flight code.
//
// Namespaced by channel so the same string used as both an email and a
// phone identifier cannot collide.
func redisKey(channel, identifier string) string {
	return fmt.Sprintf("otp:%s:%s", channel, identifier)
}

// record is what Redis holds against that key. The code itself is never
// stored — only its hash.
type record struct {
	Hash      string    `json:"hash"`
	Attempts  int       `json:"attempts"`
	CreatedAt time.Time `json:"created_at"`
}
