# Phase 0 — Foundation

| | |
|---|---|
| **Goal** | A three-service repository a coding agent can safely work in, with all local infrastructure running for free. |
| **Deliverable** | `task up && task dev` starts Go API + Python astro + Python AI + Next.js web against local Postgres/Redis/MinIO/Ollama. `task verify` passes. CI is green. |
| **Depends on** | — |
| **Unlocks** | Phase 1 |
| **Estimated size** | 5–8 days — longer than a single-language setup. That is the cost of the polyglot stack, paid once. |
| **Cost to run** | ₹0 — everything local or free-tier |

No product features are built in this phase. Resist the urge.

---

## 1. Scope

### In scope
- Three service skeletons: Go API, Python `astro`, Python `ai`
- pnpm workspace for the TypeScript frontends
- `Taskfile.yml` — one command surface across three toolchains
- Docker Compose: Postgres 16 + pgvector, Redis 7, MinIO, Mailpit, plus the two Python services
- Ollama installed with three free dev models pulled
- `golang-migrate` + `sqlc` wired; first migration runs
- Go: chi router, `/health`, `slog` structured logging with PII redaction, error middleware, config validation
- Python: FastAPI skeletons, Pydantic settings, `/health`, structured logging
- **Contract pipeline**: FastAPI → OpenAPI → generated Go clients
- Next.js skeleton with design tokens and shadcn/ui
- Testing: `testing`+`testify`+`testcontainers-go`, `pytest`, Playwright
- GitHub Actions CI covering all three languages
- `.claude/` control layer
- `docs/` living documents + first six ADRs

### Out of scope
- Auth, users, astrology, AI, UI beyond a styled placeholder
- Terraform / cloud deployment (Phase 7 hardens this)
- Mobile (Phase 10)

---

## 2. Repository structure

See the root README for the full tree. What Phase 0 must create:

```
services/api/            Go      cmd/{api,worker}, internal/, db/{migrations,queries}
services/astro/          Python  app/{api,core,schemas}, tests/golden/
services/ai/             Python  app/{api,providers,prompts,rag,context,safety}, evals/
apps/web/                Next.js
packages/                TS shared
contracts/{openapi,gen}  generated service contracts
infrastructure/docker/
docs/                    + docs/decisions/
tests/{e2e,fixtures}
Taskfile.yml
docker-compose.yml
```

### Go module boundaries — enforced, not suggested

```
services/api/internal/
├── <domain>/          auth · users · profiles · charts · conversations
│                      billing · consultations · notifications · admin
├── platform/          db · redis · storage · queue · clients (astro, ai)
└── httpapi/           router · middleware · handlers
```

Rules, enforced by `golangci-lint` with `depguard`:

- A domain package may import `platform/` and `httpapi/` helpers, never another domain package's internals.
- Cross-domain access goes through an exported interface defined by the **consumer**, not the producer. That is idiomatic Go and it keeps the dependency graph acyclic.
- `platform/` never imports a domain package.

This is what makes a later service extraction cheap. Skipping it is what turns a
modular monolith into a big ball of mud with extra network hops.

### Python layout rules

- `astro-service` may **not** import `httpx`, `anthropic`, `openai`, or anything that
  can reach a model provider. Enforced by an import-linter rule in CI, and by an
  egress deny-list in the container. It is structurally incapable of asking an LLM to
  do arithmetic.
- `ai-service` has a **read-only** database role. It cannot write. Enforced by
  Postgres grants, not by convention.

---

## 3. Local infrastructure (all free)

`docker-compose.yml`:

```yaml
services:
  postgres:
    image: pgvector/pgvector:pg16
    environment:
      POSTGRES_USER: astro
      POSTGRES_PASSWORD: astro
      POSTGRES_DB: astro_dev
    ports: ["5432:5432"]
    volumes:
      - pgdata:/var/lib/postgresql/data
      - ./infrastructure/docker/init:/docker-entrypoint-initdb.d
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U astro"]
      interval: 5s

  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]

  minio:
    image: minio/minio
    command: server /data --console-address ":9001"
    environment:
      MINIO_ROOT_USER: minioadmin
      MINIO_ROOT_PASSWORD: minioadmin
    ports: ["9000:9000", "9001:9001"]
    volumes: ["miniodata:/data"]

  mailpit:
    image: axllent/mailpit
    ports: ["1025:1025", "8025:8025"]

  astro:
    build: ./services/astro
    ports: ["8100:8100"]
    environment:
      INTERNAL_TOKEN: dev-internal-token
    # NOTE: no model API keys, no LLM env vars. Deliberate.

  ai:
    build: ./services/ai
    ports: ["8200:8200"]
    environment:
      DATABASE_URL: postgresql://astro_ro:astro_ro@postgres:5432/astro_dev
      LLM_BASE_URL: http://host.docker.internal:11434/v1
      INTERNAL_TOKEN: dev-internal-token
    extra_hosts: ["host.docker.internal:host-gateway"]   # reach host Ollama
    depends_on: { postgres: { condition: service_healthy } }

volumes: { pgdata: {}, miniodata: {} }
```

`infrastructure/docker/init/01-init.sql`:

```sql
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- The single-writer rule, enforced by Postgres rather than by discipline.
CREATE ROLE astro_ro LOGIN PASSWORD 'astro_ro';
GRANT CONNECT ON DATABASE astro_dev TO astro_ro;
GRANT USAGE ON SCHEMA public TO astro_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO astro_ro;
```

`ai-service` connects as `astro_ro`. If it ever tries to write, Postgres rejects it.
That is a much stronger guarantee than a code review comment.

### Ollama — the free LLM backend

Installed on the host, not in Compose (GPU passthrough in Docker is more trouble than
it's worth).

```bash
curl -fsSL https://ollama.com/install.sh | sh

ollama pull llama3.2:3b        # ~2 GB  — classification, extraction
ollama pull qwen2.5:7b         # ~4.7GB — chat, interpretation
ollama pull nomic-embed-text   # ~274MB — embeddings, 768-dim

curl http://localhost:11434/v1/models   # OpenAI-compatible endpoint
```

That `/v1` endpoint means the official `openai` Python SDK talks to Ollama unchanged —
and the same adapter later covers Groq, OpenRouter and Cerebras.

**Hardware sizing.** 8 GB RAM → 3B + embeddings. 16 GB → add `qwen2.5:7b`. 32 GB →
`qwen2.5:14b` for better dev output. If the machine can't manage 7B, point
`LLM_BASE_URL` at Groq's free tier and keep Ollama for embeddings.

---

## 4. Toolchain setup

```bash
# Go
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
go install github.com/air-verse/air@latest        # hot reload in dev

# Python
curl -LsSf https://astral.sh/uv/install.sh | sh
cd services/astro && uv sync
cd services/ai    && uv sync

# JS
corepack enable && pnpm install

# Task runner
go install github.com/go-task/task/v3/cmd/task@latest
```

`Taskfile.yml`:

```yaml
version: '3'

tasks:
  up:        docker compose up -d
  down:      docker compose down

  dev:
    deps: [up]
    cmds:
      - task: dev:api
      - task: dev:astro
      - task: dev:ai
      - task: dev:web
    # run in parallel via `task --parallel dev`

  dev:api:   air -c services/api/.air.toml
  dev:astro: uv run --directory services/astro uvicorn app.main:app --reload --port 8100
  dev:ai:    uv run --directory services/ai    uvicorn app.main:app --reload --port 8200
  dev:web:   pnpm --filter web dev

  migrate:   migrate -path services/api/db/migrations -database "$DATABASE_URL" up
  migrate:down: migrate -path services/api/db/migrations -database "$DATABASE_URL" down 1
  sqlc:      sqlc generate -f services/api/sqlc.yaml

  contracts:
    cmds:
      - uv run --directory services/astro python -m app.export_openapi > contracts/openapi/astro.json
      - uv run --directory services/ai    python -m app.export_openapi > contracts/openapi/ai.json
      - oapi-codegen -config contracts/astro-client.yaml contracts/openapi/astro.json
      - oapi-codegen -config contracts/ai-client.yaml    contracts/openapi/ai.json

  lint:
    cmds:
      - golangci-lint run ./services/api/...
      - uv run --directory services/astro ruff check . && uv run --directory services/astro mypy --strict app
      - uv run --directory services/ai    ruff check . && uv run --directory services/ai    mypy --strict app
      - pnpm lint

  test:
    cmds:
      - go test ./services/api/... -race -count=1
      - uv run --directory services/astro pytest
      - uv run --directory services/ai    pytest
      - pnpm test

  verify:
    cmds:
      - task: contracts
      - git diff --exit-code contracts/    # fail if generated output drifted
      - task: lint
      - task: test
      - go build ./services/api/...
      - pnpm build
```

`task verify` is the commit gate. The `git diff --exit-code contracts/` step is what
prevents a service contract changing without anyone noticing.

---

## 5. Environment variables

Each service parses and validates its own config at startup and **fails fast** if
anything required is missing. No `os.Getenv` / `os.environ` access outside the config
package.

### Go — `services/api`

```bash
ENV=development
PORT=4000
WEB_URL=http://localhost:3000

DATABASE_URL=postgresql://astro:astro@localhost:5432/astro_dev
DATABASE_MAX_CONNS=20
REDIS_URL=redis://localhost:6379

S3_ENDPOINT=http://localhost:9000
S3_BUCKET=astro-dev
S3_ACCESS_KEY=minioadmin
S3_SECRET_KEY=minioadmin
S3_FORCE_PATH_STYLE=true

ASTRO_SERVICE_URL=http://localhost:8100
AI_SERVICE_URL=http://localhost:8200
INTERNAL_TOKEN=dev-internal-token
SERVICE_TIMEOUT_SECONDS=30

LOG_LEVEL=debug
OTEL_EXPORTER_OTLP_ENDPOINT=

FEATURE_AI_CHAT_ENABLED=false
FEATURE_VOICE_ENABLED=false
FEATURE_HUMAN_ASTROLOGER_ENABLED=false
FEATURE_COMPATIBILITY_ENABLED=false
FEATURE_PAYMENTS_ENABLED=false
```

```go
type Config struct {
    Env           string `env:"ENV" envDefault:"development" validate:"oneof=development staging production"`
    Port          int    `env:"PORT" envDefault:"4000"`
    DatabaseURL   string `env:"DATABASE_URL,required"`
    RedisURL      string `env:"REDIS_URL,required"`
    AstroURL      string `env:"ASTRO_SERVICE_URL,required"`
    AIURL         string `env:"AI_SERVICE_URL,required"`
    InternalToken string `env:"INTERNAL_TOKEN,required" validate:"min=16"`
    LogLevel      string `env:"LOG_LEVEL" envDefault:"info"`
}

func Load() (*Config, error) {
    var c Config
    if err := env.Parse(&c); err != nil {
        return nil, fmt.Errorf("parse config: %w", err)
    }
    if err := validator.New().Struct(&c); err != nil {
        return nil, fmt.Errorf("validate config: %w", err)
    }
    return &c, nil
}
```

### Python — `services/astro`

```bash
ENV=development
PORT=8100
INTERNAL_TOKEN=dev-internal-token
LOG_LEVEL=debug

EPHEMERIS_FLAG=moseph                # moseph (no data files) | swieph
EPHEMERIS_PATH=./data/ephe
DEFAULT_AYANAMSA=lahiri
DEFAULT_HOUSE_SYSTEM=whole_sign
DEFAULT_NODE_TYPE=mean

# Deliberately absent: any LLM base URL, key or model name.
```

### Python — `services/ai`

```bash
ENV=development
PORT=8200
INTERNAL_TOKEN=dev-internal-token
DATABASE_URL=postgresql://astro_ro:astro_ro@localhost:5432/astro_dev   # READ-ONLY role
REDIS_URL=redis://localhost:6379

LLM_PROVIDER=openai-compatible       # openai-compatible | anthropic | google | mock
LLM_BASE_URL=http://localhost:11434/v1
LLM_API_KEY=
LLM_PROVIDER_TIER=local              # local | free-hosted | paid  ← the PII guard reads this
LLM_MODEL_FAST=llama3.2:3b
LLM_MODEL_CHAT=qwen2.5:7b
LLM_MODEL_DEEP=qwen2.5:7b

EMBEDDING_PROVIDER=ollama
EMBEDDING_MODEL=nomic-embed-text
EMBEDDING_DIM=768                    # 768 local · 1024 for bge-m3 — never hardcode
```

```python
class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="forbid")

    env: Literal["development", "staging", "production"] = "development"
    database_url: PostgresDsn
    internal_token: str = Field(min_length=16)
    llm_provider: Literal["openai-compatible", "anthropic", "google", "mock"]
    llm_provider_tier: Literal["local", "free-hosted", "paid"]
    embedding_dim: int = 768

settings = Settings()   # raises at import time if anything is missing or wrong
```

`extra="forbid"` makes a typo in a `.env` **file** an error rather than a silently
ignored setting.

It does **not** cover OS environment variables. Pydantic ignores any variable that
matches no field, because the whole environment would otherwise be "extra" — which
means the protection is absent exactly where it matters most, since production config
arrives as environment variables rather than a `.env` file:

```
DEFAULT_AYANMSA=lahiri     # note the missing 'A'
```

leaves `default_ayanamsa` at its default, and every chart in the system is then
computed against the wrong zodiac with nothing reporting a problem.

`app/env_check.py` closes that gap: at startup it flags any environment variable whose
name is within edit distance 2 of a declared field and refuses to boot. It fails rather
than warns, because a warning in a container log is read by nobody and this failure mode
otherwise goes unnoticed for months.

`.env.example` is committed per service and stays in sync; a CI job diffs the declared
keys against the config schema and fails on drift.

---

## 6. Logging, tracing and errors

### Structured logs with a trace ID across all three services

```go
// Go — log/slog
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level:       level,
    ReplaceAttr: redactPII,     // see below
}))
slog.SetDefault(logger)
```

```python
# Python — structlog or stdlib logging with a JSON formatter
logger.info("chart.computed", trace_id=trace_id, profile_id=pid, duration_ms=ms)
```

Every log line in every service carries: `trace_id`, `service`, `level`, `msg`, plus
`user_id` / `conversation_id` where they apply. The Go middleware generates
`trace_id`, propagates it via `context.Context`, and forwards it as the `X-Trace-Id`
header on every call to a Python service, which reads it into its own context.

**Getting this right on day one is what makes Phase 5 debugging tractable.** A request
that crosses three processes is unreadable without a correlating ID.

### PII redaction at the logger, not the call site

Both languages redact by key name centrally: `email`, `phone`, `name`,
`date_of_birth`, `time_of_birth`, `place_of_birth`, `latitude`, `longitude`,
`address`, `token`, `password`, `authorization`, `api_key`.

Relying on every developer to remember is how PII ends up in production logs.

### Uniform error shape

```json
{ "error": { "code": "VALIDATION_FAILED", "message": "Invalid birth time", "trace_id": "..." } }
```

Go wraps errors with `fmt.Errorf("...: %w", err)` and maps sentinel errors to HTTP
status in one middleware. Python raises typed exceptions mapped by a FastAPI exception
handler. Client-facing messages are generic; stack traces, field names and internal
logic stay server-side.

---

## 7. Database and query layer

### Migrations — `golang-migrate`, owned by `api-service`

```
services/api/db/migrations/
├── 000001_init.up.sql
├── 000001_init.down.sql
```

Every migration has a real `down`. "Revert the commit" is not a rollback plan.
Only `api-service` runs migrations; the Python services never touch schema.

### Queries — `sqlc`

```sql
-- services/api/db/queries/users.sql

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (email, phone, name) VALUES ($1, $2, $3) RETURNING *;
```

```yaml
# sqlc.yaml
version: "2"
sql:
  - engine: postgresql
    schema: db/migrations
    queries: db/queries
    gen:
      go:
        package: dbgen
        out: internal/platform/db/dbgen
        sql_package: pgx/v5
        emit_pointers_for_null_types: true
```

`task sqlc` regenerates. You write SQL; you get type-safe Go. When Phase 7 needs
`SELECT … FOR UPDATE` inside a transaction, it is plain visible SQL rather than an
ORM incantation.

---

## 8. The contract pipeline

```python
# services/astro/app/export_openapi.py
import json, sys
from app.main import app
json.dump(app.openapi(), sys.stdout, indent=2, sort_keys=True)
```

`sort_keys=True` matters — without it the generated JSON reorders between runs and
every diff is noise.

```yaml
# contracts/astro-client.yaml
package: astroclient
generate: { client: true, models: true }
output: contracts/gen/astro/client.gen.go
```

Go then calls `astro-service` through a generated, typed client:

```go
chart, err := c.astro.ComputeChartWithResponse(ctx, astroclient.ComputeChartJSONRequestBody{
    BirthDate: "1994-08-17",
    BirthTime: ptr("14:35"),
    Latitude:  26.9124,
    Longitude: 75.7873,
    Timezone:  "Asia/Kolkata",
})
```

No hand-written HTTP client anywhere. CI fails if the committed contract differs from
what the source produces.

---

## 9. Testing setup

| Language | Stack | Notes |
|---|---|---|
| Go | `testing` + `testify` + **`testcontainers-go`** | Real Postgres and Redis in integration tests. Mocking a database hides exactly the bugs you need to find — especially locking behaviour in Phase 7. |
| Python | `pytest` + `pytest-asyncio` + **`hypothesis`** | Hypothesis carries the astrology property tests in Phase 2 |
| TypeScript | Vitest + Playwright | |
| Cross-service | Playwright against a full `docker compose` stack | |

**CI must never call an LLM.** Phase 4 ships a `MockProvider` returning fixtures; CI
uses it exclusively. Real-model evals run on demand in Phase 6.

### CI (GitHub Actions)

```yaml
jobs:
  go:      # services: postgres(pgvector), redis
    - go build ./... && go vet ./... && golangci-lint run
    - migrate up
    - go test ./... -race -count=1
  python:
    strategy: { matrix: { service: [astro, ai] } }
    - uv sync && ruff check . && mypy --strict app && pytest
  web:
    - pnpm install --frozen-lockfile && pnpm lint && pnpm test && pnpm build
  contracts:
    - task contracts && git diff --exit-code contracts/
  e2e:
    - docker compose up -d && pnpm test:e2e
```

`-race` on the Go tests is not optional. This service does concurrent billing later;
the race detector is how you find those bugs before users do.

---

## 10. The `.claude/` control layer

```
.claude/
├── CLAUDE.md
├── architecture.md
├── rules/
│   ├── go.md          module boundaries, context propagation, error wrapping,
│   │                  sqlc over ORM, int64 money, -race in tests
│   ├── python.md      Pydantic v2, async patterns, mypy --strict,
│   │                  NO LLM IMPORTS IN astro-service
│   ├── frontend.md
│   ├── database.md    migrations have downs, SINGLE WRITER RULE, indexes
│   ├── ai.md          THE DETERMINISM RULE, prompt versioning, PII rule
│   ├── security.md
│   └── testing.md     testcontainers not mocks, hypothesis for astrology
├── agents/   architect · go-backend · python-ai · frontend · qa · security
├── workflows/ feature · bugfix · phase-gate
└── state/    current-phase.md · current-task.md
```

`.claude/CLAUDE.md` must be short enough to read every session and must contain:

1. The determinism rule, and that `astro-service` has no model access by construction
2. The single-writer rule — only Go writes to Postgres
3. The free-tier PII rule
4. Current phase; phases are sequential and gated
5. Go and Python module boundary rules
6. Money is integer paise everywhere
7. "No new technology without an ADR"
8. Definition of Done
9. Pointers to `docs/PROJECT_STATUS.md` and `.claude/state/current-phase.md`

`.claude/state/current-phase.md`:

```
Phase: 0 — Foundation
Gate:  🔒 LOCKED

├── 0.1  Repo skeleton + Taskfile       ✅
├── 0.2  Docker Compose infra           ✅
├── 0.3  Ollama + free models           ✅
├── 0.4  Go API skeleton                🔄
├── 0.5  Python astro skeleton          ⏳
├── 0.6  Python ai skeleton             ⏳
├── 0.7  Contract pipeline              ⏳
├── 0.8  Migrations + sqlc              ⏳
├── 0.9  Web skeleton                   ⏳
├── 0.10 Test harness                   ⏳
├── 0.11 CI                             ⏳
├── 0.12 .claude/ harness               ⏳
└── 0.13 Docs + ADRs                    ⏳
```

---

## 11. Initial ADRs

| ADR | Decision | Notes |
|---|---|---|
| `001-service-topology.md` | Go API + two Python services, not a monolith | Records the operational cost honestly, and the single-writer mitigation |
| `002-postgres-pgvector.md` | pgvector over a dedicated vector DB | One database to operate; fine below ~1M chunks |
| `003-astrology-engine.md` | Swiss Ephemeris via `pyswisseph` — **licence decision required** | See Phase 2 §4. AGPL vs commercial is a real business decision; decide by end of Phase 2 |
| `004-llm-provider-abstraction.md` | Free local models in dev, paid in prod, one protocol | Includes the free-tier PII rule as an architectural constraint |
| `005-http-json-not-grpc.md` | HTTP + JSON between services for now | Records the conditions that would flip it to gRPC |
| `006-sqlc-not-orm.md` | `sqlc` over GORM/ent | Money and concurrency are SQL problems; keep the SQL visible |

---

## 12. UI features in this phase

Deliberately minimal — prove the pipeline, don't design screens.

- **Placeholder landing page** at `/` with design tokens applied: midnight navy, gold
  accent, product name, subtle starfield. Proves Tailwind + tokens + fonts + dark theme.
- **`/health` status page** showing, individually: Go API, Postgres, Redis, MinIO,
  `astro-service`, `ai-service`, and the configured LLM provider. With three backend
  services this is genuinely load-bearing for the next eleven phases — you need to
  know *which* thing is down.
- **shadcn/ui installed** with `button`, `card`, `input`, `dialog`, `skeleton`, `toast`.

Design tokens live in `packages/ui/tokens.ts` and feed Tailwind's theme — defined once.

### Health endpoint shape

```json
{
  "status": "degraded",
  "service": "api",
  "checks": {
    "postgres":   { "status": "ok", "latency_ms": 2 },
    "redis":      { "status": "ok", "latency_ms": 1 },
    "storage":    { "status": "ok", "latency_ms": 8 },
    "astro":      { "status": "ok", "latency_ms": 14 },
    "ai":         { "status": "error", "error": "connection refused" }
  }
}
```

Go's `/health` fans out to both Python services concurrently with a short timeout
(`errgroup` + 2s context), and reports each independently. A slow dependency must
never make the health check itself hang.

---

## 13. Task list

| # | Task | Done when |
|---|---|---|
| 0.1 | Repo skeleton, `Taskfile.yml`, pnpm workspace, `go.mod`, two `pyproject.toml` | `task --list` works; each toolchain builds an empty project |
| 0.2 | Docker Compose: Postgres+pgvector, Redis, MinIO, Mailpit + init SQL with the `astro_ro` role | `docker compose up -d` → all healthy; `astro_ro` cannot INSERT (verified) |
| 0.3 | Install Ollama, pull 3 models | `curl localhost:11434/v1/models` lists all three |
| 0.4 | Go: config with fail-fast validation | Deleting a required var crashes with a named error |
| 0.5 | Go: chi router, `/health` fan-out, `slog` JSON + redaction, trace-id middleware, error middleware | `GET /health` reports each dependency; a logged email comes out redacted |
| 0.6 | `golang-migrate` + first migration + `sqlc` generating | `task migrate` and `task sqlc` both succeed |
| 0.7 | Python astro: FastAPI skeleton, Pydantic settings, `/health`, logging | Runs on :8100; import-linter blocks LLM imports |
| 0.8 | Python ai: FastAPI skeleton, settings, `/health`, read-only DB check | Runs on :8200; a write attempt fails with a permission error |
| 0.9 | Contract pipeline: OpenAPI export + `oapi-codegen` + committed output | `task contracts` is idempotent; CI diff check passes |
| 0.10 | Go → astro and Go → ai typed clients with timeout, retry, trace propagation | Trace ID visible in all three services' logs for one request |
| 0.11 | Next.js skeleton, tokens, shadcn/ui, landing + status page | Status page reads live `/health` |
| 0.12 | `packages/{ui,types,api-client,config,analytics,content}` stubs | Each builds; cross-imports resolve |
| 0.13 | Test harness: testcontainers, pytest, hypothesis, Playwright — one real test each | `task test` passes |
| 0.14 | CI: 5 jobs (go, python matrix, web, contracts, e2e) | Green on a PR |
| 0.15 | `.claude/` harness | Files exist and are accurate |
| 0.16 | `docs/` living docs + ADRs 001–006 | Written; ADR-003 marks the licence question **open** |
| 0.17 | `tests/fixtures/charts/` — 10 synthetic birth profiles | Committed, documented as the only dev data |

---

## 14. Testing

| Level | What |
|---|---|
| Unit (Go) | Config validation rejects bad env; `redactPII` actually redacts; error→status mapping |
| Unit (Python) | Settings raise on missing/extra vars; health check shape |
| Integration (Go) | API boots against testcontainers Postgres + Redis; `/health` reports accurately |
| Integration | `astro_ro` role cannot INSERT, UPDATE or DELETE — **assert the permission error** |
| Contract | Generated client compiles and round-trips against the live Python service |
| Tracing | One request produces correlated log lines with the same `trace_id` in all three services |
| E2E | Landing loads; status page shows all six dependencies |

**Synthetic fixtures now, not later.** `tests/fixtures/charts/` holds ~10 fabricated
birth profiles (growing to ~30 in Phase 2). They are invented, not real people's data.
Establishing this in Phase 0 is what makes the free-tier PII rule enforceable rather
than aspirational.

---

## 15. Security checklist

- [ ] `.env` files gitignored; `.env.example` committed per service with no real values
- [ ] No secret, key, token or password in any tracked file
- [ ] Secret scanning enabled (gitleaks pre-commit + GitHub secret scanning)
- [ ] Every service validates config at startup and exits with a named error if incomplete
- [ ] PII redaction configured in **both** Go and Python loggers
- [ ] Trace ID propagated but never carries PII
- [ ] Error responses generic; no stack traces to clients in any service
- [ ] **Python services bind to the internal network only; not publicly reachable**
- [ ] `X-Internal-Token` required on every service-to-service call; constant-time compare
- [ ] **`ai-service` connects with a read-only role; write attempt fails (tested)**
- [ ] **`astro-service` has no model-provider dependency; CI import rule enforces it**
- [ ] Helmet-equivalent security headers and CORS restricted to `WEB_URL`
- [ ] `govulncheck`, `pip-audit` and `pnpm audit` in CI; fail on high/critical
- [ ] `tests/fixtures/` documented as synthetic-only

---

## 16. Observability

- Structured JSON logs in all three services with a shared `trace_id`
- OpenTelemetry tracing wired (exporter optional in dev) so a request can be followed across processes
- `/health` per service; Go's aggregates
- Request duration logged on every route in every service
- Sentry wired but optional in dev
- `packages/analytics` defines the typed event union — nothing fires yet, but the shape exists so Phase 1 can emit immediately

---

## 17. Risks

| Risk | Mitigation |
|---|---|
| **Three toolchains slow everything down** | `Taskfile` gives one command surface; `docker compose up` gives one startup. Budget the extra 2–3 days in this phase — it is paid once. |
| Contract drift between Go and Python | Generated clients + CI diff check. Never hand-write a cross-service client. |
| Machine can't run a 7B model | Point `LLM_BASE_URL` at Groq free tier; keep Ollama for embeddings |
| Someone gives `astro-service` an LLM client | CI import rule + no API key in its environment + container egress deny-list. Three independent barriers. |
| Someone lets `ai-service` write to the DB | Postgres role grants. It is not possible, not merely discouraged. |
| Over-building the skeleton | Hard rule: no product feature in Phase 0. A placeholder page is the correct amount of UI. |
| ADR-003 licence question forgotten | Explicit checklist item in the Phase 2 gate |

---

## 18. Phase Gate 🔒

Every box must be checked before Phase 1 begins.

**Outcome (2026-09-13): 20 of 20 met. Gate closed.**

Item 2 took three attempts — the 4.7 GB model failed its checksum twice with two
different wrong hashes before succeeding, one of five data-corruption events on this
machine. Both models are now verified working, not merely present: `qwen2.5:7b`
returns a completion and `nomic-embed-text` returns 768 dimensions matching
`EMBEDDING_DIM`. The corruption pattern is documented in `docs/PROJECT_STATUS.md` and
is worth investigating independently of this project.

One item was reshaped during implementation and is recorded honestly rather than
ticked loosely:
- Contract generation produces a client for `/health` only. That is thin, but it
  proves the pipeline end to end and removes the hand-written client the spec
  forbids, so it stands.

**Amended 2026-09-13, after auditing §13 separately.** The gate above had passed three
times while the §13 task list it summarises still had six open items — a summary is not
evidence. Closing them exposed three defects the gate could not have caught: retry was
dead code (`http.NoBody`, never `nil`, so no GET was ever retryable), TypeScript had no
linter at all, and shadcn had shipped 83 hardcoded palette classes past a green `tsc`
and a green build. `packages/*` is now all six packages rather than the one noted here
previously. CI is 10 jobs, not five, and the E2E job reached Playwright for the first
time only after two environment bugs were fixed — an unpullable `minio/minio` and a
`.env` sourced from the wrong directory.

Deliberate deviations from the spec's literal wording, all documented:
- npm workspaces, not pnpm (ADR-008 — `corepack enable` needs root on this machine)
- `tests/test_no_llm_imports.py` rather than import-linter; it walks the real
  dependency tree, which is stricter than a declared contract
- ADRs 001–008, not 001–006

- [x] `docker compose up -d` brings up Postgres+pgvector, Redis, MinIO, Mailpit, astro, ai — all healthy
- [x] `ollama list` shows `llama3.2:3b`, `qwen2.5:7b`, `nomic-embed-text` — inference and 768-dim embeddings both verified
- [x] `task verify` passes from a clean clone
- [x] `task dev` starts web (3000), API (4000), astro (8100), ai (8200)
- [x] Status page shows all six dependencies green, each with the configured LLM provider surfaced on the ai-service row (§12)
- [x] `task migrate` applies; `vector` and `pg_trgm` extensions enabled
- [x] `task sqlc` generates compiling Go from SQL
- [x] `task contracts` is idempotent; CI diff check passes
- [x] Go calls both Python services through **generated** typed clients
- [x] One request produces correlated `trace_id` log lines in all three services
- [x] Deleting a required env var in any service causes a **named** startup failure
- [x] Logging an object containing an email produces a redacted line in Go **and** Python
- [x] **`astro_ro` role cannot write — permission error asserted in a test**
- [x] **`astro-service` has no LLM dependency — CI import rule proves it**
- [x] CI green on a PR across all ten jobs (spec said five; the extra five are integration, determinism, secrets, e2e and env-drift)
- [x] `.claude/` exists with CLAUDE.md, rules, agents, workflows, state
- [x] `docs/ARCHITECTURE.md`, `ROADMAP.md`, `DECISIONS.md`, `PROJECT_STATUS.md` written
- [x] ADRs 001–006 written; ADR-003 records the ephemeris licence question as **open**
- [x] `tests/fixtures/charts/` contains ≥10 synthetic profiles
- [x] Zero secrets in git history (gitleaks clean)
