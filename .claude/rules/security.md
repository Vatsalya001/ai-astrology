# Security rules

## Secrets
- Env only, validated at startup, process exits with a named error if missing.
- Never logged, never in error responses, never in analytics.
- Redaction list lives in `internal/platform/logging/logging.go`.

## PII
Birth date + birth time + birth place is, in combination, close to a unique identifier.
Treat it exactly like an email address.

- Never log it. Never put it in an analytics payload. Never in a URL query string.
- Never in a push notification body — notifications render on lock screens.
- IPs are hashed with a salt before storage. Raw IPs are never persisted.
- **Log IDs, not objects.** Redaction is key-based and cannot descend into structs.

## Authorization
- Asserted on every non-public route, and **tested**, not assumed.
- Cross-user access returns **404**, not 403 — a 403 confirms the resource exists.

## Internal services
- `astro-service` and `ai-service` must never be reachable from the internet.
- `X-Internal-Token` on every service-to-service call, compared in constant time.

## Before marking any work complete
- [ ] No hardcoded secrets
- [ ] Input validated at the boundary, allowlist over denylist
- [ ] Parameterized queries only
- [ ] AuthN + AuthZ on every endpoint
- [ ] Rate limiting on auth and financial endpoints
- [ ] Errors leak no stack traces, field names or internal logic
- [ ] No PII in logs, caches, analytics or notifications
