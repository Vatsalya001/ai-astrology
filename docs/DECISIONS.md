# Architecture Decision Records

Every significant technical choice gets an ADR. The point is not ceremony — it is that
six months from now, someone (possibly you) will ask "why is it like this?" and the
answer should be written down, including the options rejected and what they cost.

**Format:** Decision · Context · Options considered · Reason · Tradeoffs · Status.

---

| # | Decision | Status |
|---|---|---|
| [001](decisions/001-service-topology.md) | Three services: Go API + Python astro + Python AI | ✅ accepted |
| [002](decisions/002-postgres-pgvector.md) | PostgreSQL with pgvector, not a dedicated vector DB | ✅ accepted |
| [003](decisions/003-astrology-engine.md) | **Astrology engine and its licence** | 🔴 **proposed — due before Phase 7** |
| [004](decisions/004-llm-provider-abstraction.md) | One provider protocol; free in dev, paid in prod | ✅ accepted |
| [005](decisions/005-http-json-not-grpc.md) | HTTP + JSON between services, not gRPC | ✅ accepted |
| [006](decisions/006-sqlc-not-orm.md) | sqlc, not an ORM | ✅ accepted |
| [007](decisions/007-go-toolchain-pin.md) | Pin the Go toolchain in go.mod | ✅ accepted |
| [008](decisions/008-npm-workspaces.md) | npm workspaces instead of pnpm | ✅ accepted |

---

## Open

**[ADR-003 — Astrology engine licence](decisions/003-astrology-engine.md)** is the only
unresolved architectural question.

Swiss Ephemeris is dual-licensed AGPL-3.0 or commercial. The AGPL network clause
requires offering the complete corresponding source to users of a networked
application, which is incompatible with closed-source SaaS. The alternatives are a
commercial licence from Astrodienst, or MIT-licensed `skyfield` with the Vedic layer
implemented directly — which is less additional work than it first appears, since that
layer is being written either way.

**This must be closed before Phase 7 (Monetization).** Once money changes hands,
an unresolved licence question stops being a task and becomes a liability. The Phase 2
gate carries a blocking checklist item for it.

---

## Upcoming decisions

Anticipated, not yet written:

| Topic | Phase | Why it will need a decision |
|---|---|---|
| Managed auth vs. hand-rolled | 1 | Hand-rolled auth is a classic vulnerability source. Decide before building, not after. |
| Deletion vs. financial retention | 7 | Phase 1 promises complete account deletion; tax law requires keeping transaction records. A real conflict needing an explicit, documented resolution. |
| In-app purchase strategy | 10 | 15–30% store commission is a pricing decision, not a technical one. Must be settled before store submission. |
| Splitting voice out of ai-service | 9/11 | CPU-bound STT alongside async LLM I/O risks GIL contention. The one service extraction already anticipated. |
