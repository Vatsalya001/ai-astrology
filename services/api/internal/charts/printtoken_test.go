package charts

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A Redis stand-in with the two operations the store needs, including
// GetDel's atomicity — which is the property the whole design rests on.
type fakeTokenStore struct {
	mu     sync.Mutex
	values map[string]string
	setErr error
	getErr error
}

func newFakeStore() *fakeTokenStore {
	return &fakeTokenStore{values: map[string]string{}}
}

func (f *fakeTokenStore) SetNX(_ context.Context, key, value string, _ time.Duration) (bool, error) {
	if f.setErr != nil {
		return false, f.setErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.values[key]; exists {
		return false, nil
	}
	f.values[key] = value
	return true, nil
}

func (f *fakeTokenStore) GetDel(_ context.Context, key string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	f.mu.Lock()
	value := f.values[key]
	f.mu.Unlock()
	f.mu.Lock()
	delete(f.values, key)
	f.mu.Unlock()
	return value, nil
}

func scope() PrintScope {
	return PrintScope{UserID: uuid.New(), ProfileID: uuid.New()}
}

func TestMintedTokenRedeemsToItsScope(t *testing.T) {
	t.Parallel()
	tokens := NewPrintTokens(newFakeStore())
	want := scope()

	token, err := tokens.Mint(context.Background(), want)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	got, err := tokens.Redeem(context.Background(), token)
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if got != want {
		t.Fatalf("scope round trip: got %+v, want %+v", got, want)
	}
}

// The property the whole design rests on. A token that works twice is a
// token worth stealing from a browser profile.
func TestATokenWorksExactlyOnce(t *testing.T) {
	t.Parallel()
	tokens := NewPrintTokens(newFakeStore())

	token, err := tokens.Mint(context.Background(), scope())
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	if _, err := tokens.Redeem(context.Background(), token); err != nil {
		t.Fatalf("first redeem: %v", err)
	}
	if _, err := tokens.Redeem(context.Background(), token); !errors.Is(err, ErrPrintTokenInvalid) {
		t.Fatalf("second redeem: got %v, want ErrPrintTokenInvalid", err)
	}
}

// Ten goroutines racing one token. Exactly one may win.
//
// This is why the store needs GetDel rather than Get-then-Delete: with
// a read followed by a delete, two concurrent redemptions both read a
// value before either deletes, and "single use" quietly becomes "single
// use, usually".
func TestConcurrentRedemptionsYieldExactlyOneWinner(t *testing.T) {
	t.Parallel()
	tokens := NewPrintTokens(newFakeStore())

	token, err := tokens.Mint(context.Background(), scope())
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	const racers = 10
	var wg sync.WaitGroup
	results := make(chan error, racers)

	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := tokens.Redeem(context.Background(), token)
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrPrintTokenInvalid) {
			t.Fatalf("unexpected error from a loser: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("got %d successful redemptions, want exactly 1", winners)
	}
}

func TestUnknownTokensAreRefused(t *testing.T) {
	t.Parallel()
	tokens := NewPrintTokens(newFakeStore())

	for _, token := range []string{"", "not-a-token", strings.Repeat("A", 22)} {
		if _, err := tokens.Redeem(context.Background(), token); !errors.Is(err, ErrPrintTokenInvalid) {
			t.Fatalf("redeem(%q): got %v, want ErrPrintTokenInvalid", token, err)
		}
	}
}

// One error for expired, spent and never-existed. Telling a caller
// which it was tells an attacker whether a token was ever real.
func TestARefusalRevealsNothingAboutWhy(t *testing.T) {
	t.Parallel()
	tokens := NewPrintTokens(newFakeStore())

	token, err := tokens.Mint(context.Background(), scope())
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if _, err := tokens.Redeem(context.Background(), token); err != nil {
		t.Fatalf("first redeem: %v", err)
	}

	spent := errMessage(t, tokens, token)
	never := errMessage(t, tokens, "AAAAAAAAAAAAAAAAAAAAAA")

	if spent != never {
		t.Fatalf("a spent token and an invented one give different errors:\n  spent: %s\n  never: %s", spent, never)
	}
}

func errMessage(t *testing.T, tokens *PrintTokens, token string) string {
	t.Helper()
	_, err := tokens.Redeem(context.Background(), token)
	if err == nil {
		t.Fatalf("redeem(%q) unexpectedly succeeded", token)
	}
	return err.Error()
}

func TestMintRefusesAnIncompleteScope(t *testing.T) {
	t.Parallel()
	tokens := NewPrintTokens(newFakeStore())

	for name, s := range map[string]PrintScope{
		"no user":    {ProfileID: uuid.New()},
		"no profile": {UserID: uuid.New()},
		"neither":    {},
	} {
		if _, err := tokens.Mint(context.Background(), s); err == nil {
			t.Fatalf("%s: mint succeeded, want an error", name)
		}
	}
}

// Every token distinct, and long enough that guessing is not a strategy.
func TestTokensAreUnpredictable(t *testing.T) {
	t.Parallel()
	tokens := NewPrintTokens(newFakeStore())

	seen := map[string]bool{}
	for range 500 {
		token, err := tokens.Mint(context.Background(), scope())
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		if seen[token] {
			t.Fatalf("duplicate token %q", token)
		}
		seen[token] = true

		// 16 random bytes, base64url without padding.
		if len(token) != 22 {
			t.Fatalf("token %q is %d chars, want 22 (128 bits)", token, len(token))
		}
	}
}

// A token carrying somebody else's profile must redeem to THAT profile,
// so the handler can compare rather than trusting the request.
func TestRedeemReportsTheScopeRatherThanTheRequest(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	tokens := NewPrintTokens(store)

	mine := scope()
	token, err := tokens.Mint(context.Background(), mine)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	got, err := tokens.Redeem(context.Background(), token)
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if got.ProfileID != mine.ProfileID || got.UserID != mine.UserID {
		t.Fatalf("scope is not the minted one: got %+v, want %+v", got, mine)
	}
}
