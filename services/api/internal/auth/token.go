package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrTokenInvalid = errors.New("auth: token is invalid")
	ErrTokenExpired = errors.New("auth: token has expired")
)

// refreshTokenBytes is the entropy in an opaque refresh token.
//
// 32 bytes — 256 bits. These are bearer credentials valid for 30 days
// and stored only as a hash, so there is no rate limit standing between
// an attacker and a guess; the only defence is that the space is too
// large to search.
const refreshTokenBytes = 32

// Claims is what an access token carries.
//
// Deliberately minimal, and deliberately free of PII. A JWT is base64,
// not encryption: anyone holding one can read every claim, and tokens
// end up in browser storage, proxy logs, error reports and support
// tickets. `sub` is an opaque UUID that means nothing without database
// access.
//
// No name, no email, no phone. The temptation is real — putting the name
// in the token saves a query on every request — and it is exactly how
// PII ends up in a log aggregator forever.
type Claims struct {
	Role string `json:"role"`
	// Fam is the rotation family this token was issued into — the
	// device, in user-facing terms.
	//
	// Added deliberately, and it is not PII: an opaque UUID identifying a
	// session lineage, meaningless without database access, exactly like
	// `sub`. It is here because the sessions screen must be able to say
	// "this device" — and without it the API cannot tell, since the
	// refresh cookie is scoped to /api/v1/auth and never reaches
	// /users/me/sessions.
	Fam string `json:"fam"`
	jwt.RegisteredClaims
}

// TokenPair is what a successful authentication returns.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	// FamilyID ties a refresh token to its rotation lineage. The caller
	// persists it; detecting one leaked token revokes the whole family.
	FamilyID  uuid.UUID
	ExpiresAt time.Time
}

// Issuer mints and validates tokens.
type Issuer struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	// now is injectable so expiry can be tested without sleeping. Nil
	// means time.Now — see nowFunc.
	now func() time.Time
}

func NewIssuer(secret string, accessTTL, refreshTTL time.Duration) (*Issuer, error) {
	// HS256 with a short key is brute-forceable offline, and every token
	// ever issued stays forgeable afterwards. Config validation already
	// enforces 32; refusing here too means a caller constructing an
	// Issuer directly cannot bypass it.
	if len(secret) < 32 {
		return nil, fmt.Errorf("auth: JWT secret is %d bytes, need at least 32", len(secret))
	}
	if accessTTL <= 0 || refreshTTL <= 0 {
		return nil, fmt.Errorf("auth: token TTLs must be positive")
	}
	return &Issuer{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}, nil
}

func (i *Issuer) nowFunc() time.Time {
	if i.now != nil {
		return i.now()
	}
	return time.Now()
}

// AccessTTL and RefreshTTL expose the configured lifetimes so handlers
// can set cookie expiry without a second source of truth.
func (i *Issuer) AccessTTL() time.Duration  { return i.accessTTL }
func (i *Issuer) RefreshTTL() time.Duration { return i.refreshTTL }

// IssueAccessToken mints a short-lived JWT.
//
// `jti` is present so a specific token can be denied before it expires
// (Phase 6 adds the denylist). Without it, revocation can only be
// per-user, which logs someone out of every device to deal with one.
func (i *Issuer) IssueAccessToken(userID, familyID uuid.UUID, role string) (string, error) {
	now := i.nowFunc()

	claims := Claims{
		Role: role,
		Fam:  familyID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.accessTTL)),
			Issuer:    "ayana-api",
		},
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.secret)
	if err != nil {
		return "", fmt.Errorf("auth: sign access token: %w", err)
	}
	return signed, nil
}

// VerifyAccessToken parses and validates.
//
// The algorithm is pinned with WithValidMethods. Without it a token
// claiming `"alg": "none"` — or HS256 forged using an RS256 public key
// as the HMAC secret — is accepted. Both are textbook JWT attacks and
// both are one missing option away.
func (i *Issuer) VerifyAccessToken(token string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(
		token,
		&Claims{},
		func(t *jwt.Token) (any, error) {
			// Belt and braces alongside WithValidMethods: if the parser's
			// behaviour ever changes, this still refuses anything that is
			// not HMAC.
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("auth: unexpected signing method %v", t.Header["alg"])
			}
			return i.secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer("ayana-api"),
		jwt.WithTimeFunc(i.nowFunc),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		// Everything else collapses to one error. Telling a caller
		// whether the signature or the structure was wrong helps only
		// someone probing it.
		return nil, ErrTokenInvalid
	}

	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, ErrTokenInvalid
	}
	if _, err := uuid.Parse(claims.Subject); err != nil {
		return nil, ErrTokenInvalid
	}

	return claims, nil
}

// GenerateRefreshToken returns an opaque token and its storage hash.
//
// Opaque, not a JWT. A refresh token is a database lookup by design: it
// must be revocable the instant a device is removed, and a self-contained
// signed token cannot be revoked without the denylist it was meant to
// avoid. It carries no claims because it asserts nothing — it is a
// handle.
//
// URL-safe base64 without padding so it survives cookies, headers and
// query strings without escaping.
func GenerateRefreshToken() (token string, hash []byte, err error) {
	raw := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("auth: read random bytes: %w", err)
	}

	token = base64.RawURLEncoding.EncodeToString(raw)
	sum := HashRefreshToken(token)
	return token, sum, nil
}

// HashRefreshToken returns the sha256 stored in `sessions.refresh_hash`.
//
// Plain sha256, no KDF, for the same reason as OTP codes: the input is
// 256 bits of uniform randomness, so there is no dictionary to attack
// and stretching buys nothing. What it does buy — a database dump not
// being a list of live credentials — comes from hashing at all.
func HashRefreshToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// RefreshTokensMatch compares in constant time.
//
// Lookup is by hash, so a timing leak here is less reachable than in the
// OTP path — but the comparison is free and the exception is not worth
// defending at review time.
func RefreshTokensMatch(storedHash []byte, presented string) bool {
	return hmac.Equal(storedHash, HashRefreshToken(presented))
}

// HashIP returns a salted hash of a client IP.
//
// Raw IPs are never persisted: an IP address is PII under the project's
// rules, and a sessions table full of them is a breach worth having.
//
// HMAC rather than sha256(salt || ip), because the IPv4 space is 2^32 —
// small enough to rainbow-table entirely in minutes. The salt is what
// makes the digest unreproducible without it, and HMAC is the
// construction that uses a key correctly.
func HashIP(ip, salt string) []byte {
	if ip == "" {
		return nil
	}
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write([]byte(ip))
	return mac.Sum(nil)
}
