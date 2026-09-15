# Phase 1 — Authentication & User Profiles (Go)

| | |
|---|---|
| **Goal** | Identity. A person can create an account, prove it's theirs, manage a profile, and delete everything. |
| **Deliverable** | Signup and login via phone or email OTP, profile and preference management, and permanent account deletion — all in `api-service`. |
| **Depends on** | Phase 0 |
| **Unlocks** | Phase 2 |
| **Estimated size** | 6–9 days |
| **Cost to run** | ₹0 — OTPs print to stdout, emails land in Mailpit |

Entirely a Go phase. Neither Python service is touched.

---

## 1. Scope

### In scope
- Phone OTP and email OTP. **Social sign-in is out of scope** — see §14.
- Access token (short-lived JWT) + refresh token (rotating, revocable)
- RBAC: `USER`, `ASTROLOGER`, `ADMIN`, `SUPER_ADMIN`
- User profile, preferences, language selection
- Rate limiting on every auth endpoint
- Account deletion — real deletion, not a flag
- Session management — list and revoke devices
- i18n scaffolding (en / hi / hinglish)

### Out of scope
- Birth details — Phase 2, and deliberately a **separate table** from `users`
- Astrologer onboarding — Phase 8 (the role exists now; the workflow doesn't)
- Payments — Phase 7

---

## 2. Architecture

```
Client
  │  POST /api/v1/auth/otp/request  { channel, identifier }
  ▼
internal/auth
  │  rate-limit check (Redis)            ← per identifier AND per IP
  │  generate 6-digit code (crypto/rand)
  │  store sha256(code) in Redis, TTL 5m
  │  dispatch via AuthChannel
  ▼
AuthChannel (interface)
  ├─ dev:  ConsoleChannel  → slog to stdout
  ├─ dev:  SMTPChannel     → Mailpit, view at :8025
  └─ prod: SMSChannel      → MSG91 / Twilio

Client
  │  POST /api/v1/auth/otp/verify  { channel, identifier, code }
  ▼
  │  subtle.ConstantTimeCompare on the hash
  │  consume code (single use, atomic DEL)
  │  find-or-create user      (pgx transaction)
  │  issue access JWT (15m) + refresh token (30d, rotating)
  ▼
{ access_token, refresh_token, user, is_new_user }
```

### Go package layout

```
services/api/internal/
├── auth/
│   ├── service.go        business logic
│   ├── handler.go        chi handlers
│   ├── otp.go            generation, hashing, verification
│   ├── token.go          JWT issue/verify, refresh rotation
│   ├── channel.go        AuthChannel interface + implementations
│   ├── middleware.go     Authenticate, RequireRole
│   └── service_test.go
├── users/
│   ├── service.go  handler.go  service_test.go
└── platform/
    ├── db/dbgen/         sqlc-generated
    └── redis/ratelimit/
```

`auth` and `users` never import each other's internals. Where `auth` needs to create a
user it does so through an interface it declares itself:

```go
// internal/auth/service.go — the consumer defines the interface
type UserCreator interface {
    FindOrCreateByIdentity(ctx context.Context, p FindOrCreateParams) (User, bool, error)
}
```

Idiomatic Go, acyclic dependency graph, and trivially mockable in tests.

### Token strategy

| Token | Lifetime | Storage | Notes |
|---|---|---|---|
| Access | 15 min | Memory (web) / SecureStore (mobile) | JWT HS256. Claims: `sub`, `role`, `jti`, `exp`, `iat` |
| Refresh | 30 days | httpOnly SameSite=Strict cookie (web) / SecureStore (mobile) | Opaque 32 random bytes, **sha256-hashed** in DB, rotated every use |

**Refresh rotation with reuse detection.** Every refresh issues a new token and marks
the old one used. If a *used* token is presented again, it leaked — revoke the entire
token family and force re-authentication. Small amount of code; turns a silent
compromise into a detected one.

Never put PII in JWT claims. `sub` is the user ID. Nothing else about the person.

---

## 3. Schema

`services/api/db/migrations/000002_auth.up.sql`:

```sql
CREATE TYPE user_role AS ENUM ('user', 'astrologer', 'admin', 'super_admin');
CREATE TYPE auth_provider AS ENUM ('phone', 'email', 'google', 'apple');

CREATE TABLE users (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email          TEXT UNIQUE,
    email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    phone          TEXT UNIQUE,                      -- E.164
    phone_verified BOOLEAN NOT NULL DEFAULT FALSE,
    name           TEXT,
    gender         TEXT,
    role           user_role NOT NULL DEFAULT 'user',
    status         TEXT NOT NULL DEFAULT 'active',   -- active | suspended | deleted
    last_login_at  TIMESTAMPTZ,
    deletion_requested_at TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX users_phone_idx ON users (phone);
CREATE INDEX users_email_idx ON users (email);
CREATE INDEX users_deletion_idx ON users (deletion_requested_at)
    WHERE deletion_requested_at IS NOT NULL;

CREATE TABLE user_preferences (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                  UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    preferred_language       TEXT NOT NULL DEFAULT 'en',
    astrology_system         TEXT NOT NULL DEFAULT 'vedic',
    chart_style              TEXT NOT NULL DEFAULT 'north',   -- Phase 3 reads this
    theme                    TEXT NOT NULL DEFAULT 'dark',
    notification_preferences JSONB NOT NULL DEFAULT '{}',
    communication_preferences JSONB NOT NULL DEFAULT '{}'
);

CREATE TABLE auth_identities (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider         auth_provider NOT NULL,
    provider_user_id TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_user_id)
);
CREATE INDEX auth_identities_user_idx ON auth_identities (user_id);

CREATE TABLE sessions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    family_id    UUID NOT NULL,              -- rotation lineage
    refresh_hash BYTEA NOT NULL,             -- sha256. NEVER the token.
    user_agent   TEXT,
    ip_hash      BYTEA,                      -- hashed; an IP is PII
    expires_at   TIMESTAMPTZ NOT NULL,
    used_at      TIMESTAMPTZ,                -- set on rotation; re-presented ⇒ leak
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_idx    ON sessions (user_id);
CREATE INDEX sessions_family_idx  ON sessions (family_id);
CREATE UNIQUE INDEX sessions_hash_idx ON sessions (refresh_hash);

CREATE TABLE audit_logs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID,
    action     TEXT NOT NULL,
    metadata   JSONB NOT NULL DEFAULT '{}',  -- NEVER PII. IDs and enums only.
    ip_hash    BYTEA,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_user_idx   ON audit_logs (user_id);
CREATE INDEX audit_logs_action_idx ON audit_logs (action, created_at DESC);
```

OTP codes live in **Redis only**, never Postgres: `otp:{channel}:{identifier}` →
`{hash, attempts}`, TTL 300s. They are short-lived secrets; persisting them creates a
breach liability for no benefit.

### Representative `sqlc` queries

```sql
-- name: RotateRefreshToken :one
-- Atomically marks the presented token used and returns it, only if unused.
UPDATE sessions
SET used_at = now()
WHERE refresh_hash = $1
  AND used_at IS NULL
  AND revoked_at IS NULL
  AND expires_at > now()
RETURNING *;

-- name: RevokeTokenFamily :exec
UPDATE sessions SET revoked_at = now()
WHERE family_id = $1 AND revoked_at IS NULL;

-- name: HardDeleteUser :exec
DELETE FROM users WHERE id = $1;
```

`RotateRefreshToken` doing the check and the update in one statement is what makes
rotation safe under concurrency. Two simultaneous refreshes with the same token:
exactly one gets a row back, the other gets `pgx.ErrNoRows` → treated as reuse →
family revoked. No application-level lock needed.

---

## 4. API surface

| Method | Path | Auth | Notes |
|---|---|---|---|
| `POST` | `/api/v1/auth/otp/request` | — | `{ channel, identifier }` |
| `POST` | `/api/v1/auth/otp/verify` | — | → tokens + `is_new_user` |
| `POST` | `/api/v1/auth/refresh` | refresh | rotates; reuse ⇒ family revoked |
| `POST` | `/api/v1/auth/logout` | access | revokes current session |
| `POST` | `/api/v1/auth/logout-all` | access | revokes every session |
| `GET` | `/api/v1/users/me` | access | |
| `PATCH` | `/api/v1/users/me` | access | name, gender only |
| `GET` | `/api/v1/users/me/preferences` | access | |
| `PATCH` | `/api/v1/users/me/preferences` | access | |
| `GET` | `/api/v1/users/me/sessions` | access | device list |
| `DELETE` | `/api/v1/users/me/sessions/{id}` | access | revoke one device |
| `POST` | `/api/v1/users/me/delete` | access + fresh OTP | two-step deletion |
| `GET` | `/api/v1/users/me/export` | access + fresh OTP | data portability (JSON) |

### Enumeration resistance

`/auth/otp/request` returns the **same** body and the **same** timing whether or not
the identifier exists. Otherwise it becomes a free "is this number registered?"
oracle — a real privacy leak for a product where the answer is "this person consults
astrologers."

In Go, equalise timing explicitly rather than hoping the code paths match:

```go
start := time.Now()
defer func() {
    if d := minResponseTime - time.Since(start); d > 0 {
        time.Sleep(d)
    }
}()
```

---

## 5. Rate limiting

Non-optional on auth. Redis sliding window, implemented once in
`platform/redis/ratelimit` and applied as chi middleware.

| Endpoint | Limit |
|---|---|
| `otp/request` per identifier | 3 / 15 min, 10 / day |
| `otp/request` per IP | 10 / 15 min |
| `otp/verify` per identifier | 5 attempts per code, then the code is burned |
| `refresh` per session | 60 / hour |
| Global per IP | 100 / min |

Exponential backoff on repeated OTP requests: 30s → 60s → 300s. Surface the wait as a
countdown in the UI, not a bare error. Responses carry `Retry-After`.

Implement the window with a Lua script so check-and-increment is atomic — a
read-then-write in Go is a race under load, and rate limiters are exactly where load
shows up.

---

## 6. UI features

### Screens

| Screen | Route | Contents |
|---|---|---|
| Landing | `/` | Hero, value proposition, "Get your free Kundli" CTA, how-it-works, FAQ |
| Login / Signup | `/auth` | Single combined flow — no separate signup page |
| OTP entry | `/auth/verify` | 6-digit input, resend countdown, change-number link |
| Onboarding name | `/onboarding/name` | Name + language; birth details are Phase 2 |
| Home (placeholder) | `/home` | Authenticated shell — filled in Phase 3 |
| Profile | `/settings/profile` | Name, gender, phone/email, verification badges |
| Preferences | `/settings/preferences` | Language, astrology system, chart style, theme, notifications |
| Sessions | `/settings/sessions` | Device list with revoke |
| Delete account | `/settings/delete` | Two-step, typed confirmation, clear consequences |

### Interaction details that matter

**OTP input.** Six boxes, auto-advance, paste-the-whole-code support,
`autocomplete="one-time-code"` so iOS and Android offer the SMS code from the keyboard.
Getting this wrong costs real conversion at the very top of the funnel.

**Resend.** Disabled with a visible countdown. After three sends, offer "Try email
instead" rather than just blocking.

**Single combined auth flow.** Never make a user guess whether they already have an
account. One field, one button: "Continue". Branch internally.

### Auth screen wireframe

```
┌──────────────────────────────────────┐
│              ✦                       │
│                                      │
│      Welcome to <Product>            │
│   Your personal AI astrologer        │
│                                      │
│   ┌──────────────────────────────┐   │
│   │ 🇮🇳 +91 │ 98765 43210        │   │
│   └──────────────────────────────┘   │
│                                      │
│   ┌──────────────────────────────┐   │
│   │        Continue              │   │
│   └──────────────────────────────┘   │
│                                      │
│   ──────────  or  ──────────         │
│                                      │
│   [ Continue with Apple  ]           │
│   [ Use email instead    ]           │
│                                      │
│   By continuing you agree to the     │
│   Terms and Privacy Policy.          │
└──────────────────────────────────────┘
```

### Delete-account screen

Deletion must be unambiguous, because it is irreversible:

```
┌──────────────────────────────────────┐
│  Delete your account                 │
│                                      │
│  This permanently deletes:           │
│   • Your profile and login           │
│   • Your birth details and charts    │
│   • All conversations and memories   │
│   • Your reports and saved readings  │
│                                      │
│  This cannot be undone.              │
│                                      │
│  Type DELETE to confirm:             │
│  ┌────────────────────────────────┐  │
│  │                                │  │
│  └────────────────────────────────┘  │
│                                      │
│  [ Download my data first ]          │
│  [ Delete my account permanently ]   │
└──────────────────────────────────────┘
```

Step 2 requires a fresh OTP. Then a 7-day grace window with `status=deleted` and login
blocked, after which an `asynq` worker performs hard deletion. The grace window
protects against a compromised session deleting someone's account; the hard delete is
what makes the promise real.

---

## 7. Free tooling for this phase

| Need | Dev (free) | Prod |
|---|---|---|
| SMS OTP | `ConsoleChannel` — `slog.Info("otp.dev", "identifier", id, "code", code)` | MSG91 / Twilio / AWS SNS — SMS is the one place with no free tier |
| Email OTP | Mailpit at `localhost:8025` | Resend or Brevo free tier (~3k/mo) covers early production |
| Social sign-in | Google is free; Apple needs $99/yr | Both deferred to Phase 10 — see §14 |

**Build email OTP first.** It is free end to end, in dev and in early production, and
it exercises the identical code path as phone OTP through the channel interface.

```go
type AuthChannel interface {
    ID() string
    Send(ctx context.Context, identifier, code, locale string) error
}
```

Selected by `AUTH_CHANNEL`. A startup assertion refuses `console` when
`ENV == "production"` — same fail-loud pattern used for the LLM provider in Phase 4.

```go
if cfg.Env == "production" && cfg.AuthChannel == "console" {
    return fmt.Errorf("refusing to start: AUTH_CHANNEL=console in production would print OTPs to logs")
}
```

---

## 8. Environment variables added

```bash
JWT_SECRET=                        # 32+ random bytes; required, no default
JWT_ACCESS_TTL=15m
JWT_REFRESH_TTL=720h

AUTH_CHANNEL=console               # console | smtp | sms
SMTP_HOST=localhost
SMTP_PORT=1025
SMTP_FROM="AI Astrology <noreply@localhost>"

GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=
APPLE_CLIENT_ID=
APPLE_TEAM_ID=
APPLE_KEY_ID=
APPLE_PRIVATE_KEY=

OTP_LENGTH=6
OTP_TTL=5m
OTP_MAX_ATTEMPTS=5

IP_HASH_SALT=                      # required — raw IPs never stored
ACCOUNT_DELETE_GRACE=168h          # 7 days
```

`JWT_SECRET` and `IP_HASH_SALT` have no defaults and no fallbacks. Missing values must
crash the process at startup with a named error.

---

## 9. Task list

| # | Task | Done when |
|---|---|---|
| 1.1 | Migration + sqlc queries | `task migrate && task sqlc` succeed; generated code compiles |
| 1.2 | `AuthChannel` interface + Console + SMTP | OTP logs in dev; lands in Mailpit with SMTP |
| 1.3 | OTP generate (`crypto/rand`), hash, store in Redis, attempt limits | Wrong code 5× burns it; correct code works once |
| 1.4 | JWT issue/verify; refresh rotation via the atomic SQL above | Replaying a used token revokes the family |
| 1.5 | Rate limit middleware with an atomic Lua window | 4th OTP request in 15 min → 429 with `Retry-After` |
| 1.6 | `Authenticate` + `RequireRole` middleware | A `user` token gets 403 on an admin route |
| 1.9 | Users handlers: me, patch, preferences | |
| 1.10 | Sessions list + revoke | Revoking invalidates that device's refresh immediately |
| 1.11 | Deletion: two-step, grace window, `asynq` hard-delete worker | After grace, no row anywhere references the user |
| 1.12 | Data export endpoint | Complete JSON of everything held |
| 1.13 | Audit logging on auth events | IDs and actions only; zero PII |
| 1.14 | i18n scaffolding, en + hi | Language switch changes copy; no hardcoded strings |
| 1.15 | All 9 screens with loading/error/empty states | |
| 1.16 | Analytics events wired | |

---

## 10. Testing

### Unit
- OTP hashing and `subtle.ConstantTimeCompare`
- JWT claim construction — asserts **no PII in claims**
- Rate-limit window arithmetic
- Refresh rotation state machine (fresh / used / revoked / expired)
- Role guard matrix

### Integration (testcontainers — real Postgres + Redis)
- Full OTP round trip on both channels
- Refresh rotation happy path
- **Refresh reuse → family revoked** — the test that catches real bugs
- **Concurrent refresh with the same token** — run with `-race`; exactly one succeeds, the other triggers family revocation
- Rate limit returns 429 with correct `Retry-After`
- Deletion cascades: after hard delete, no orphan rows in any table
- Enumeration: body and latency identical for existing vs unknown identifier

Testcontainers rather than mocks matters here: the rotation safety property lives in a
SQL `UPDATE … WHERE used_at IS NULL`, and a mocked database cannot test it.

### E2E
```
New user:     landing → phone → OTP → name → /home
Returning:    landing → phone → OTP → /home  (no onboarding)
Sessions:     log in on two "devices" → revoke one → that one gets 401
Deletion:     settings → delete → OTP → login blocked
```

---

## 11. Security checklist

- [ ] No secrets in code; `JWT_SECRET` ≥32 bytes, required at startup
- [ ] OTP from `crypto/rand`, compared in constant time, stored hashed, single-use, TTL enforced
- [ ] Refresh tokens stored as sha256 hashes, never plaintext
- [ ] Refresh rotation with reuse detection ⇒ family revocation, proven under concurrency
- [ ] Rate limiting on **every** auth endpoint, per identifier and per IP, atomic
- [ ] Enumeration-resistant responses and timing
- [ ] All input validated at the boundary; unknown fields rejected (allowlist)
- [ ] Parameterized queries only — `sqlc` gives this by construction; no `fmt.Sprintf` into SQL anywhere
- [ ] Authn + authz asserted on every non-public route; tested, not assumed
- [ ] No PII in JWT claims, logs, audit metadata or analytics
- [ ] IPs stored hashed with a salt; raw IPs never persisted
- [ ] Refresh cookie: `HttpOnly`, `Secure`, `SameSite=Strict`
- [ ] Generic client errors; details server-side only
- [ ] Account deletion genuinely deletes — verified by an integration test
- [ ] `AUTH_CHANNEL=console` refused in production
- [ ] Security headers + CSP on web

---

## 12. Analytics events

```
signup_started            { channel }
otp_requested             { channel }
otp_verified              { channel, attempts }
signup_completed          { channel, user_id }
login_completed           { channel, user_id }
onboarding_name_completed { user_id }
preferences_updated       { fields: []string }
session_revoked           { user_id }
account_deletion_requested{ user_id }
account_deleted           { user_id }
```

IDs and enums. Never a phone number, email or name — the same rule as logs, applied to
the analytics pipeline.

---

## 13. Definition of Done

Global DoD (root README) **plus**:

- [ ] A new user goes from landing page to authenticated `/home` in under 60 seconds
- [ ] Every auth endpoint is rate limited with a usable `Retry-After`
- [ ] Refresh reuse detection proven, including under concurrent requests with `-race`
- [ ] Account deletion proven to leave no residue by an integration test
- [ ] Zero PII in logs, JWT claims, audit metadata and analytics
- [ ] All UI strings come from locale files

---

## 14. Risks

| Risk | Mitigation |
|---|---|
| SMS has no free tier | Ship email OTP first; the channel interface makes SMS an isolated addition |
| Social sign-in is the lowest-friction signup path | **Deliberately deferred to Phase 10**, by the owner's decision: keep sign-in simple until the product exists, then add it. Email OTP needs no third party and works today, so nothing is blocked. The conversion cost is real and accepted. Google OAuth *was* built and fully tested in PR 8c and removed in PR 9a — restoring it is a revert of that commit, not a rewrite. |
| Hand-rolled auth is a classic vulnerability source | Every item in §11 is tested, not just reviewed. If the team would rather not own this, an ADR switching to a managed provider is legitimate — write it before building, not after. |
| Deletion cascade misses a table added later | Every phase that adds a user-owned table extends the deletion integration test. This is a line item in each subsequent phase gate. |
| Rate limiter race under load | Atomic Lua script, tested concurrently with `-race` |

---

## 15. Phase Gate 🔒

- [ ] Email OTP works end to end against Mailpit
- [ ] Phone OTP works end to end via `ConsoleChannel`
- [ ] Access + refresh tokens issue, rotate and revoke correctly
- [ ] **Refresh reuse revokes the token family — integration test green**
- [ ] **Concurrent refresh handled correctly under `-race`**
- [ ] Rate limits enforced and tested on every auth endpoint
- [ ] RBAC enforced; a `user` cannot reach an `admin` route
- [ ] Profile, preferences and session management all work
- [ ] Account deletion + hard-delete worker verified to leave no residue
- [ ] Data export returns complete user data
- [ ] Enumeration-resistance verified (identical body and timing)
- [ ] Zero PII in logs, claims, audit rows and analytics
- [ ] `AUTH_CHANNEL=console` refused when `ENV=production`
- [ ] i18n scaffolding in place; no hardcoded UI strings
- [ ] `task verify` green, including `go test -race`
- [ ] `docs/PROJECT_STATUS.md` and `.claude/state/current-phase.md` updated
