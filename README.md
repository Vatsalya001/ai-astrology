# Antara — AI Astrology Companion

> **The AI doesn't just know astrology. It knows YOUR astrology.**

A personal AI astrologer that understands your birth chart, your life context and your
conversation history — and connects you to a human astrologer when you need one.

**Status: Phase 0 — Foundation.** No product features yet. The scaffolding, service
topology and safety invariants are in place and verified.

> `Antara` is a working name (Sanskrit: *inner*, and the root of *antardasha*).
> Easily changed — it appears only in `apps/web/src/components/Logo.tsx` and page metadata.

---

## Quick start

```bash
# 1. Prerequisites: Go 1.22+, Python 3.12+, Node 20+, Docker, uv, task
cp .env.example .env

# 2. Infrastructure
task up                 # Postgres :5433 · Redis :6381 · MinIO :9001 · Mailpit :8025

# 3. Run the stack (four terminals, or `task --parallel dev`)
task dev:api            # Go API          → :4000
task dev:astro          # Python astro    → :8100
task dev:ai             # Python ai       → :8200
task dev:web            # Next.js         → :3000

# 4. Verify
task health             # aggregate health of every dependency
open http://localhost:3000/status
```

`task` with no arguments lists everything available.

> **Port note:** Postgres and Redis are on **5433** and **6381**, not their defaults.
> This machine already runs instances on 5432 and 6379, and silently shadowing them
> would be worse than using non-standard ports.

---

## Architecture

Three backend services plus a web client. The split is deliberate and each language is
doing what it is genuinely best at.

```
                         ┌─────────────────────┐
                         │   web  (Next.js)    │
                         └──────────┬──────────┘
                                    │ HTTPS
                                    ▼
            ┌───────────────────────────────────────────────┐
            │           api-service   ·   Go                │
            │  Auth · RBAC · rate limiting · validation      │
            │  Users · charts · billing · consultations      │
            │  WebSocket hub · background workers            │
            │                                               │
            │  OWNS THE DATABASE. Sole writer.               │
            │  The only service reachable from the internet. │
            └───────┬───────────────────────────┬───────────┘
                    │ HTTP/JSON                 │ HTTP/JSON + SSE
                    ▼                           ▼
    ┌───────────────────────────┐  ┌────────────────────────────────┐
    │  astro-service · Python   │  │   ai-service · Python          │
    │  Swiss Ephemeris          │  │  LLM providers · prompts       │
    │  Charts · dashas · yogas  │  │  RAG · safety · memory · evals │
    │                           │  │                                │
    │  STATELESS. No database.  │  │  READ-ONLY database role.      │
    │  NO LLM CLIENT AT ALL.    │  │                                │
    └───────────────────────────┘  └────────────────────────────────┘
                    │                           │
                    └─────────────┬─────────────┘
                                  ▼
                    PostgreSQL + pgvector · Redis · S3
```

| Service | Language | Why |
|---|---|---|
| `api-service` | **Go** | High-concurrency work: WebSocket fan-out, per-30-second billing ticks under contention, connection handling. Single static binary. |
| `astro-service` | **Python** | `pyswisseph` is the mature binding to Swiss Ephemeris. Stateless and cacheable, so the GIL is irrelevant. |
| `ai-service` | **Python** | Every LLM, embedding and audio library is Python-first. The eval harness is pytest. |
| `web` | **TypeScript** | Next.js App Router. |

**The honest tradeoff:** three deployables instead of one is real operational cost.
It is mitigated by one database with one writer, one `docker compose up`, and one trace
ID propagated across all three. See [ADR-001](docs/decisions/001-service-topology.md).

---

## The four invariants

These are enforced by machinery, not by code review. Each has a test that proves it
fires.

### 1. Astrology is computed, never generated

An LLM must never produce a planetary position, house, nakshatra or dasha.
`astro-service` has **no** model SDK, **no** API key and **no** HTTP client. It is
structurally incapable of asking a model to do arithmetic.

```
services/astro/tests/test_no_llm_imports.py   ✓ 4 passing
```

### 2. One writer

Only `api-service` writes to Postgres. `ai-service` connects as `astro_ro`, a role with
`SELECT` and nothing else — enforced by Postgres grants, which cannot be forgotten
during a refactor.

```sql
astro_ro=> INSERT INTO guard_probe VALUES (99);
ERROR:  permission denied for table guard_probe      -- verified
```

### 3. No real user data reaches a free model tier

Birth date + time + place is close to a unique identifier, and conversations cover
health, marriage and money. Most free API tiers may train on inputs. So: free models in
development, paid provider in production — enforced by the process refusing to boot.

```
$ ENV=production LLM_PROVIDER_TIER=local uvicorn app.main:app
UnsafeConfigurationError: Refusing to start: LLM provider "openai-compatible"
is tier "local". Production requires a paid provider with a no-training commitment...
```

```
services/ai/tests/test_guards.py              ✓ 16 passing
```

### 4. Degraded, never down

A non-critical service failing degrades the system rather than downing it. With
`astro-service` stopped, `/health` returns **HTTP 200 / `degraded`**, not a 503 — cached
charts still render, and everything except chat still works without `ai-service`.

---

## Repository layout

```
apps/web/              Next.js — user-facing
services/api/          Go — HTTP API, workers, sole DB writer
services/astro/        Python — deterministic ephemeris, stateless, AI-free
services/ai/           Python — LLM orchestration, read-only DB
packages/              Shared TypeScript
contracts/             Generated cross-service OpenAPI clients
infrastructure/        Docker, DB bootstrap
docs/specs/            The 13 phase specifications — read these first
docs/decisions/        Architecture Decision Records
tests/fixtures/        Synthetic data — the ONLY data development uses
```

---

## Development workflow

Work **one phase at a time**. Each spec in `docs/specs/` ends with a Phase Gate; every
box must be checked before the next phase starts.

```
task verify      # lint + test + build across all three languages — the commit gate
task test        # tests only
task fmt         # auto-format everything
```

**CI must never call a language model.** Phase 4 ships a `MockProvider` returning
fixtures; real-model evals run on demand in Phase 6.

---

## Free-first development

The entire stack runs locally at zero cost.

| Need | Development (free) | Production |
|---|---|---|
| LLM | Ollama — `qwen2.5:7b`, `llama3.2:3b` | Claude (Haiku / Sonnet / Opus by job) |
| Embeddings | `nomic-embed-text` (768-dim) | Hosted |
| Database | pgvector in Docker | Managed Postgres |
| Object storage | MinIO | S3 / R2 |
| Email | Mailpit | Resend / Brevo |

Swapping `LLM_PROVIDER=ollama` → `anthropic` is a config change, nothing more.
See [ADR-004](docs/decisions/004-llm-provider-abstraction.md).

---

## Documentation

| Document | Purpose |
|---|---|
| [docs/specs/00-OVERVIEW.md](docs/specs/00-OVERVIEW.md) | Product vision, principles, full stack |
| [docs/specs/PHASE-*.md](docs/specs/) | One self-contained spec per phase |
| [docs/PROJECT_STATUS.md](docs/PROJECT_STATUS.md) | Where we are right now |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | As-built architecture |
| [docs/DECISIONS.md](docs/DECISIONS.md) | ADR index |

---

## Licence

Private. Not yet licensed for distribution.

> **Open licence question:** Swiss Ephemeris (Phase 2) is dual-licensed AGPL-3.0 or
> commercial. The AGPL network clause is a real constraint for closed-source SaaS.
> [ADR-003](docs/decisions/003-astrology-engine.md) must be closed before Phase 7.
