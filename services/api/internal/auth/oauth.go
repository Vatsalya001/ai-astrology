package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

var (
	// ErrOAuthNotConfigured is a supported state, not a failure. The flow
	// ships without credentials so the rest of auth works; the route says
	// so plainly rather than 500ing.
	ErrOAuthNotConfigured = errors.New("auth: oauth provider is not configured")

	// ErrOAuthStateInvalid covers a missing, unknown, expired or already
	// used state. They are one error deliberately — a caller cannot act
	// differently, and distinguishing them helps only someone probing.
	ErrOAuthStateInvalid = errors.New("auth: oauth state is invalid")

	ErrOAuthExchangeFailed  = errors.New("auth: oauth code exchange failed")
	ErrOAuthEmailUnverified = errors.New("auth: oauth account has no verified email")
)

// oauthStateTTL bounds how long a login can sit half-finished.
//
// Ten minutes is generous for "click Google, pick an account, come
// back". Longer widens the window in which a captured state is useful.
const oauthStateTTL = 10 * time.Minute

// GoogleConfig is what the provider needs.
type GoogleConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

func (c GoogleConfig) configured() bool {
	return c.ClientID != "" && c.ClientSecret != ""
}

// Google implements the authorization-code flow.
//
// Hand-rolled rather than golang.org/x/oauth2 because the flow is three
// HTTP calls and the library's value is in the parts we do not use —
// token refresh, scoped clients, provider registries. What we DO need,
// state validation, it leaves entirely to the caller.
type Google struct {
	cfg    GoogleConfig
	redis  *goredis.Client
	client *http.Client

	// Endpoints are fields so a test can point them at httptest rather
	// than at Google.
	authEndpoint     string
	tokenEndpoint    string
	userinfoEndpoint string
}

func NewGoogle(cfg GoogleConfig, redis *goredis.Client) *Google {
	return &Google{
		cfg:   cfg,
		redis: redis,
		client: &http.Client{
			// Bounded: an OAuth provider that hangs must not hold a
			// request open indefinitely.
			Timeout: 10 * time.Second,
		},
		authEndpoint:     "https://accounts.google.com/o/oauth2/v2/auth",
		tokenEndpoint:    "https://oauth2.googleapis.com/token",
		userinfoEndpoint: "https://openidconnect.googleapis.com/v1/userinfo",
	}
}

func (g *Google) Configured() bool { return g.cfg.configured() }

// AuthURL begins the flow.
//
// The returned state is stored server-side and must come back unchanged.
// Without that check the callback accepts a code from anywhere, which is
// login CSRF: an attacker sends a victim a callback URL carrying the
// ATTACKER's code, and the victim silently ends up signed into the
// attacker's account — where everything they then do is visible to its
// owner.
func (g *Google) AuthURL(ctx context.Context, returnTo string) (string, error) {
	if !g.Configured() {
		return "", ErrOAuthNotConfigured
	}

	state, err := randomToken()
	if err != nil {
		return "", err
	}

	// Stored hashed, like every other short-lived secret here: a Redis
	// dump should not be a list of live states.
	if err := g.redis.Set(ctx, stateKey(state), returnTo, oauthStateTTL).Err(); err != nil {
		return "", fmt.Errorf("auth: store oauth state: %w", err)
	}

	params := url.Values{
		"client_id":     {g.cfg.ClientID},
		"redirect_uri":  {g.cfg.RedirectURL},
		"response_type": {"code"},
		// openid and email only. Asking for more than is needed is both a
		// worse consent screen and more data to hold.
		"scope": {"openid email"},
		"state": {state},
		// Forces the account chooser rather than silently reusing
		// whichever Google account the browser last used — which is
		// surprising on a shared machine.
		"prompt": {"select_account"},
	}

	return g.authEndpoint + "?" + params.Encode(), nil
}

// GoogleIdentity is what the callback establishes.
type GoogleIdentity struct {
	// Subject is Google's stable user id. The identity is keyed on this,
	// never on the email: an email can be reassigned within a Google
	// Workspace domain, and keying on it would hand the new owner the old
	// owner's account.
	Subject string
	Email   string
}

// Exchange completes the flow.
func (g *Google) Exchange(ctx context.Context, code, state string) (GoogleIdentity, string, error) {
	if !g.Configured() {
		return GoogleIdentity{}, "", ErrOAuthNotConfigured
	}
	if code == "" || state == "" {
		return GoogleIdentity{}, "", ErrOAuthStateInvalid
	}

	// Single use: GETDEL consumes the state atomically, so a replayed
	// callback finds nothing. A read-then-delete would let two concurrent
	// replays both pass.
	returnTo, err := g.redis.GetDel(ctx, stateKey(state)).Result()
	if err != nil {
		return GoogleIdentity{}, "", ErrOAuthStateInvalid
	}

	token, err := g.exchangeCode(ctx, code)
	if err != nil {
		return GoogleIdentity{}, "", err
	}

	identity, err := g.fetchIdentity(ctx, token)
	if err != nil {
		return GoogleIdentity{}, "", err
	}

	return identity, returnTo, nil
}

func (g *Google) exchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{
		"code":          {code},
		"client_id":     {g.cfg.ClientID},
		"client_secret": {g.cfg.ClientSecret},
		"redirect_uri":  {g.cfg.RedirectURL},
		"grant_type":    {"authorization_code"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		g.tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("auth: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrOAuthExchangeFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// The body can echo the client secret back in an error message.
		// Status only.
		return "", fmt.Errorf("%w: status %d", ErrOAuthExchangeFailed, resp.StatusCode)
	}

	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("%w: decode: %v", ErrOAuthExchangeFailed, err)
	}
	if body.AccessToken == "" {
		return "", ErrOAuthExchangeFailed
	}
	return body.AccessToken, nil
}

func (g *Google) fetchIdentity(ctx context.Context, accessToken string) (GoogleIdentity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.userinfoEndpoint, nil)
	if err != nil {
		return GoogleIdentity{}, fmt.Errorf("auth: build userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := g.client.Do(req)
	if err != nil {
		return GoogleIdentity{}, fmt.Errorf("%w: %v", ErrOAuthExchangeFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return GoogleIdentity{}, fmt.Errorf("%w: userinfo status %d", ErrOAuthExchangeFailed, resp.StatusCode)
	}

	var body struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return GoogleIdentity{}, fmt.Errorf("%w: decode userinfo: %v", ErrOAuthExchangeFailed, err)
	}

	if body.Sub == "" {
		return GoogleIdentity{}, ErrOAuthExchangeFailed
	}

	// An unverified email must not link an account. Google will hand back
	// addresses the holder has never proved they own, and accepting one
	// lets somebody claim an account belonging to whoever really owns
	// that address.
	if !body.EmailVerified || body.Email == "" {
		return GoogleIdentity{}, ErrOAuthEmailUnverified
	}

	return GoogleIdentity{Subject: body.Sub, Email: strings.ToLower(body.Email)}, nil
}

// randomToken returns 32 bytes of URL-safe randomness.
func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("auth: read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func stateKey(state string) string {
	sum := sha256.Sum256([]byte(state))
	return "oauth:state:" + base64.RawURLEncoding.EncodeToString(sum[:])
}
