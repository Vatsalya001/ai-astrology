package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// safeReturnTo is the open-redirect guard.
//
// Without it, `?return_to=https://evil.example` turns the callback into
// an open redirect: an attacker sends a link that completes a REAL
// sign-in and then lands the user on a page they control — which is a
// very convincing place to ask for something, because the user did just
// legitimately authenticate.
func TestSafeReturnToRefusesAnythingOffSite(t *testing.T) {
	refused := []string{
		"https://evil.example",
		"http://evil.example/path",
		// Protocol-relative: a browser reads this as a host, not a path.
		"//evil.example",
		"///evil.example",
		// Backslashes are normalised to slashes by some browsers.
		"/\\evil.example",
		"\\\\evil.example",
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"HTTPS://evil.example",
	}

	for _, raw := range refused {
		if got := safeReturnTo(raw); got != "" {
			t.Errorf("safeReturnTo(%q) = %q; it must refuse anything off-site", raw, got)
		}
	}
}

func TestSafeReturnToAllowsLocalPaths(t *testing.T) {
	allowed := map[string]string{
		"/home":                 "/home",
		"/settings/profile":     "/settings/profile",
		"/settings?tab=devices": "/settings?tab=devices",
		"":                      "",
	}

	for raw, want := range allowed {
		if got := safeReturnTo(raw); got != want {
			t.Errorf("safeReturnTo(%q) = %q, want %q", raw, got, want)
		}
	}
}

// The state key is a hash, so a Redis dump is not a list of live states
// and a key logged by MONITOR does not hand one over.
func TestStateKeyIsHashed(t *testing.T) {
	const state = "a-state-value"
	key := stateKey(state)

	if strings.Contains(key, state) {
		t.Errorf("the Redis key contains the raw state: %s", key)
	}
	if stateKey(state) != key {
		t.Error("stateKey is not deterministic; lookup could never succeed")
	}
	if stateKey("different") == key {
		t.Error("two states produced the same key")
	}
}

func TestRandomTokenIsUnpredictable(t *testing.T) {
	seen := map[string]bool{}
	for range 500 {
		token, err := randomToken()
		if err != nil {
			t.Fatalf("randomToken: %v", err)
		}
		if seen[token] {
			t.Fatal("randomToken returned a duplicate")
		}
		seen[token] = true
		// 32 bytes → 43 unpadded base64url characters.
		if len(token) != 43 {
			t.Errorf("token is %d chars, want 43", len(token))
		}
	}
}

// ─── The HTTP surface ────────────────────────────────────────────────

// fakeProvider stands in for *Google. The provider's own behaviour is
// covered against a stubbed Google in oauth_integration_test.go; what is
// under test here is what the HANDLER does with each outcome.
type fakeProvider struct {
	configured  bool
	authURL     string
	authErr     error
	identity    GoogleIdentity
	returnTo    string
	exchangeErr error

	gotReturnTo string
}

func (f *fakeProvider) Configured() bool { return f.configured }

func (f *fakeProvider) AuthURL(_ context.Context, returnTo string) (string, error) {
	f.gotReturnTo = returnTo
	return f.authURL, f.authErr
}

func (f *fakeProvider) Exchange(context.Context, string, string) (GoogleIdentity, string, error) {
	return f.identity, f.returnTo, f.exchangeErr
}

func testHandler() *Handler {
	return NewHandler(HandlerConfig{
		WriteError: func(w http.ResponseWriter, _ *http.Request, status int, code, message string, _ error) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{"code": code, "message": message},
			})
		},
	})
}

// Without credentials the route must say so, not 500 and not 404.
//
// A 500 reads as an outage and generates a support ticket; a 404 reads as
// a broken build. "Not available yet, use email" is the truth and it is
// actionable.
func TestOAuthStartWithoutCredentialsSaysNotConfigured(t *testing.T) {
	h := testHandler()
	rec := httptest.NewRecorder()

	h.OAuthStart(&fakeProvider{configured: false}, "http://localhost:3000")(
		rec, httptest.NewRequest(http.MethodGet, "/auth/oauth/google", nil))

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "OAUTH_NOT_CONFIGURED") {
		t.Errorf("body does not carry the code the UI branches on: %s", rec.Body.String())
	}
	// The reply must not name an environment variable or a config field.
	// That is internal detail and an aid to anyone probing the deployment.
	for _, leak := range []string{"GOOGLE_CLIENT", "client_secret", "ClientID"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("the reply leaks %q: %s", leak, rec.Body.String())
		}
	}
}

// A declined consent screen comes back as ?error=access_denied, not as an
// HTTP status. It is a normal outcome and must land the user back on the
// sign-in page rather than on an error page.
func TestOAuthCallbackHandlesADeclinedConsentScreen(t *testing.T) {
	h := testHandler()
	rec := httptest.NewRecorder()

	h.OAuthCallback(&fakeProvider{configured: true}, nil, "google", "http://localhost:3000")(
		rec, httptest.NewRequest(http.MethodGet,
			"/auth/oauth/google/callback?error=access_denied", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "http://localhost:3000/auth?error=cancelled" {
		t.Errorf("Location = %q", got)
	}
}

// Every exchange failure looks identical to the browser. Telling the
// caller whether the STATE or the CODE was wrong is a probing aid and
// helps nobody who is signing in honestly.
func TestOAuthCallbackFailureIsUniform(t *testing.T) {
	h := testHandler()

	for name, err := range map[string]error{
		"forged state":   ErrOAuthStateInvalid,
		"bad code":       ErrOAuthExchangeFailed,
		"unverified":     ErrOAuthEmailUnverified,
		"not configured": ErrOAuthNotConfigured,
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.OAuthCallback(
				&fakeProvider{configured: true, exchangeErr: err},
				nil, "google", "http://localhost:3000",
			)(rec, httptest.NewRequest(http.MethodGet,
				"/auth/oauth/google/callback?code=c&state=s", nil))

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if strings.Contains(rec.Body.String(), "state") ||
				strings.Contains(rec.Body.String(), "verified") {
				t.Errorf("the reply distinguishes failure causes: %s", rec.Body.String())
			}
		})
	}
}

// The web app reads this to decide whether to render the button. If it
// lied, the first person to click would get a 503.
func TestProvidersReportsWhatIsActuallyConfigured(t *testing.T) {
	h := testHandler()

	for _, configured := range []bool{true, false} {
		rec := httptest.NewRecorder()
		h.Providers(&fakeProvider{configured: configured})(
			rec, httptest.NewRequest(http.MethodGet, "/auth/providers", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body map[string]bool
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["google"] != configured {
			t.Errorf("google = %v, want %v", body["google"], configured)
		}
	}
}
