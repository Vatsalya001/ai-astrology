package charts

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// A single-use credential that lets headless Chrome render one private
// chart, once.
//
// ── Why this exists at all ──
//
// The PDF is produced by driving a real browser at a print-styled web
// route, so the PDF and the site can never drift apart. But that browser
// has no session: it is a process on a worker, not a signed-in person.
// Giving it the user's access token would mean minting a full-privilege
// credential and handing it to a subprocess, where it would sit in a
// command line and a browser profile on disk.
//
// So it gets the narrowest possible thing instead: one token, for one
// chart, for one user, that works once and expires in minutes.
//
// ── The properties that matter, and why ──
//
//	single use     redeeming deletes it. A token that leaks from a
//	               browser profile or a log is already spent.
//	short lived    minutes, not hours. It only has to outlive one page
//	               load.
//	scoped         it names ONE chart and ONE user. Redeeming it tells
//	               the caller which, so the handler never takes the
//	               chart id from the query string — that is how a valid
//	               token for your own chart becomes a reader for
//	               somebody else's.
//	opaque         128 bits from crypto/rand, no structure. Nothing to
//	               parse, forge or enumerate.
//
// Stored in Redis rather than Postgres: the lifetime is minutes, the
// write is on the PDF path rather than the request path, and the
// single-writer rule is about durable state. A token that vanishes if
// Redis restarts costs one retried PDF.

// PrintTokenTTL is how long a token is worth anything.
//
// Five minutes is generous for a page load and short enough that a
// leaked token is almost always already dead. chromedp's own navigation
// timeout is well inside it.
const PrintTokenTTL = 5 * time.Minute

// ErrPrintTokenInvalid covers expired, already-redeemed, and never-
// existed. Deliberately one error: telling a caller which of the three
// it was tells an attacker whether a token was ever real.
var ErrPrintTokenInvalid = errors.New("charts: print token is not valid")

// PrintScope is what a redeemed token authorises.
type PrintScope struct {
	UserID    uuid.UUID
	ProfileID uuid.UUID
}

// PrintTokenStore is the Redis surface this needs, declared here by the
// consumer so the charts package does not import a Redis wrapper's whole
// API — and so a test can supply a map.
type PrintTokenStore interface {
	// SetNX stores value at key only if key is absent, with a TTL.
	// Returns false when the key already existed.
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	// GetDel atomically reads and deletes. Atomic is the whole point:
	// a read-then-delete pair lets two concurrent redemptions both
	// succeed, and "single use" becomes "single use, usually".
	GetDel(ctx context.Context, key string) (string, error)
}

// PrintTokens mints and redeems.
type PrintTokens struct {
	store PrintTokenStore
	now   func() time.Time
}

func NewPrintTokens(store PrintTokenStore) *PrintTokens {
	return &PrintTokens{store: store, now: time.Now}
}

// WithClock replaces the clock, so a test can expire a token without
// sleeping.
func (p *PrintTokens) WithClock(now func() time.Time) *PrintTokens {
	if now != nil {
		p.now = now
	}
	return p
}

// Mint returns an opaque token authorising one render of one chart.
func (p *PrintTokens) Mint(ctx context.Context, scope PrintScope) (string, error) {
	if scope.UserID == uuid.Nil || scope.ProfileID == uuid.Nil {
		return "", fmt.Errorf("charts: print token needs both a user and a profile")
	}

	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		// Not recoverable and not worth degrading for: a token from a
		// weak source is worse than no PDF.
		return "", fmt.Errorf("charts: generate print token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)

	// SetNX, not Set. A collision is astronomically unlikely at 128
	// bits, and if one ever happened, overwriting would silently revoke
	// somebody else's token.
	stored, err := p.store.SetNX(ctx, printTokenKey(token), scope.UserID.String()+":"+scope.ProfileID.String(), PrintTokenTTL)
	if err != nil {
		return "", fmt.Errorf("charts: store print token: %w", err)
	}
	if !stored {
		return "", fmt.Errorf("charts: print token collided")
	}

	return token, nil
}

// Redeem consumes a token and reports what it authorised.
//
// Returns the scope so the caller uses THAT rather than anything from
// the request. A handler that reads the chart id from the query string
// and only checks that the token is valid has built an authorisation
// bypass: any valid token would read any chart.
func (p *PrintTokens) Redeem(ctx context.Context, token string) (PrintScope, error) {
	if token == "" {
		return PrintScope{}, ErrPrintTokenInvalid
	}

	value, err := p.store.GetDel(ctx, printTokenKey(token))
	if err != nil {
		return PrintScope{}, fmt.Errorf("charts: redeem print token: %w", err)
	}
	if value == "" {
		return PrintScope{}, ErrPrintTokenInvalid
	}

	var userID, profileID string
	if n, _ := fmt.Sscanf(value, "%36s:%36s", &userID, &profileID); n != 2 {
		return PrintScope{}, ErrPrintTokenInvalid
	}

	user, err := uuid.Parse(userID)
	if err != nil {
		return PrintScope{}, ErrPrintTokenInvalid
	}
	profile, err := uuid.Parse(profileID)
	if err != nil {
		return PrintScope{}, ErrPrintTokenInvalid
	}

	return PrintScope{UserID: user, ProfileID: profile}, nil
}

// Namespaced, so a token can never be confused with a rate-limit key or
// an OTP.
func printTokenKey(token string) string { return "print:" + token }
