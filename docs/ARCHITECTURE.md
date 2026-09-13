# Architecture

**As-built, not as-planned.** This describes what exists today. Where something is
planned but absent, it says so. Update it when the code changes, not before.

Last verified against a running system: 2026-09-13 (Phase 0).

---

## Shape

```
                         ┌─────────────────────┐
                         │   web  (Next.js 16) │  :3000
                         └──────────┬──────────┘
                                    │ HTTPS
                                    ▼
            ┌───────────────────────────────────────────────┐
            │           api-service   ·   Go 1.26           │  :4000
            │                                               │
            │  chi router · pgx · slog · asynq (Phase 2+)    │
            │                                               │
            │  SOLE DATABASE WRITER                          │
            │  ONLY SERVICE EXPOSED TO THE INTERNET          │
            └───────┬───────────────────────────┬───────────┘
                    │ HTTP/JSON                 │ HTTP/JSON (+SSE, Phase 5)
                    │ X-Internal-Token          │ X-Internal-Token
                    ▼                           ▼
    ┌───────────────────────────┐  ┌────────────────────────────────┐
    │ astro-service · Python    │  │ ai-service · Python            │
    │ FastAPI                   │  │ FastAPI                        │  :8200
    │                      :8100│  │                                │
    │ STATELESS — no DB         │  │ READ-ONLY DB role (astro_ro)   │
    │ OFFLINE — no HTTP client  │  │ Provider-agnostic LLM layer    │
    │ AI-FREE — no model SDK    │  │ Refuses free tier in prod      │
    └───────────────────────────┘  └────────────────┬───────────────┘
                                                    │ SELECT only
                                                    ▼
            ┌───────────────────────────────────────────────┐
            │  PostgreSQL 16 + pgvector + pg_trgm     :5433  │
            │  Redis 7                                :6381  │
            │  MinIO (S3-compatible)             :9000/9001  │
            │  Mailpit                           :1025/8025  │
            └───────────────────────────────────────────────┘
```

Ports 5433 and 6381 are non-standard because this development machine already runs
Postgres on 5432 and Redis on 6379/6380. Container-internal ports are unchanged.

---

## Why three services

The languages fall on genuine workload boundaries, not arbitrary domain lines.

| Service | Language | The work it is suited to |
|---|---|---|
| `api-service` | Go | WebSocket fan-out for consultations, a billing tick every 30s per active session under contention, connection handling. Goroutines and a real type system make this materially better than an event loop. Single static binary; the runtime image is distroless. |
| `astro-service` | Python | `pyswisseph` is the mature Swiss Ephemeris binding. The Vedic layer — dashas, vargas, Ashtakoota — is numerical work Python does comfortably. Stateless, so the GIL is irrelevant and scaling is "add replicas". |
| `ai-service` | Python | Every LLM, embedding and audio library is Python-first. FastAPI streams SSE cleanly. The eval harness (Phase 6) is pytest. |

**The cost, stated plainly:** three CI pipelines, three dependency trees, cross-service
contracts to keep in sync, and local development that needs several processes. Roughly
2–3 extra days in Phase 0 and 2–5 in most later phases.

**What makes it tolerable:** one database with one writer, one `docker compose up`, one
`Taskfile` command surface, one trace ID across all three. See
[ADR-001](decisions/001-service-topology.md).

---

## The four invariants and how each is enforced

Each is machinery, not discipline. Each has a test that fails if it stops holding.

### 1. Astrology is computed, never generated

`astro-service` cannot reach a language model. Three independent barriers:

- **AST scan** over `app/**` rejecting any LLM SDK or HTTP client import
- **Installed-package check** — the SDKs must not even be present in the environment
- **No model configuration** in its settings or its container environment

`services/astro/tests/test_no_llm_imports.py` · its own named CI job.

### 2. One writer

`ai-service` connects as `astro_ro`: `USAGE` on the schema, `SELECT` on tables, and
nothing else. Enforced by Postgres grants in migration `000001_bootstrap`, because a
grant cannot be forgotten during a refactor.

`services/api/internal/platform/db/singlewriter_test.go` runs the real bootstrap SQL
against a real container and asserts `INSERT`, `UPDATE`, `DELETE`, `TRUNCATE` and
`CREATE TABLE` are all refused with SQLSTATE 42501, while `SELECT` works.

### 3. No real user data reaches a free model tier

`ai-service` refuses to start when `ENV=production` and the provider tier is not
`paid`. Birth date + time + place is close to a unique identifier; conversations cover
health, marriage and money; most free tiers may train on inputs.

`services/ai/app/guards.py` · `tests/test_guards.py`.

### 4. Degraded, never down

`/health` probes every dependency concurrently with a 2-second budget and reports each
independently. Postgres and Redis are **critical**; the Python services and storage are
**not**. A failing non-critical dependency yields HTTP 200 `degraded`, not 503.

Verified by stopping `astro-service` and observing the other dependencies stay `ok`.

---

## Request path

```
Browser
  │  GET /api/v1/meta
  ▼
api-service
  │  Recover        panic → 500, stack to log only
  │  TraceID        adopt X-Trace-Id (≤64 chars) or mint a UUID
  │  AccessLog      method, path, status, duration — no query string, no body
  │  SecurityHeaders
  │  CORS           restricted to WEB_URL, credentials allowed
  │  Timeout        30s
  ▼
handler
  │  (Phase 1+) auth → authz → rate limit → domain service
  ▼
response + X-Trace-Id
```

The trace ID is carried on `context.Context`, attached to every log line by a
`slog.Handler` wrapper, and forwarded as `X-Trace-Id` on calls to the Python services,
which read it into a `contextvar` and attach it to their own log lines. One user request
is therefore correlatable across three processes.

---

## Observability

Both languages emit the same JSON shape, so all three services can be queried with one
set of filters:

```json
{"time":"...","level":"INFO","service":"api","msg":"http request",
 "trace_id":"...","method":"GET","path":"/api/v1/meta","status":200,"duration_ms":1}
```

**PII redaction happens in the formatter, never at the call site.** Both
implementations redact the same key list, which includes `birth_date`, `birth_time`,
`birth_place`, `latitude` and `longitude` alongside the obvious fields — because in
combination those are close to a unique identifier.

One asymmetry worth knowing: Go's `slog` cannot descend into arbitrary struct values,
so the "log IDs, not objects" rule is load-bearing there. Python **can** walk nested
dicts and lists (bounded to depth 6), so it catches nested leaks the Go side would
miss. The discipline still applies to both.

---

## Data

**PostgreSQL 16** with `pgvector` (embeddings, Phases 5–6) and `pg_trgm` (place-name
search, Phase 2).

Migrations are `golang-migrate`, owned by `api-service`; the Python services never
touch schema. **Every migration has a real `down`** — "revert the commit" is not a
rollback plan. `000001_bootstrap` is idempotent so it is a no-op on a database already
bootstrapped by the local container's init script.

Queries are `sqlc`-generated from hand-written SQL ([ADR-006](decisions/006-sqlc-not-orm.md)).
Not an ORM: this application's hard parts — `SELECT … FOR UPDATE` on wallet balances,
hybrid vector + keyword retrieval, billing ticks under contention — are SQL problems,
and an ORM would obscure exactly the queries that most need careful reading.

**Money is `int64` paise in Go, `BIGINT` in Postgres, `int` in Python. Never a float.**
Not yet exercised — Phase 7 — but the rule is established now because retrofitting it
is far worse than adopting it.

---

## Cross-service contracts

FastAPI emits OpenAPI; Go clients are generated from it
([ADR-005](decisions/005-http-json-not-grpc.md)).

```
services/{astro,ai}/app  ──►  /openapi.json  ──►  contracts/openapi/*.json  (committed)
                                                          │
                                                   oapi-codegen
                                                          ▼
                                              contracts/gen/*/client.gen.go
```

CI regenerates and fails if the committed output differs from what the source produces,
which makes every contract change visible in a diff.

Service-to-service calls carry `X-Internal-Token`, compared in constant time. `/health`
is exempt so orchestrators can probe liveness without holding a credential. Both Python
services bind to the internal network only and are never reachable from the internet.

---

## What is deliberately absent in Phase 0

| Absent | Arrives |
|---|---|
| Authentication, users, sessions | Phase 1 |
| Any domain table beyond `schema_meta` | Phase 1 |
| Ephemeris computation | Phase 2 |
| Any LLM call | Phase 4 |
| Background jobs (`asynq`) | Phase 2 |
| `packages/*` contents | Phase 3 (UI), Phase 10 (mobile reuse) |
| Terraform / deployment | Phase 7 |

`api-service` is not in `docker-compose.yml` on purpose: it is the service edited most
often, and running it from source is faster than any container rebuild loop. Its
Dockerfile exists for deployment.
