# Role: security

Threat review before any phase gate, and the hat to wear whenever a change touches
auth, money, PII or a service boundary.

## Before reviewing
Read `.claude/rules/security.md`.

## The review questions
1. **What new data does this handle, and is any of it PII?** Birth date + time + place
   in combination is close to a unique identifier. Treat it exactly like an email.
2. **Where does that data travel?** Specifically: can it reach a free model tier? The
   startup guard blocks that in production, but a new code path may route around it.
3. **What does this endpoint leak on failure?** Stack traces, field names, SQL, and the
   existence of another user's resource are all leaks. Cross-user access returns 404,
   not 403 — a 403 confirms the resource exists.
4. **Is authorisation asserted, and is it tested?** Not assumed. Tested.
5. **Is anything here attacker-controlled that ends up in a log, a cache key, or a
   query?** The trace header is capped at 64 chars for exactly this reason.

## Known traps in this codebase
- **Client IP is not trustworthy by default.** chi's `middleware.RealIP` was removed
  because it rewrites `RemoteAddr` from a spoofable header. Phase 1 rate-limits per IP,
  so a naive reinstatement would be a bypass.
- **Redaction cannot descend into Go structs.** `slog` redacts by key name only, so
  "log IDs, not objects" is load-bearing on the Go side. Python walks nested structures
  and is stricter.
- **Secret scanning is only as good as its allowlist.** If a test needs a
  credential-shaped value, use an obviously-fake literal rather than adding an
  allowlist entry.

## Before marking any phase gate closed
Walk the checklist at the end of `.claude/rules/security.md`. Every box, against the
running system.
