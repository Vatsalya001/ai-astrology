package shares

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

/*
The share token: a bearer credential that lives in a URL.

── What it has to survive ──

A share link is sent over WhatsApp, forwarded, screenshotted, and pasted
into group chats. It will end up somewhere public. Every property below
follows from accepting that rather than pretending otherwise:

	unguessable   128 bits from crypto/rand. Not a counter, not a slug,
	              not anything derived from the profile — an id a
	              recipient can increment is a way to read strangers'
	              charts.

	opaque        no structure to parse. It names no user and no profile,
	              so a leaked link reveals nothing until it is redeemed,
	              and cannot be edited into a link for a different chart.

	hashed        only SHA-256 of it reaches the database. A backup, a
	              replica or a support tool's `SELECT *` must not hand
	              over working links to every shared chart in the product.

	revocable     the owner can kill it, and correcting birth details
	              kills it automatically. This is the only real remedy
	              once a link has escaped.

── Why SHA-256 and not argon2 ──

This is the one place the reasoning differs from password storage, so it
is worth being explicit. A password is low-entropy and attackable with a
dictionary, which is what a slow KDF defends against. This token is 128
bits of uniform randomness: there is no dictionary and no entropy to
stretch. What there IS, is a lookup on every single page view — and a
login-speed hash on a read path buys nothing while costing a great deal.
*/

// TokenBytes is the raw entropy behind a token.
//
// Sixteen bytes — 128 bits — encoded as 22 base64url characters. Short
// enough to survive a WhatsApp line break, long enough that guessing is
// not a strategy anyone would attempt.
const TokenBytes = 16

// MintToken returns a new token and the hash to store for it.
//
// Both at once, deliberately: a caller that could obtain one without the
// other would be free to store the plaintext, which is the mistake this
// whole file exists to prevent.
func MintToken() (token string, hash string, err error) {
	raw := make([]byte, TokenBytes)
	if _, err := rand.Read(raw); err != nil {
		// Not recoverable and not worth degrading for. A token from a
		// weak source is worse than no share link.
		return "", "", fmt.Errorf("shares: generate token: %w", err)
	}

	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, HashToken(token), nil
}

// HashToken is the one-way mapping from a token to its stored form.
//
// Hex rather than base64 so the value is trivially greppable in a psql
// session and so the CHECK constraint on length is exact.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NormaliseToken trims what a URL or a paste may have added.
//
// People send these links by hand. A trailing space from a copy, or a
// full stop from the end of a sentence, is the difference between a
// working link and a 404 that reads as "the owner revoked this" — so the
// obvious damage is repaired rather than punished.
//
// Only whitespace and sentence punctuation. Nothing here rewrites the
// token itself: a token that is wrong in its body stays wrong.
func NormaliseToken(raw string) string {
	trimmed := strings.TrimSpace(raw)
	return strings.TrimRight(trimmed, ".,;:)]}>\"'")
}
