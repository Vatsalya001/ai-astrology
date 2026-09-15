//go:build integration

package users

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis/ratelimit"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/testsupport"
)

// POST /users/me/challenge sends a real email or SMS on every call.
//
// Unlimited, it is a mailbomb aimed at whoever owns the account,
// triggerable by anyone holding an access token — and the destination is
// the account's own verified contact, so the victim is a real person who
// did nothing. This was genuinely unlimited until it was measured: 25 of
// 25 consecutive calls returned 200.
//
// Redis is real because the limit is a Lua sliding window; a fake would
// assert that the handler calls something, not that the window holds.

func redisClient(ctx context.Context, t *testing.T) (*goredis.Client, func()) {
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

	client := goredis.NewClient(&goredis.Options{Addr: host + ":" + port.Port()})
	return client, func() {
		_ = client.Close()
		_ = container.Terminate(ctx)
	}
}

// countingChallenger records how many codes were actually sent, which is
// the number that matters: a 429 that still sends is not a limit.
type countingChallenger struct{ sent int }

func (c *countingChallenger) Challenge(context.Context, uuid.UUID, string) error {
	c.sent++
	return nil
}

func challengeHandler(t *testing.T) (*Handler, *countingChallenger, Throttle, func()) {
	t.Helper()

	client, stop := redisClient(context.Background(), t)

	h := NewHandler(nil, nil, func(w http.ResponseWriter, _ *http.Request, status int, code, message string, _ error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"code": code, "message": message},
		})
	})

	return h, &countingChallenger{}, ratelimit.New(client), stop
}

// The tests run through the REAL Authenticate middleware with a REAL
// token rather than injecting a Principal into the context directly.
//
// Exporting a context setter for the convenience of a test would mean
// any handler could forge a principal, which is exactly the thing the
// middleware exists to be the only source of. Minting a token costs two
// lines and tests the path that actually runs.
var testIssuer = func() *auth.Issuer {
	issuer, err := auth.NewIssuer(
		"test-signing-secret-at-least-32-bytes-long", 15*time.Minute, 30*24*time.Hour)
	if err != nil {
		panic(err)
	}
	return issuer
}()

func authenticated(t *testing.T, h http.HandlerFunc) http.Handler {
	t.Helper()
	return auth.Authenticate(testIssuer, func(w http.ResponseWriter, _ *http.Request, status int, code, message string) {
		w.WriteHeader(status)
	})(h)
}

func signedIn(t *testing.T, userID uuid.UUID) *http.Request {
	t.Helper()
	token, err := testIssuer.IssueAccessToken(userID, uuid.New(), auth.RoleUser)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/users/me/challenge", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func TestChallengeIsRateLimitedPerUser(t *testing.T) {
	h, challenger, throttle, stop := challengeHandler(t)
	defer stop()

	userID := uuid.New()
	handler := authenticated(t, h.Challenge(challenger, throttle))

	var allowed, denied int
	for range ratelimit.ChallengePerUser.Max + 10 {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, signedIn(t, userID))

		switch rec.Code {
		case http.StatusOK:
			allowed++
		case http.StatusTooManyRequests:
			denied++
			if rec.Header().Get("Retry-After") == "" {
				t.Error("a 429 with no Retry-After leaves the client guessing")
			}
		default:
			t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
		}
	}

	if allowed != ratelimit.ChallengePerUser.Max {
		t.Errorf("%d calls were allowed, want %d", allowed, ratelimit.ChallengePerUser.Max)
	}
	if denied != 10 {
		t.Errorf("%d calls were denied, want 10", denied)
	}

	// The number that actually reaches an inbox. A limiter that returns
	// 429 after sending has prevented nothing.
	if challenger.sent != ratelimit.ChallengePerUser.Max {
		t.Errorf("%d codes were sent, want %d — the limiter is checked too late",
			challenger.sent, ratelimit.ChallengePerUser.Max)
	}
}

// Per user, not per IP: one exhausted account must not lock out everyone
// else, and an attacker changing IP must not get a fresh allowance
// against the same victim.
func TestChallengeLimitIsScopedToTheUser(t *testing.T) {
	h, challenger, throttle, stop := challengeHandler(t)
	defer stop()

	handler := authenticated(t, h.Challenge(challenger, throttle))
	exhausted := uuid.New()

	for range ratelimit.ChallengePerUser.Max + 1 {
		handler.ServeHTTP(httptest.NewRecorder(), signedIn(t, exhausted))
	}

	// A different account, same everything else.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, signedIn(t, uuid.New()))

	if rec.Code != http.StatusOK {
		t.Fatalf("a second user got %d; one account's limit must not affect another", rec.Code)
	}
}

// Unauthenticated callers are rejected before the limiter runs, so an
// anonymous flood cannot consume anybody's allowance.
func TestChallengeRejectsAnonymousBeforeSpendingTheLimit(t *testing.T) {
	h, challenger, throttle, stop := challengeHandler(t)
	defer stop()

	rec := httptest.NewRecorder()
	authenticated(t, h.Challenge(challenger, throttle)).ServeHTTP(rec,
		httptest.NewRequest(http.MethodPost, "/users/me/challenge", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if challenger.sent != 0 {
		t.Errorf("%d codes were sent to an anonymous caller", challenger.sent)
	}
}
