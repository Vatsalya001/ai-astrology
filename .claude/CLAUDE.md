# Ayana — project constitution

Read this at the start of every session. It is short on purpose.

**Current phase: 2 — Astrology Engine.** Phases 0 and 1 are closed.
See `.claude/state/current-phase.md` for what carries forward.

---

## The four invariants

These are enforced by machinery. If you find yourself wanting to weaken one, the
answer is almost always to change the design instead.

### 1. Astrology is computed, never generated

An LLM must **never** produce a planetary position, house, nakshatra, dasha, degree,
transit or yoga. Those come from `astro-service`.

`astro-service` has no model SDK, no API key and no HTTP client.
**Never add one.** `services/astro/tests/test_no_llm_imports.py` will fail the build.

If a task seems to need a model inside `astro-service`, it belongs in `ai-service`.

### 2. One writer

Only `api-service` (Go) writes to PostgreSQL.
`ai-service` connects as `astro_ro` — `SELECT` only, enforced by Postgres grants.
`astro-service` has no database access at all.

When Python needs something persisted, it **returns** it and Go writes it.

### 3. No real user data reaches a free model tier

Free models in development and CI. Paid provider in production.
`services/ai/app/guards.py` makes the process refuse to boot otherwise.

Development and CI use synthetic fixtures only — never real birth data.

### 4. Money is an integer

`int64` paise in Go, `BIGINT` in Postgres, `int` in Python. **Never a float, anywhere.**
The ledger is append-only; corrections are new opposing entries, never edits.

---

## Working rules

- **One phase at a time.** Do not start Phase N+1 until the Phase N gate in
  `docs/specs/PHASE-NN-*.md` passes in full.
- **No new technology without an ADR** in `docs/decisions/`.
- **No secrets in code.** Everything from env, validated at startup, failing loudly.
- **Never hand-write a cross-service client.** Generate from OpenAPI.
- **Log IDs, not objects.** PII redaction is at the logger, but it cannot descend into
  arbitrary structs.
- Update `docs/PROJECT_STATUS.md` at the end of every task.

---

## Language boundaries

| Concern | Goes in |
|---|---|
| HTTP API, auth, billing, WebSocket, workers, **all DB writes** | `services/api` (Go) |
| Ephemeris, charts, dashas, yogas, compatibility maths | `services/astro` (Python) |
| LLM calls, prompts, RAG, safety, memory, evals, voice | `services/ai` (Python) |
| UI | `apps/web` (TypeScript) |

Go module boundaries: a domain package may import `platform/` and `httpapi/`, never
another domain's internals. Cross-domain access goes through an interface declared by
the **consumer**.

---

## Commands

```bash
task verify     # the commit gate: lint + test + build, all three languages
task up         # infrastructure
task dev:api    # etc.
task health     # aggregate health of the running stack
```

`go test` always runs with `-race`. This service does concurrent billing from Phase 7;
the race detector is how those bugs get found before users find them.

---

## Local environment notes

- Postgres is on **5433**, Redis on **6381** — the defaults are taken on this machine.
- Go toolchain is pinned in `go.mod` (ADR-007) because the system Go 1.22 stdlib has a
  corrupted byte. Do not remove the pin.
- npm workspaces, not pnpm (ADR-008).

---

## Definition of Done

Implementation · `go build`/`go vet` clean · `mypy --strict` clean · `tsc` clean ·
all linters pass · tests pass · error/loading/empty states handled · mobile responsive ·
accessibility considered · security reviewed · analytics events emitted · docs updated.

---

## Open decisions

- **[ADR-003](../docs/decisions/003-astrology-engine.md)** — Swiss Ephemeris licence.
  AGPL vs commercial vs MIT `skyfield`. **Must be closed before Phase 7.**
