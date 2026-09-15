package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Deliberately not a random-looking string. A value that *looks* like a
// credential trips the gitleaks pre-commit hook, and silencing that with
// an allowlist entry would blunt the scanner for real secrets too. The
// only property under test is that it clears the 32-byte minimum.
const testSecret = "example-not-a-real-signing-secret-abcd"

func testIssuer(t *testing.T) *Issuer {
	t.Helper()
	iss, err := NewIssuer(testSecret, 15*time.Minute, 720*time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	return iss
}

// HS256 with a short key is brute-forceable offline, and every token
// ever issued stays forgeable afterwards. Refusing at construction means
// a caller building an Issuer directly cannot bypass config validation.
func TestNewIssuerRejectsWeakConfiguration(t *testing.T) {
	cases := map[string]struct {
		secret     string
		accessTTL  time.Duration
		refreshTTL time.Duration
	}{
		"empty secret":     {"", time.Minute, time.Hour},
		"short secret":     {"too-short", time.Minute, time.Hour},
		"31 bytes":         {strings.Repeat("x", 31), time.Minute, time.Hour},
		"zero access ttl":  {testSecret, 0, time.Hour},
		"negative access":  {testSecret, -time.Minute, time.Hour},
		"zero refresh ttl": {testSecret, time.Minute, 0},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewIssuer(tc.secret, tc.accessTTL, tc.refreshTTL); err == nil {
				t.Error("NewIssuer accepted a configuration it must refuse")
			}
		})
	}

	// Exactly 32 is the documented minimum and must be accepted.
	if _, err := NewIssuer(strings.Repeat("x", 32), time.Minute, time.Hour); err != nil {
		t.Errorf("NewIssuer rejected a 32-byte secret: %v", err)
	}
}

func TestAccessTokenRoundTrip(t *testing.T) {
	iss := testIssuer(t)
	userID := uuid.New()

	token, err := iss.IssueAccessToken(userID, uuid.New(), "user")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	claims, err := iss.VerifyAccessToken(token)
	if err != nil {
		t.Fatalf("VerifyAccessToken: %v", err)
	}
	if claims.Subject != userID.String() {
		t.Errorf("Subject = %q, want %q", claims.Subject, userID.String())
	}
	if claims.Role != "user" {
		t.Errorf("Role = %q, want user", claims.Role)
	}
	if claims.ID == "" {
		t.Error("jti is empty; a specific token could never be denied before expiry")
	}
}

// THE test for this file.
//
// A JWT is base64, not encryption. Anyone holding one can read every
// claim, and tokens end up in browser storage, proxy logs, error reports
// and support tickets. The temptation to put a name in — saving a query
// per request — is exactly how PII ends up in a log aggregator forever.
//
// Decoded from the wire rather than from the struct, because that is
// what an attacker sees.
func TestAccessTokenClaimsContainNoPII(t *testing.T) {
	iss := testIssuer(t)
	userID := uuid.New()

	token, err := iss.IssueAccessToken(userID, uuid.New(), "astrologer")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3", len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	// An allowlist, not a denylist. A denylist passes the first time
	// someone adds a claim nobody thought to forbid.
	allowed := map[string]bool{
		"sub": true, "role": true, "jti": true,
		// `fam` identifies the rotation family — the device. Added
		// deliberately and reviewed here: an opaque UUID, meaningless
		// without database access, exactly like `sub`. Widening this list
		// is the review step, which is why the test uses an allowlist.
		"fam": true,
		"exp": true, "iat": true, "nbf": true, "iss": true,
	}
	for claim := range decoded {
		if !allowed[claim] {
			t.Errorf("unexpected claim %q in the access token — every claim is "+
				"readable by anyone holding the token, so new ones need "+
				"deliberate review: %v", claim, decoded[claim])
		}
	}

	// And explicitly: none of these, under any key.
	raw := strings.ToLower(string(payload))
	for _, forbidden := range []string{
		"email", "phone", "name", "gender", "birth", "address",
		"@", "+91",
	} {
		if strings.Contains(raw, forbidden) {
			t.Errorf("the token payload contains %q: %s", forbidden, payload)
		}
	}
}

// Pinning the algorithm is what stops the two textbook JWT attacks.
// Without WithValidMethods a token claiming "alg":"none" is accepted, and
// so is HS256 forged with an RS256 public key as the HMAC secret.
func TestVerifyRejectsAlgNone(t *testing.T) {
	iss := testIssuer(t)

	// Hand-roll an unsigned token with the same claims a real one has.
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(
		`{"sub":"` + uuid.NewString() + `","role":"admin","iss":"ayana-api","exp":9999999999}`))
	forged := header + "." + payload + "."

	if _, err := iss.VerifyAccessToken(forged); err == nil {
		t.Fatal("an alg=none token was accepted — anyone can mint an admin token")
	}
}

func TestVerifyRejectsWrongSignature(t *testing.T) {
	iss := testIssuer(t)
	userID := uuid.New()

	token, err := iss.IssueAccessToken(userID, uuid.New(), "user")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	// A different secret must not validate.
	other, err := NewIssuer("example-not-a-real-other-secret-wxyz12", time.Minute, time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	if _, err := other.VerifyAccessToken(token); err == nil {
		t.Error("a token signed with another key validated")
	}

	// Tampering with the payload must invalidate it. This is the attack
	// that matters: escalate role to admin and re-encode.
	parts := strings.Split(token, ".")
	tampered := parts[0] + "." +
		base64.RawURLEncoding.EncodeToString([]byte(
			`{"sub":"`+userID.String()+`","role":"super_admin","iss":"ayana-api","exp":9999999999}`)) +
		"." + parts[2]
	if _, err := iss.VerifyAccessToken(tampered); err == nil {
		t.Error("a token with an edited role claim validated — privilege escalation")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	iss := testIssuer(t)

	for _, bad := range []string{
		"", "not-a-token", "a.b", "a.b.c.d",
		"....", "eyJhbGciOiJIUzI1NiJ9",
	} {
		if _, err := iss.VerifyAccessToken(bad); err == nil {
			t.Errorf("malformed token %q validated", bad)
		}
	}
}

// Expiry, tested by moving the clock rather than sleeping 15 minutes.
func TestAccessTokenExpires(t *testing.T) {
	iss := testIssuer(t)
	base := time.Now()
	iss.now = func() time.Time { return base }

	token, err := iss.IssueAccessToken(uuid.New(), uuid.New(), "user")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	// Still inside the window.
	iss.now = func() time.Time { return base.Add(14 * time.Minute) }
	if _, err := iss.VerifyAccessToken(token); err != nil {
		t.Fatalf("token rejected before expiry: %v", err)
	}

	// Past it.
	iss.now = func() time.Time { return base.Add(16 * time.Minute) }
	_, err = iss.VerifyAccessToken(token)
	if err != ErrTokenExpired {
		t.Errorf("after expiry got %v, want ErrTokenExpired", err)
	}
}

// A token from another system that happens to share our secret must not
// be accepted. Checking the issuer costs nothing and closes it.
func TestVerifyRejectsForeignIssuer(t *testing.T) {
	iss := testIssuer(t)

	claims := Claims{
		Role: "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   uuid.NewString(),
			Issuer:    "some-other-service",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	foreign, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if _, err := iss.VerifyAccessToken(foreign); err == nil {
		t.Error("a token from a different issuer validated")
	}
}

// `sub` is load-bearing: every authorization decision keys off it. A
// non-UUID subject means something else minted the token.
func TestVerifyRejectsNonUUIDSubject(t *testing.T) {
	iss := testIssuer(t)

	claims := Claims{
		Role: "user",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "not-a-uuid",
			Issuer:    "ayana-api",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if _, err := iss.VerifyAccessToken(token); err == nil {
		t.Error("a token with a non-UUID subject validated")
	}
}

// ─── Refresh tokens ──────────────────────────────────────────────────

func TestGenerateRefreshTokenIsOpaqueAndUnique(t *testing.T) {
	const draws = 1000
	seen := make(map[string]bool, draws)

	for range draws {
		token, hash, err := GenerateRefreshToken()
		if err != nil {
			t.Fatalf("GenerateRefreshToken: %v", err)
		}
		if seen[token] {
			t.Fatal("GenerateRefreshToken returned a duplicate")
		}
		seen[token] = true

		// Opaque: not a JWT, carrying no readable structure.
		if strings.Count(token, ".") == 2 {
			t.Error("the refresh token looks like a JWT; it must be opaque")
		}
		if len(hash) != 32 {
			t.Errorf("hash is %d bytes, want 32 (sha256)", len(hash))
		}
		// 32 random bytes → 43 base64url chars without padding.
		if len(token) != 43 {
			t.Errorf("token is %d chars, want 43 (32 bytes, unpadded base64url)", len(token))
		}
		// URL-safe so it survives cookies, headers and query strings.
		if strings.ContainsAny(token, "+/=") {
			t.Errorf("token %q contains characters that need escaping", token)
		}
	}
}

func TestHashRefreshTokenDoesNotEmbedTheToken(t *testing.T) {
	token, hash, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}

	if strings.Contains(string(hash), token) {
		t.Error("the stored hash contains the plaintext token")
	}
	// Deterministic, or lookup by hash could never find the row.
	if string(HashRefreshToken(token)) != string(hash) {
		t.Error("HashRefreshToken is not deterministic")
	}
}

func TestRefreshTokensMatch(t *testing.T) {
	token, hash, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}

	if !RefreshTokensMatch(hash, token) {
		t.Error("the correct token did not match its own hash")
	}

	other, _, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}
	if RefreshTokensMatch(hash, other) {
		t.Error("a different token matched")
	}
	for _, wrong := range []string{"", token + "x", token[:len(token)-1]} {
		if RefreshTokensMatch(hash, wrong) {
			t.Errorf("a malformed token %q matched", wrong)
		}
	}
}

// ─── IP hashing ──────────────────────────────────────────────────────

func TestHashIP(t *testing.T) {
	const salt = "a-test-salt-value"

	h1 := HashIP("203.0.113.7", salt)
	h2 := HashIP("203.0.113.7", salt)
	h3 := HashIP("203.0.113.8", salt)

	if string(h1) != string(h2) {
		t.Error("HashIP is not deterministic; rate limiting would never match")
	}
	if string(h1) == string(h3) {
		t.Error("two different IPs hashed identically")
	}
	if strings.Contains(string(h1), "203.0.113.7") {
		t.Error("the hash contains the raw IP")
	}
	if len(h1) != 32 {
		t.Errorf("hash is %d bytes, want 32", len(h1))
	}
	if HashIP("", salt) != nil {
		t.Error("an empty IP should hash to nil rather than to a constant")
	}
}

// The salt is the whole defence. IPv4 is 2^32 — small enough to
// rainbow-table entirely in minutes — so an unsalted digest is
// reversible and the "hashing" is decorative.
func TestHashIPDependsOnTheSalt(t *testing.T) {
	a := HashIP("203.0.113.7", "salt-one")
	b := HashIP("203.0.113.7", "salt-two")

	if string(a) == string(b) {
		t.Error("the same IP hashed identically under two different salts — " +
			"the salt is not being used, so the digest is rainbow-tableable")
	}
}
