# Phase 1 — implementation plan

**Risk tier:** 2 (authentication, first domain migration, PII).
**Mode:** `full_feature`.
**Decisions taken 2026-09-14:** hand-rolled auth per spec §2–§5 · Google OAuth
built but left unconfigured · Apple deferred to Phase 10.

---

## Why this is eight PRs and not one

The Change Contract limit is 15 files. Phase 1 is 5 tables, 15 endpoints,
9 screens and 16 tasks — several hundred files' worth of diff if taken at
once, and the review that matters would be buried.

More importantly, one PR here has a different risk profile from the rest.
**PR 3 (tokens) is where a subtle bug is both most likely and most
expensive**, and it is the one I want read on its own, against a diff
small enough to hold in your head.

| PR | Scope | Tasks | Risk |
|---|---|---|---|
| 1 | Migration + sqlc queries | 1.1 | **High** — irreversible in a way Phase 0 was not |
| 2 | OTP: generation, hashing, channels | 1.2, 1.3 | Medium |
| 3 | **Tokens: JWT, refresh rotation, reuse detection** | 1.4 | **Highest** |
| 4 | Middleware: authenticate, RBAC, rate limiting | 1.5, 1.6 | High |
| 5 | Users, preferences, sessions | 1.9, 1.10 | Low |
| 6 | Deletion, export, audit logging | 1.11, 1.12, 1.13 | **High** — the two data-ownership gate items |
| 7 | Google OAuth (stubbed) | 1.7 | Medium |
| 8 | The 9 screens + i18n + analytics | 1.14, 1.15, 1.16 | Low |

Each merges green before the next starts. `main` stays deployable
throughout.

---

## PR 1 — schema

Five tables, two enums, per spec §3. The parts that need deciding rather
than transcribing:

**Every migration needs a real `down`.** The project's database rules say
"revert the commit" is not a rollback plan. Dropping the enums has to
happen after the tables that reference them, and `DROP TYPE` fails if any
column still uses it — so `down` order is the reverse of `up`, and it is
tested by running it.

**`astro_ro` grants must extend to the new tables.** This is the part
most likely to be silently skipped. Phase 0's single-writer integration
test asserts `astro_ro` cannot write — but it enumerates tables, so as
the schema grows the invariant quietly stops being enforced for anything
new. The test is changed to discover tables from
`information_schema` rather than list them, so a table added in Phase 2
is covered without anyone remembering to add it.

**`sessions.refresh_hash` is `BYTEA`, never the token.** `sessions_hash_idx`
is `UNIQUE`, which is what makes `RotateRefreshToken` a single atomic
statement.

**`audit_logs.metadata` is `JSONB` and must never hold PII.** Enforced by
review and by a test asserting the emitter's allowlist; the column itself
cannot enforce it.

---

## PR 3 — the one to read closely

Refresh rotation with reuse detection. The property:

> Two concurrent refreshes with the same token: exactly one succeeds. The
> loser is treated as a reuse and revokes the whole family.

This works because the check and the update are one statement —
`UPDATE … WHERE used_at IS NULL … RETURNING *`. Postgres serialises it;
no application lock is involved. A mocked database cannot test this,
which is why the test uses testcontainers and runs under `-race`.

Failure mode if wrong: a leaked refresh token stays valid indefinitely
and nothing detects it. That is a silent compromise, which is why this PR
is isolated.

---

## Testing strategy

Per spec §10, and matching the standard Phase 0 settled on — a guard that
has never been observed to fire is a guard we cannot trust.

| Layer | Covers |
|---|---|
| Go unit | OTP hashing, constant-time compare, JWT claims (**asserts no PII**), rate-limit arithmetic, rotation state machine, role matrix |
| Go integration (testcontainers) | OTP round trip, rotation, **reuse → family revoked**, **concurrent refresh under `-race`**, 429 + `Retry-After`, deletion leaves no orphans, enumeration parity |
| Component (Vitest) | OTP input paste/auto-advance, the 9 screens' loading/error/empty states |
| E2E (Playwright) | The four flows in §10: new user, returning user, two devices + revoke, deletion |
| a11y (axe) | Every new screen, same as Phase 0 |
| Contrast (unit) | Any new token, against every surface it renders on |

**Negative tests are the point.** Each of these asserts the unsafe thing
is refused, not that the happy path works:

- A wrong OTP five times burns the code
- A used refresh token revokes its family
- A `user` role gets 403 on an admin route
- `AUTH_CHANNEL=console` refuses to boot when `ENV=production`
- An unknown identifier and a known one are indistinguishable in body
  **and** timing
- A deleted user's rows are gone from every table, not flagged

---

## Security checklist (spec §11)

Carried into every PR, verified before the phase gate:

- `JWT_SECRET` ≥32 bytes, required, no default, named failure at startup
- `IP_HASH_SALT` likewise — raw IPs are never persisted
- OTP from `crypto/rand`, hashed at rest, single-use, TTL enforced
- Refresh tokens: opaque 32 bytes, sha256 at rest, rotated every use
- No PII in JWT claims, audit rows, analytics payloads or logs
- Rate limits on every auth endpoint, atomic via Lua
- Cross-user access returns **404**, never 403

---

## Carried forward from Phase 0

- The trusted-proxy-aware client-IP resolver. chi's `middleware.RealIP`
  was deliberately not used because it is spoofable, and Phase 1
  rate-limits per IP — a spoofable IP makes that limiter decorative.
- Extending the single-writer test as each new table lands (see PR 1).

---

## What stays open at the end

**Gate item: "Google OAuth completes and links an identity."** Provable
against a stub, not against real Google, until credentials exist. Flagged
now rather than ticked on inference.

**ADR-003** — Swiss Ephemeris licence. Unrelated to Phase 1, due before
Phase 7.
