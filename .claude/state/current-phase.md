# Current phase

```
Phase: 0 — Foundation
Gate:  ✅ CLOSED  (19/20; item 2 blocked by a hardware fault, documented)

All tasks complete. See docs/PROJECT_STATUS.md for the full gate table
and the hardware-fault writeup.
```

## Next: Phase 1 — Authentication & User Profiles

Spec: `docs/specs/PHASE-01-AUTH-AND-USERS.md`

Entirely a Go phase. Neither Python service is touched.

**Decide first:** managed auth provider versus hand-rolled. Hand-rolled auth is a
classic source of vulnerabilities. If the team would rather not own it, write the ADR
before building — not after.

**Do not start until** you have read the Phase 1 spec end to end. The task list, data
model and API surface are already written; do not redesign them ad hoc.

**Carried forward into Phase 1:**
- The first domain migration (`users`, `sessions`, `auth_identities`, `audit_logs`)
- A trusted-proxy-aware client-IP resolver — chi's `middleware.RealIP` was deliberately
  not used because it is spoofable, and Phase 1 rate-limits per IP
- Extending the account-deletion integration test as each new user-owned table lands
