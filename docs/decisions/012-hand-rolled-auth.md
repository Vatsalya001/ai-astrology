# ADR-012 — Authentication is built in-house, not bought

**Status:** accepted · 2026-09-22
**Recorded late.** The code shipped in Phase 1; Phase 3 carried "no ADR for hand-rolled
auth" as an open item and this closes it. The decision is described as it was actually
made, not reconstructed to look tidier.

## Decision

`services/api/internal/auth` implements authentication directly: passwordless OTP over
email or SMS, short-lived **HS256 JWT** access tokens, and opaque **rotating refresh
tokens** held in Postgres with reuse detection. No Auth0, Clerk, Supabase Auth,
Firebase, Keycloak or Ory.

## Context

The product is an astrology companion for an Indian audience. Two constraints shaped
this before any vendor comparison:

- **Phone-first.** A large part of the audience signs in with a mobile number and an
  OTP, not an email and a password. Hosted providers price SMS aggressively and
  several route it through their own aggregator, which in India means worse
  deliverability than a local provider and no control over the DLT template registration
  that TRAI requires.
- **Birth data is the account.** Birth date, time and place together approach a unique
  identifier — `.claude/rules/security.md` treats the combination as PII of the same
  class as an email address. A hosted identity provider means that linkage lives in
  somebody else's tenancy.

## Reason

- **The surface is genuinely small.** OTP issue/verify, token issue, refresh rotation,
  session revocation. It is not password reset, social login, MFA enrolment, SCIM or
  enterprise SSO — the things that make rolling your own a bad idea.
- **The expensive parts are not the auth.** Rate limiting, audit logging, IP hashing and
  deletion-residue guarantees had to exist for the rest of the product regardless. A
  hosted provider would not have removed them; it would have split them across two
  systems.
- **Deletion is a first-class requirement.** `TestHardDeleteLeavesNoResidue` walks every
  user-owned table and refuses to pass until a new one is seeded and proven to be
  cleared. That guarantee is only enforceable over data we hold.

## What this obliges us to get right, and where each is enforced

| Obligation | Enforced by |
|---|---|
| Refresh reuse revokes the whole family | `ErrRefreshReused`, and the rotation tests |
| A signing secret cannot be short | `NewIssuer` refuses < 32 bytes, *and* config validation — belt and braces, so constructing an `Issuer` directly cannot bypass it |
| OTP brute force is bounded | `OTPRequestPerIdentifier` 3/15min, `OTPVerifyPerIdentifier` 10/15min, plus a per-IP rule sized for carrier-grade NAT |
| A wrong guess does not extend the window | `otpstore.go` preserves the remaining TTL deliberately |
| Invalid, expired and revoked are indistinguishable | One `ErrRefreshInvalid` for all three, so a probe learns nothing |
| Cross-user access returns 404, not 403 | A 403 confirms the resource exists |

## Tradeoffs

- **We own the vulnerability surface.** A flaw in token handling is ours to find; there
  is no vendor security team watching it. Mitigated only by the negative-case testing
  this repo requires — every guard here has a test that proves it *fires*.
- **No free upgrades.** Passkeys, device management and step-up auth arrive when we
  build them.
- **HS256 is symmetric**, so every service that verifies a token could also mint one.
  Acceptable while `api-service` is the only verifier; it stops being acceptable the day
  a second service needs to verify independently, and the migration then is to RS256 or
  EdDSA rather than to a vendor.

## Revisit when

- A second service needs to verify tokens without being able to issue them → asymmetric
  signing, not necessarily a provider.
- Enterprise SSO, SCIM or compliance attestations are required → the surface stops being
  small and the calculation inverts.
- Phase 8's astrologer marketplace introduces a second, differently-privileged account
  type. That is the point at which role handling grows beyond `RequireRole` comparing
  strings exactly, and it is worth re-testing this decision then rather than assuming it.
