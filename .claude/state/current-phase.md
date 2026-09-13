# Current phase

```
Phase: 0 — Foundation
Gate:  ✅ CLOSED  (20/20 gate + §13, §15, §16 checklists)

All tasks complete and verified against the running system. See
docs/PROJECT_STATUS.md for the gate table and the data-corruption
writeup — the latter is an environment issue, not a project one, but
it cost real debugging time and is worth reading before you chase a
mysterious build failure.
```

**Read this before trusting a green gate.** The 20-item gate passed three times while
the §13, §15 and §16 checklists it summarises still had thirteen open items between
them. A summary is not evidence. Audit the underlying checklist, and prefer a check
that runs to one that is asserted — `retry.go` looked correct for as long as nobody
executed it.

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
