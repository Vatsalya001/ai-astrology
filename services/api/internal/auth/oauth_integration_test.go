//go:build integration

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis/ratelimit"
)

// The OAuth flow against a stubbed Google and a real Redis.
//
// Google is stubbed because a test cannot hold a real consent screen.
// Redis is NOT stubbed: the state lifecycle — stored, single-use,
// expiring — is the security property, and it lives in Redis commands.
//
// No credentials are needed to run any of this, which is the point: the
// flow is fully exercised now, and pointing it at real Google later is a
// configuration change rather than untested code.

func stubGoogle(t *testing.T, opts stubOptions) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if opts.tokenStatus != 0 {
			w.WriteHeader(opts.tokenStatus)
			return
		}
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// The provider MUST receive the code and the credentials.
		if r.Form.Get("code") == "" || r.Form.Get("client_id") == "" ||
			r.Form.Get("client_secret") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "stub-access-token"})
	})

	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer stub-access-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sub":            opts.subject,
			"email":          opts.email,
			"email_verified": opts.emailVerified,
		})
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

type stubOptions struct {
	subject       string
	email         string
	emailVerified bool
	tokenStatus   int
}

func googleWithStub(t *testing.T, opts stubOptions) (*Google, func()) {
	t.Helper()

	if opts.subject == "" {
		opts.subject = "google-subject-1"
	}
	if opts.email == "" {
		opts.email = "person@example.com"
	}

	server := stubGoogle(t, opts)
	client, stop := startRedis(context.Background(), t)

	g := NewGoogle(GoogleConfig{
		ClientID:     "example-not-a-real-client-id",
		ClientSecret: "example-not-a-real-client-secret",
		RedirectURL:  "http://localhost:4000/api/v1/auth/oauth/google/callback",
	}, client)

	g.authEndpoint = server.URL + "/auth"
	g.tokenEndpoint = server.URL + "/token"
	g.userinfoEndpoint = server.URL + "/userinfo"

	return g, stop
}

func TestOAuthRoundTrip(t *testing.T) {
	ctx := context.Background()
	g, stop := googleWithStub(t, stubOptions{emailVerified: true})
	defer stop()

	authURL, err := g.AuthURL(ctx, "/settings/profile")
	if err != nil {
		t.Fatalf("AuthURL: %v", err)
	}

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	q := parsed.Query()

	if q.Get("state") == "" {
		t.Fatal("no state in the authorization URL — the callback would accept a code from anywhere")
	}
	if q.Get("scope") != "openid email" {
		t.Errorf("scope = %q; asking for more than is needed is a worse consent screen", q.Get("scope"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q, want code", q.Get("response_type"))
	}
	// The implicit flow would put a token in the URL fragment.
	if q.Get("response_type") == "token" {
		t.Error("the implicit flow puts a token in the URL")
	}

	identity, returnTo, err := g.Exchange(ctx, "stub-code", q.Get("state"))
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if identity.Subject != "google-subject-1" {
		t.Errorf("Subject = %q", identity.Subject)
	}
	if identity.Email != "person@example.com" {
		t.Errorf("Email = %q", identity.Email)
	}
	// The return path must survive the round trip, or every OAuth login
	// dumps the user on the home page regardless of where they started.
	if returnTo != "/settings/profile" {
		t.Errorf("returnTo = %q, want /settings/profile", returnTo)
	}
}

// THE test for this file.
//
// Without state validation the callback accepts a code from anywhere,
// which is login CSRF: an attacker sends a victim a callback URL
// carrying the ATTACKER's code, the victim silently ends up signed into
// the attacker's account, and everything they do there is visible to its
// owner.
func TestExchangeRejectsAnyStateItDidNotIssue(t *testing.T) {
	ctx := context.Background()
	g, stop := googleWithStub(t, stubOptions{emailVerified: true})
	defer stop()

	cases := map[string]string{
		"empty":        "",
		"never issued": "a-state-this-server-never-created",
		"wrong shape":  "../../etc/passwd",
		"whitespace":   "   ",
	}

	for name, state := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := g.Exchange(ctx, "stub-code", state)
			if !errors.Is(err, ErrOAuthStateInvalid) {
				t.Fatalf("got %v, want ErrOAuthStateInvalid — a forged state was accepted", err)
			}
		})
	}
}

// Single use. A replayed callback must find nothing, or a state captured
// from a Referer header or a browser history stays usable.
func TestStateCannotBeReplayed(t *testing.T) {
	ctx := context.Background()
	g, stop := googleWithStub(t, stubOptions{emailVerified: true})
	defer stop()

	authURL, err := g.AuthURL(ctx, "")
	if err != nil {
		t.Fatalf("AuthURL: %v", err)
	}
	state := mustQuery(t, authURL, "state")

	if _, _, err := g.Exchange(ctx, "stub-code", state); err != nil {
		t.Fatalf("first exchange: %v", err)
	}
	if _, _, err := g.Exchange(ctx, "stub-code", state); !errors.Is(err, ErrOAuthStateInvalid) {
		t.Fatalf("a replayed state returned %v, want ErrOAuthStateInvalid", err)
	}
}

// GETDEL is atomic for this reason: a read-then-delete would let two
// concurrent replays both pass the check before either deleted.
func TestConcurrentExchangeConsumesTheStateOnce(t *testing.T) {
	ctx := context.Background()
	g, stop := googleWithStub(t, stubOptions{emailVerified: true})
	defer stop()

	authURL, err := g.AuthURL(ctx, "")
	if err != nil {
		t.Fatalf("AuthURL: %v", err)
	}
	state := mustQuery(t, authURL, "state")

	const goroutines = 16
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		wins    int
		release = make(chan struct{})
	)

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-release
			if _, _, err := g.Exchange(ctx, "stub-code", state); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	close(release)
	wg.Wait()

	if wins != 1 {
		t.Fatalf("%d of %d concurrent exchanges succeeded; exactly 1 must", wins, goroutines)
	}
}

// An unverified email must not link an account. Google hands back
// addresses the holder has never proved they own, and accepting one lets
// somebody claim the account of whoever really owns that address.
func TestUnverifiedEmailIsRefused(t *testing.T) {
	ctx := context.Background()
	g, stop := googleWithStub(t, stubOptions{emailVerified: false})
	defer stop()

	authURL, err := g.AuthURL(ctx, "")
	if err != nil {
		t.Fatalf("AuthURL: %v", err)
	}

	_, _, err = g.Exchange(ctx, "stub-code", mustQuery(t, authURL, "state"))
	if !errors.Is(err, ErrOAuthEmailUnverified) {
		t.Fatalf("got %v, want ErrOAuthEmailUnverified", err)
	}
}

func TestStateExpires(t *testing.T) {
	ctx := context.Background()
	g, stop := googleWithStub(t, stubOptions{emailVerified: true})
	defer stop()

	authURL, err := g.AuthURL(ctx, "")
	if err != nil {
		t.Fatalf("AuthURL: %v", err)
	}
	state := mustQuery(t, authURL, "state")

	// PExpire, not Expire: go-redis rounds a sub-second duration UP to a
	// full second, so `Expire(..., time.Millisecond)` leaves the key alive
	// for another second and the test reads as a missing expiry check.
	if err := g.redis.PExpire(ctx, stateKey(state), 10*time.Millisecond).Err(); err != nil {
		t.Fatalf("expire: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	if _, _, err := g.Exchange(ctx, "stub-code", state); !errors.Is(err, ErrOAuthStateInvalid) {
		t.Fatalf("an expired state returned %v, want ErrOAuthStateInvalid", err)
	}
}

// A token-endpoint failure must not surface the body: it can echo the
// client secret back inside an error message.
func TestExchangeFailureLeaksNothing(t *testing.T) {
	ctx := context.Background()
	g, stop := googleWithStub(t, stubOptions{emailVerified: true, tokenStatus: http.StatusBadRequest})
	defer stop()

	authURL, err := g.AuthURL(ctx, "")
	if err != nil {
		t.Fatalf("AuthURL: %v", err)
	}

	_, _, err = g.Exchange(ctx, "stub-code", mustQuery(t, authURL, "state"))
	if !errors.Is(err, ErrOAuthExchangeFailed) {
		t.Fatalf("got %v, want ErrOAuthExchangeFailed", err)
	}
	if contains(err.Error(), "client-secret") || contains(err.Error(), "example-not-a-real-client-secret") {
		t.Errorf("the error carries the client secret: %v", err)
	}
}

// Without credentials the flow is a clear "not available", never a 500
// that looks like an outage.
func TestUnconfiguredProviderSaysSo(t *testing.T) {
	ctx := context.Background()
	client, stop := startRedis(ctx, t)
	defer stop()

	g := NewGoogle(GoogleConfig{}, client)

	if g.Configured() {
		t.Fatal("Configured() is true with no credentials")
	}
	if _, err := g.AuthURL(ctx, ""); !errors.Is(err, ErrOAuthNotConfigured) {
		t.Errorf("AuthURL returned %v, want ErrOAuthNotConfigured", err)
	}
	if _, _, err := g.Exchange(ctx, "c", "s"); !errors.Is(err, ErrOAuthNotConfigured) {
		t.Errorf("Exchange returned %v, want ErrOAuthNotConfigured", err)
	}
}

func mustQuery(t *testing.T, rawURL, key string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	value := parsed.Query().Get(key)
	if value == "" {
		t.Fatalf("no %q in %s", key, rawURL)
	}
	return value
}

// ─── The two handler tests that need a real limiter ──────────────────
//
// OAuthStart consults the rate limiter before it writes a state key, and
// the limiter is a concrete *ratelimit.Limiter rather than an interface,
// so these cannot run against a nil one. They live here rather than
// weakening the handler's type to make a unit test possible.

func throttledHandler(t *testing.T) *Handler {
	t.Helper()
	client, stop := startRedis(context.Background(), t)
	t.Cleanup(stop)

	return NewHandler(HandlerConfig{
		Limiter: ratelimit.New(client),
		IPSalt:  "integration-test-salt-not-a-secret",
		WriteError: func(w http.ResponseWriter, _ *http.Request, status int, code, message string, _ error) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{"code": code, "message": message},
			})
		},
	})
}

func TestOAuthStartRedirectsToConsent(t *testing.T) {
	h := throttledHandler(t)
	provider := &fakeProvider{
		configured: true,
		authURL:    "https://accounts.google.com/o/oauth2/v2/auth?state=abc",
	}
	rec := httptest.NewRecorder()

	h.OAuthStart(provider, "http://localhost:3000")(
		rec, httptest.NewRequest(http.MethodGet, "/auth/oauth/google?return_to=/settings", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != provider.authURL {
		t.Errorf("Location = %q, want %q", got, provider.authURL)
	}
	if provider.gotReturnTo != "/settings" {
		t.Errorf("return_to reached the provider as %q, want /settings", provider.gotReturnTo)
	}
}

// The open-redirect guard must be applied at the ENTRY to the flow, not
// only inside safeReturnTo's own unit test. A hostile return_to that is
// merely stored and replayed on the callback is the same bug, later.
func TestOAuthStartStripsOffSiteReturnTo(t *testing.T) {
	h := throttledHandler(t)
	provider := &fakeProvider{configured: true, authURL: "https://accounts.google.com/x"}

	h.OAuthStart(provider, "http://localhost:3000")(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/auth/oauth/google?return_to=https://evil.example", nil))

	if provider.gotReturnTo != "" {
		t.Fatalf("an off-site return_to reached the provider as %q", provider.gotReturnTo)
	}
}

// Starting a flow writes a Redis key with a ten-minute TTL before the
// caller has proved anything. Unlimited, that is an unauthenticated
// write amplifier.
func TestOAuthStartIsRateLimited(t *testing.T) {
	h := throttledHandler(t)
	provider := &fakeProvider{configured: true, authURL: "https://accounts.google.com/x"}

	var allowed, denied int
	for range ratelimit.OAuthStartPerIP.Max + 5 {
		rec := httptest.NewRecorder()
		h.OAuthStart(provider, "http://localhost:3000")(
			rec, httptest.NewRequest(http.MethodGet, "/auth/oauth/google", nil))

		switch rec.Code {
		case http.StatusFound:
			allowed++
		case http.StatusTooManyRequests:
			denied++
			if rec.Header().Get("Retry-After") == "" {
				t.Error("a 429 with no Retry-After leaves the client guessing")
			}
		default:
			t.Fatalf("unexpected status %d", rec.Code)
		}
	}

	if allowed != ratelimit.OAuthStartPerIP.Max {
		t.Errorf("%d requests were allowed, want %d", allowed, ratelimit.OAuthStartPerIP.Max)
	}
	if denied != 5 {
		t.Errorf("%d requests were denied, want 5", denied)
	}
}
