# Phase 1 — gate evidence

Every line below was produced by running something, not by reading code. Where a
guard is claimed, it was deliberately broken first and the failure recorded — a
guard that has never been observed to fire is a guard you cannot trust.

Reproduce the live checks with the stack up:

```bash
./scripts/ayana up          # brings up six dependencies, applies migrations, starts all four services
./scripts/ayana status      # what is listening, and on what
npx playwright test         # 55 browser specs against the running stack
```

---

## §15 Phase Gate

| # | Item | Evidence |
|---|---|---|
| 1 | Email OTP works end to end against Mailpit | `AUTH_CHANNEL=smtp`, real SMTP to Mailpit: message delivered (`subject: Your Ayana code: 253142`, `from: noreply@localhost`), code extracted from the email body and posted to `/auth/otp/verify` → `access_token` issued, `is_new_user: true`. Not the console channel. |
| 2 | Phone OTP works end to end via `ConsoleChannel` | `tests/e2e/flows.spec.ts` → *a phone number signs up and lands on onboarding*. E.164 in, code read from the delivery channel, onboarding reached. |
| 3 | Google OAuth completes and links an identity | **Open — by decision, not omission.** No credentials exist. The whole flow is covered against a stubbed Google with a real Redis (`oauth_integration_test.go`): round trip, forged/replayed/expired state, 16-way concurrent exchange with exactly 1 winner, unverified email refused. Turning it on is a `.env` edit. See "What is deliberately open" below. |
| 4 | Access + refresh tokens issue, rotate and revoke | `rotation_integration_test.go` against real Postgres; `token_test.go` for issue/verify, `alg: none`, wrong signature, foreign issuer, non-UUID subject, expiry. |
| 5 | **Refresh reuse revokes the token family** | `rotation_integration_test.go`. Proven by breaking it: without `AND used_at IS NULL`, 32 of 32 concurrent rotations succeeded. |
| 6 | **Concurrent refresh handled under `-race`** | Same file, `-race`, exactly one winner; the loser triggers family revocation. |
| 7 | Rate limits enforced and tested on every auth endpoint | See §11 item 5 below. Three holes were found by measuring and closed in PR 8d. |
| 8 | RBAC enforced; a `user` cannot reach an `admin` route | `middleware_test.go` → `TestRequireRoleMatrix`, plus `TestRequireRoleWithoutAuthenticateIs401` — the case where the guard is mounted without authentication in front of it. 7 passing cases. |
| 9 | Profile, preferences and session management work | `settings.spec.ts`, 9 specs, driven through a real signup. |
| 10 | Account deletion + hard-delete worker leave no residue | `TestHardDeleteLeavesNoResidue`, `TestWorkerRespectsTheGraceWindow` against real Postgres. |
| 11 | Data export returns complete user data | `deletion_integration_test.go`. The `Identities` field was declared and never populated until a test read it. |
| 12 | Enumeration-resistance verified (identical body **and** timing) | Live: existing account `{"sent":true,"expires_in_seconds":300}` in **254.5 ms**; unknown address, byte-identical body, **253.9 ms**. |
| 13 | Zero PII in logs, claims, audit rows and analytics | `TestAccessTokenClaimsContainNoPII`, `TestAuditEventsCarryNoIdentifier`, `logging_test.go`, and eight PII-shaped analytics properties each asserted refused. Live: the analytics lines for a full signup contain neither the test address nor the name. |
| 14 | `AUTH_CHANNEL=console` refused when `ENV=production` | `TestProductionRefusesConsoleAuthChannel`. |
| 15 | i18n scaffolding; no hardcoded UI strings | `en` + `hi` dictionaries, `hi` structurally required to satisfy `Dictionary`. `settings.spec.ts` → *changing the language changes the interface*. |
| 16 | `task verify` green, including `go test -race` | Green. Unit and integration suites both run with `-race`. |
| 17 | `PROJECT_STATUS.md` and `current-phase.md` updated | This document, plus both files. |

---

## §11 Security checklist

| # | Item | Evidence |
|---|---|---|
| 1 | No secrets in code; `JWT_SECRET` ≥32 bytes, required at startup | `TestJWTSecretIsRequiredAndLongEnough`. `gitleaks` runs pre-commit and in CI. |
| 2 | OTP from `crypto/rand`, constant-time compare, stored hashed, single-use, TTL | `otp_test.go`, `otpstore_integration_test.go`. Proven by breaking it: without the Lua script, 6 of 24 concurrent verifications of one code succeeded. |
| 3 | Refresh tokens stored as sha256 hashes | `TestHashRefreshTokenDoesNotEmbedTheToken`. |
| 4 | Rotation with reuse detection ⇒ family revocation, under concurrency | Gate items 5–6 above. |
| 5 | Rate limiting on **every** auth endpoint, per identifier and per IP, atomic | Route-specific: `otp/request` (3/15min per identifier, 10/day, 30/15min per IP), `otp/verify` (10/15min), `refresh` (60/hr per session), `oauth/google` (20/15min per IP), `users/me/challenge` (3/15min **per user**). Everything else — `logout`, `providers`, `users/me`, `users/me/preferences`, `users/me/sessions` — is covered by the per-IP backstop on `/api/v1`, which is what makes this true by construction rather than by remembering. All windows are a Lua sorted-set sliding window, tested concurrently. |
| 6 | Enumeration-resistant responses and timing | Gate item 12. |
| 7 | Input validated at the boundary; unknown fields rejected | `DisallowUnknownFields` on both JSON decoders; allowlists for gender, language, system, style, theme. |
| 8 | Parameterized queries only; no `fmt.Sprintf` into SQL | `sqlc` by construction. A grep for `Sprintf` near SQL keywords across `internal/` returns nothing. |
| 9 | AuthN + AuthZ on every non-public route; tested | Gate item 8. Cross-user access returns **404**, not 403 — `RevokeSession` was `:exec` and answered 204 while revoking nothing, until a test read the row count. |
| 10 | No PII in JWT claims, logs, audit metadata or analytics | Gate item 13. |
| 11 | IPs stored hashed with a salt; raw IPs never persisted | `TestHashIP`, `TestHashIPDependsOnTheSalt`, and `TestGlobalThrottleStoresNoRawAddress` — the limiter's Redis keys are hashed too, because a Redis key is persistence. |
| 12 | Refresh cookie `HttpOnly`, `Secure`, `SameSite=Strict` | Live: `Set-Cookie: ayana_refresh=; Path=/api/v1/auth; HttpOnly; SameSite=Strict`. `Secure` is `!IsDevelopment()`, absent here because local development is plain HTTP. |
| 13 | Generic client errors; detail server-side only | Every OAuth callback failure — forged state, bad code, unverified email, unconfigured provider — returns an identical 401. The token endpoint's body is never echoed: it can carry the client secret. |
| 14 | Account deletion genuinely deletes | Gate item 10. |
| 15 | OAuth `state` validated | 12 passing OAuth specs. State is random, stored **hashed** in Redis with a 10-minute TTL, consumed with `GETDEL`; 16 concurrent exchanges of one state yield exactly 1 winner. |
| 16 | `AUTH_CHANNEL=console` refused in production | Gate item 14. |
| 17 | Security headers + CSP on web | Live on every page: `Content-Security-Policy`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy`, `Permissions-Policy`. `tests/e2e/csp.spec.ts` asserts the policy **and** that the app still hydrates under it. **`script-src` carries `'unsafe-inline'`** — see below. |

### The CSP concession, stated plainly

`script-src 'unsafe-inline'` is the weakest part of the policy and is a dated
decision rather than an oversight.

Next's App Router streams its RSC payload through inline `self.__next_f.push(...)`
scripts. Under `script-src 'self'` those are blocked and the app never hydrates —
measured, not assumed: `csp.spec.ts` failed on a click timeout with the strict
policy in place, on a page that rendered perfectly.

The two alternatives both cost something real. A per-request nonce forces **every**
page into dynamic rendering, ending static optimisation and CDN caching for an
audience on Indian mobile networks. `experimental.sri` keeps static rendering and is
strict, but it is experimental and this repo requires an ADR before adopting new
technology.

What makes it acceptable *today* is that the app renders no untrusted content at
all — its own strings and the user's own name, through React, which escapes by
default. That changes in Phase 5, when the chat renders model output. **PHASE-05
now carries a blocking gate item** to replace this before the first model response
is rendered, in both §12 and §16, because a comment is not a plan.

The rest of the policy is not decoration: `connect-src` is what turns a successful
injection into a dead end, and `object-src 'none'` / `base-uri 'self'` /
`form-action 'self'` close three standard bypass routes.

---

## §13 Definition of Done

| Item | Evidence |
|---|---|
| Landing page to authenticated `/home` in under 60 seconds | `auth.spec.ts` → *landing → auth → code → onboarding → home*, consistently under 5 s in CI. |
| Every auth endpoint rate limited with a usable `Retry-After` | §11 item 5. Live: the 4th challenge in a window returns `429` with `Retry-After: 899`. |
| Refresh reuse detection proven, including concurrently with `-race` | Gate items 5–6. |
| Account deletion proven to leave no residue | Gate item 10. |
| Zero PII in logs, claims, audit metadata and analytics | Gate item 13. |
| All UI strings from locale files | Gate item 15. |

---

## §10 test flows

| Flow | Where |
|---|---|
| New user: landing → identifier → OTP → name → `/home` | `auth.spec.ts` |
| Returning: landing → identifier → OTP → `/home`, no onboarding | `auth.spec.ts` |
| Two devices → revoke one → **that one** gets 401 | `flows.spec.ts` |
| Deletion → OTP → every session dead, refresh refused | `flows.spec.ts` |

The device flow uses two browser **contexts**, not two tabs. Two tabs share a cookie
jar and the test would pass whether or not revocation worked.

---

## Guards proven by breaking them

Each of these was reverted immediately after the failure was recorded.

| Guard removed | Observed failure |
|---|---|
| `AND used_at IS NULL` from `RotateRefreshToken` | 32 of 32 concurrent rotations succeeded |
| `AND revoked_at IS NULL` from `RotateRefreshToken` | a revoked device kept minting tokens — `/auth/refresh` returned **200** where the test expects 401, and the revoked browser stayed on `/home` |
| the OTP verify Lua script | 6 of 24 concurrent verifications of one code succeeded; the code survived 40 concurrent wrong guesses |
| `safeReturnTo` from `OAuthStart` | `an off-site return_to reached the provider as "https://evil.example"` |
| `PExpire` → `Expire` in the state-expiry test | the test passed **spuriously** — go-redis rounds a sub-second duration up to a full second |
| `bg-surface` → `bg-surface-raised` | computed background `rgba(0, 0, 0, 0)`. Tailwind drops an unknown colour class silently, `tsc` sees a valid string and `next build` succeeds |

---

## Found only by running it

Nothing in this list was visible by reading the code.

- **`POST /users/me/challenge` was completely unlimited.** 25 of 25 consecutive calls
  returned 200 — 25 emails to whoever owns the account, triggerable by anyone holding
  an access token.
- **`GlobalPerIP` was declared "applies to every request regardless of route" and was
  wired to nothing.**
- **The spec's 100/minute backstop was wrong.** Six concurrent browser sessions
  generated 105 counted requests in 15 seconds — 420/minute from one address. Behind
  Indian carrier-grade NAT that would have broken entire carrier pools while
  throttling no attacker.
- **`go test -tags=integration` reported `ok` when Docker was unavailable**, skipping
  every test. The single-writer grants, reuse detection and the limiter under
  concurrency would all have gone unverified behind a passing check mark.
  `REQUIRE_CONTAINERS=1` in CI now turns that skip into a failure.
- **`retryable: true` on a permanently-unconfigured feature**, because the flag was
  derived from `status >= 500`. A well-behaved client would have retried forever.
- The SMTP envelope sender was rejected with `501 invalid FROM parameter`: `MAIL FROM`
  needs a bare address where the header needs the display-name form.
- `ConsoleChannel` printed `[REDACTED]` — the logger redacts `code`. Fixed by
  bypassing the logger, not by exempting the key.
- The device list showed **rotations, not devices**: 6 session rows, 1 family, 3 entries.
- `current` and `Identities` were each declared and never populated.
- `maxLength={6}` truncated a pasted `482-913` to five digits.
- `scripts/ayana` never applied migrations — invisible on any machine that had run
  `task migrate` by hand, caught by a clean CI runner.

---

## What is deliberately open

**Gate item 3 — "Google OAuth completes and links an identity."**

No Google credentials exist. This is the account owner's decision, recorded here so
the gate is not quietly ticked against a stub.

Everything up to the boundary with Google is covered and passing. The remaining step
is `.env`:

```
GOOGLE_CLIENT_ID=...
GOOGLE_CLIENT_SECRET=...
```

Authorised redirect URI: `http://localhost:4000/api/v1/auth/oauth/google/callback`.
`GET /api/v1/auth/providers` then reports `{"google":true}` and the button appears by
itself. With no credentials the route returns **501** with `OAUTH_NOT_CONFIGURED` —
not 503, because a deployment without credentials will still not have them in ten
seconds.

**ADR-003 — the Swiss Ephemeris licence.** Unrelated to this phase; due before Phase 7.
