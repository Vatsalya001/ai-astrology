# AI Astrology Companion

> **The AI doesn't just know astrology. It knows YOUR astrology.**

A personal AI astrologer that understands the user's birth chart, life context and
previous conversations — and connects the user to a human astrologer when deeper
guidance is needed.

This folder is the **specification set**. It is not code. Each `PHASE-*.md` file is
self-contained: goal, scope, architecture, data model, API, UI features, free-tooling
setup, task list, tests, security checklist, Definition of Done and a blocking phase gate.

---

## How to use these documents

Work **one phase at a time**. Do not start Phase N+1 until the Phase N gate passes.

```
Read PHASE-NN-*.md
        ↓
Plan the phase (tickets are already listed in the file)
        ↓
Implement the smallest coherent increment
        ↓
Test / lint / vet / typecheck
        ↓
Update docs/PROJECT_STATUS.md
        ↓
Run the Phase Gate checklist at the bottom of the file
        ↓
Only then: next phase
```

Each file ends with a **Phase Gate**. Every box must be checked before moving on.
A gate is a hard stop, not a suggestion.

---

## Phase index

| # | File | Deliverable | Depends on |
|---|------|-------------|------------|
| 0 | [PHASE-00-FOUNDATION.md](./PHASE-00-FOUNDATION.md) | Three-service skeleton, tooling, CI, `.claude/` control layer, ADRs, local infra | — |
| 1 | [PHASE-01-AUTH-AND-USERS.md](./PHASE-01-AUTH-AND-USERS.md) | A user can sign up, log in, manage a profile (Go) | 0 |
| 2 | [PHASE-02-ASTROLOGY-ENGINE.md](./PHASE-02-ASTROLOGY-ENGINE.md) | Birth details in → deterministic chart, dashas, transits out (Python) | 1 |
| 3 | [PHASE-03-KUNDLI-UI.md](./PHASE-03-KUNDLI-UI.md) | A user can *see* their Kundli | 2 |
| 4 | [PHASE-04-AI-INFRASTRUCTURE.md](./PHASE-04-AI-INFRASTRUCTURE.md) | LLM provider abstraction, prompt registry, intent classifier, router (Python) | 3 |
| 5 | [PHASE-05-RAG-AND-CHAT.md](./PHASE-05-RAG-AND-CHAT.md) | "Chat with my Kundli" — streaming, grounded, explainable | 4 |
| 6 | [PHASE-06-MEMORY-AND-PERSONALIZATION.md](./PHASE-06-MEMORY-AND-PERSONALIZATION.md) | Memory, summaries, daily astrology, eval harness | 5 |
| 7 | [PHASE-07-MONETIZATION.md](./PHASE-07-MONETIZATION.md) | Subscriptions, credits, wallet, payments (Go) | 6 |
| 8 | [PHASE-08-ASTROLOGER-MARKETPLACE.md](./PHASE-08-ASTROLOGER-MARKETPLACE.md) | AI → human handoff, consultations, payouts (Go) | 7 |
| 9 | [PHASE-09-VOICE-AI.md](./PHASE-09-VOICE-AI.md) | Speak naturally with the AI astrologer (Python) | 6 |
| 10 | [PHASE-10-MOBILE.md](./PHASE-10-MOBILE.md) | React Native / Expo app on the same APIs | 6 |
| 11 | [PHASE-11-ADVANCED-INTELLIGENCE.md](./PHASE-11-ADVANCED-INTELLIGENCE.md) | Life timeline, reports, astrologer copilot, scale | 8 |

**MVP = Phases 0 → 6.** Everything after that is expansion.

---

## Non-negotiable product principles

### 1. Astrology calculation is deterministic. Always.

An LLM must **never** compute planetary positions, houses, nakshatras, dashas,
ascendant, degrees, transits or yogas. Those come from an ephemeris engine.

```
Birth Data
    ↓
astro-service  (Python + Swiss Ephemeris)    ← deterministic, testable, reproducible
    ↓
Structured Chart JSON
    ↓
Astrology Context Builder  (ai-service)      ← selects what matters for this question
    ↓
LLM                                          ← interpretation, language, empathy only
    ↓
Personalized Response
```

This principle is **enforced by the service topology**, not just by discipline:
`astro-service` has no LLM client, no API key and no network egress to any model
provider. It is structurally incapable of asking a model to do arithmetic.

If a number appears in an AI response, it was computed upstream and passed in.

### 2. Every AI claim is traceable

Each substantive response records which chart facts it was built from. The user can
tap **"Why am I seeing this?"** and get: 10th house · Saturn · current Mahadasha ·
current transit. A black box does not earn trust in this category.

### 3. Guidance, never certainty

No guaranteed marriage, pregnancy, death, medical, legal or financial outcomes.
Traditional framing ("this combination is traditionally read as…"), not prediction
framing ("you will…"). See the safety section in Phase 4.

### 4. Privacy is a feature, not a compliance chore

Birth date, birth time and birth place **are PII** — and in combination they are
close to a unique identifier. This constrains where that data is allowed to travel.
See "The free-tier PII rule" below; it is the most important operational rule here.

---

## System architecture

```
                         ┌─────────────────────┐
                         │      CLIENTS        │
                         │ Web · Mobile        │
                         │ Admin · Astrologer  │
                         └──────────┬──────────┘
                                    │ HTTPS / WSS
                                    ▼
            ┌───────────────────────────────────────────────┐
            │           api-service   ·   Go                │
            │                                               │
            │  Auth · RBAC · Rate limiting · Validation      │
            │  Users · Birth profiles · Chart storage        │
            │  Conversations · Billing · Wallet · Ledger     │
            │  Consultations · WebSocket hub · Workers       │
            │                                               │
            │  OWNS THE DATABASE. Single writer.             │
            └───────┬───────────────────────────┬───────────┘
                    │ HTTP/JSON                 │ HTTP/JSON + SSE
                    ▼                           ▼
    ┌───────────────────────────┐  ┌────────────────────────────────┐
    │  astro-service · Python   │  │   ai-service · Python          │
    │                           │  │                                │
    │  Swiss Ephemeris          │  │  LLM providers · Prompt registry│
    │  Chart · Dasha · Transit  │  │  Intent · Safety · Validation   │
    │  Yoga · Varga · Ashtakoota│  │  RAG retrieval · Context builder│
    │                           │  │  Memory extraction · Evals      │
    │  STATELESS. No DB.        │  │  Voice: STT / TTS (Phase 9)     │
    │  NO LLM CLIENT.           │  │                                │
    │  NO MODEL EGRESS.         │  │  Read-only DB access (pgvector) │
    └───────────────────────────┘  └────────────────┬───────────────┘
                    │                               │
                    └───────────────┬───────────────┘
                                    ▼
                         ┌─────────────────────┐
                         │    DATA LAYER       │
                         │ PostgreSQL+pgvector │
                         │ Redis · Object Store│
                         └──────────┬──────────┘
                                    ▼
                         ┌─────────────────────┐
                         │ EXTERNAL SERVICES   │
                         │ LLM · Payments      │
                         │ Notify · LiveKit    │
                         └─────────────────────┘
```

### Why this split

| Service | Language | Why this language |
|---|---|---|
| `api-service` | **Go** | The high-concurrency work: WebSocket fan-out for consultations, per-30-second billing ticks under contention, request routing, connection handling. Goroutines and a real type system make this genuinely better than an event-loop runtime. Single static binary, fast cold start, trivial containers. |
| `astro-service` | **Python** | `pyswisseph` is the mature reference binding to Swiss Ephemeris. The Vedic layer on top — dashas, vargas, nakshatras, Ashtakoota — is numerical work Python does comfortably. Stateless and cacheable, so the GIL is irrelevant. |
| `ai-service` | **Python** | Every LLM, embedding and audio library is Python-first. FastAPI streams SSE cleanly. The eval harness is pytest. Fighting this ecosystem from another language costs weeks for no gain. |
| Web / Mobile | **TypeScript** | Next.js and Expo. Unchanged. |

### The honest tradeoff

This is **three deployables instead of one**. That is real operational cost: three
CI pipelines, three dependency trees, service-to-service contracts to keep in sync,
distributed tracing to correlate a request across processes, and local development
that needs several things running at once.

You are buying, in exchange: Go's concurrency exactly where the marketplace needs it,
Python's ecosystem exactly where the AI and ephemeris work needs it, and a hard
structural wall between deterministic astrology and probabilistic AI.

Mitigations, all applied from Phase 0:
- **One database, one writer.** Only `api-service` writes. `ai-service` gets a
  read-only role scoped to the knowledge and memory tables. `astro-service` gets no
  database access at all. This removes the worst class of distributed-systems bug.
- **One `docker compose up`** brings the whole stack up locally.
- **One contract format.** FastAPI emits OpenAPI; Go clients are generated from it.
  No hand-written HTTP clients that silently drift.
- **One trace ID** propagated across all three services from the first request.

Do not split further until traffic demands it. Three services is the floor for this
stack, not a starting point to expand from.

---

## Technology stack

### `api-service` — Go

| Concern | Choice | Notes |
|---|---|---|
| Go version | 1.22+ | |
| Router | `chi` | stdlib-compatible `http.Handler`, no framework lock-in |
| Postgres driver | `pgx/v5` | native protocol, `pgxpool` for pooling |
| Query layer | **`sqlc`** | generates type-safe Go from plain SQL. Not an ORM — you write SQL, you get structs. |
| Migrations | `golang-migrate` | versioned up/down SQL |
| Validation | `go-playground/validator` | struct tags |
| Config | `caarlos0/env` + validation | fail fast at startup |
| Logging | `log/slog` (stdlib) | structured JSON, context-aware |
| Queue | `hibiken/asynq` | Redis-backed, mature, has a web UI |
| WebSocket | `coder/websocket` | Phase 8 |
| JWT | `golang-jwt/jwt/v5` | |
| Tracing | OpenTelemetry | |
| Testing | stdlib `testing` + `testify` + `testcontainers-go` | real Postgres in tests, not mocks |
| Lint | `golangci-lint` | |

**`sqlc` over an ORM, deliberately.** This application's hard parts are money and
concurrency: `SELECT … FOR UPDATE`, CTEs for ledger aggregation, hybrid vector +
keyword retrieval. Those are SQL problems. An ORM obscures exactly the queries you
most need to read carefully.

### `astro-service` and `ai-service` — Python

| Concern | Choice | Notes |
|---|---|---|
| Python version | 3.12+ | |
| Package manager | **`uv`** | dramatically faster than pip/poetry; lockfile-based |
| Framework | FastAPI + `uvicorn` | OpenAPI generation is the cross-service contract |
| Validation & settings | Pydantic v2 | models double as the API schema |
| Ephemeris | `pyswisseph` | astro-service only |
| LLM SDK | `anthropic` | ai-service, production |
| Local models | `openai` SDK pointed at Ollama | one client for every OpenAI-compatible backend |
| DB access | `asyncpg` + `pgvector` | ai-service only, **read-only role** |
| HTTP client | `httpx` | async |
| Lint & format | `ruff` | replaces black + flake8 + isort |
| Types | `mypy --strict` | |
| Testing | `pytest` + `pytest-asyncio` + `hypothesis` | hypothesis for the astrology property tests |

### Frontend — TypeScript

Next.js (App Router) · React · Tailwind · shadcn/ui · TanStack Query · Expo for mobile.
pnpm workspaces for the JS side.

---

## Repository layout

```
ai-astrology/
├── apps/                          TypeScript
│   ├── web/                       Next.js — user-facing
│   ├── mobile/                    Expo (Phase 10)
│   ├── admin/                     Next.js — internal console
│   └── astrologer/                Next.js — astrologer console (Phase 8)
│
├── services/
│   ├── api/                       Go
│   │   ├── cmd/
│   │   │   ├── api/main.go
│   │   │   └── worker/main.go
│   │   ├── internal/
│   │   │   ├── auth/  users/  profiles/  charts/  conversations/
│   │   │   ├── billing/  consultations/  notifications/  admin/
│   │   │   ├── platform/          db · redis · storage · queue · clients
│   │   │   └── httpapi/           router · middleware · handlers
│   │   ├── db/
│   │   │   ├── migrations/        golang-migrate SQL
│   │   │   └── queries/           sqlc source SQL
│   │   ├── sqlc.yaml
│   │   └── go.mod
│   │
│   ├── astro/                     Python — deterministic, stateless
│   │   ├── app/
│   │   │   ├── main.py
│   │   │   ├── api/               FastAPI routers
│   │   │   ├── core/              ephemeris · chart · dasha · transit
│   │   │   │                      yoga · varga · ashtakoota
│   │   │   └── schemas/           Pydantic models
│   │   ├── tests/
│   │   │   └── golden/            30 fixture charts
│   │   └── pyproject.toml
│   │
│   └── ai/                        Python — probabilistic
│       ├── app/
│       │   ├── main.py
│       │   ├── api/
│       │   ├── providers/         llm · embedding · stt · tts adapters
│       │   ├── prompts/           versioned, immutable
│       │   ├── rag/               retrieval · chunking · ingestion
│       │   ├── context/           astrology + memory context builders
│       │   ├── safety/            classifiers · validators
│       │   └── schemas/
│       ├── evals/                 the product's own quality harness
│       ├── tests/
│       └── pyproject.toml
│
├── packages/                      TypeScript, frontend-shared
│   ├── ui/  types/  api-client/  config/  analytics/  content/
│
├── contracts/
│   ├── openapi/                   generated from FastAPI, committed
│   └── gen/                       oapi-codegen Go clients, generated
│
├── infrastructure/
│   ├── docker/  terraform/  deployment/
│
├── docs/
│   ├── ARCHITECTURE.md  ROADMAP.md  DECISIONS.md  PROJECT_STATUS.md
│   └── decisions/                 ADRs
│
├── tests/
│   ├── e2e/                       Playwright, cross-service
│   └── fixtures/                  synthetic birth profiles — the ONLY dev data
│
├── Taskfile.yml                   one command surface for three languages
└── docker-compose.yml
```

### One command surface

Three languages means three toolchains. A `Taskfile.yml` (or `Makefile`) hides that:

```bash
task up            # docker compose up -d, all deps + services
task dev           # run api + astro + ai + web with reload
task test          # go test ./... && pytest && pnpm test
task lint          # golangci-lint && ruff && eslint
task verify        # lint + test + build, everything
task migrate       # golang-migrate up
task sqlc          # regenerate Go from SQL
task contracts     # regenerate OpenAPI + Go clients
task eval          # AI eval harness (Phase 6)
```

`task verify` is the gate for every commit. If it doesn't pass, nothing moves.

---

## Service contracts

FastAPI generates OpenAPI; Go clients are generated from it. No hand-written clients.

```
services/astro/app/  ──uvicorn──►  /openapi.json
                                        │
                                   task contracts
                                        │
                     ┌──────────────────┴──────────────────┐
                     ▼                                     ▼
      contracts/openapi/astro.json              contracts/gen/astro/client.go
         (committed, reviewable)                   (oapi-codegen, generated)
```

CI regenerates and fails if the committed output differs from what the source
produces. A contract change is therefore always visible in a diff — which is the
whole point.

**HTTP + JSON rather than gRPC, for now.** gRPC is a better fit at scale, but protoc
toolchains in a three-language monorepo are a real tax and the call volume here is
low (charts are cached; AI calls are dominated by model latency, not transport).
`ADR-005` records this with the conditions that would flip it.

### Internal authentication

Service-to-service calls carry a shared secret in `X-Internal-Token`, and the Python
services bind to the internal network only — never exposed publicly. `astro-service`
and `ai-service` must be unreachable from the internet; only Go faces the world.

---

## Free-first development strategy

You asked to build and test on free models. That works well here, and the Python AI
service makes it cleaner than it would be anywhere else — every free local inference
tool in this space is Python-native.

### The core move: one provider protocol, many backends

Everything in `ai-service` talks to `LLMProvider`. Nothing imports a vendor SDK
outside `app/providers/`.

```python
class LLMProvider(Protocol):
    id: str
    tier: Literal["local", "free-hosted", "paid"]   # ← the PII guard reads this

    async def complete(self, req: CompletionRequest) -> CompletionResponse: ...
    def stream(self, req: CompletionRequest) -> AsyncIterator[CompletionChunk]: ...
```

Swapping `LLM_PROVIDER=ollama` → `LLM_PROVIDER=anthropic` must require **zero**
changes outside config. Phase 4 builds this.

### Tier 0 — Local, free, offline, no API key (default for development)

[Ollama](https://ollama.com) serves an OpenAI-compatible endpoint at
`http://localhost:11434/v1`, so the official `openai` Python SDK talks to it directly.
One adapter covers Ollama, LM Studio, Groq, OpenRouter and Cerebras.

```bash
ollama pull qwen2.5:7b          # chat / interpretation
ollama pull llama3.2:3b         # intent classification, memory extraction
ollama pull nomic-embed-text    # embeddings, 768-dim
```

| Job | Local free model | Why |
|---|---|---|
| Intent classification | `llama3.2:3b` | 21 labels, short output — a small model is genuinely fine |
| Memory extraction | `llama3.2:3b` | Structured JSON from a short transcript |
| Chat / interpretation | `qwen2.5:7b` or `14b` | Strong instruction-following at 7–14B |
| Embeddings | `nomic-embed-text` | 768-dim, free, CPU-fast |

8 GB RAM runs the 3B + embeddings. 16 GB runs 7B. 32 GB runs 14B.

> **Read `EMBEDDING_DIM` from config, never a literal.** `nomic-embed-text` is 768-dim;
> `bge-m3` and most hosted embeddings are 1024. A hardcoded `vector(768)` in a
> migration means re-embedding the whole corpus later. Phase 5 covers the migration.

### Tier 1 — Free hosted tiers (when local is slow, or for CI smoke tests)

| Provider | Free offering | Best for |
|---|---|---|
| **Google AI Studio** | Gemini Flash free tier + free embeddings | Most generous free hosted option |
| **Groq** | Free tier, very high tokens/sec | Latency and streaming UX work |
| **OpenRouter** | `:free` suffixed models | Comparing many models behind one key |
| **Cerebras** | Free tier, very fast | Streaming demos |

Rate limits and model names change frequently — verify at signup. Build retry and
fallback into the provider layer (Phase 4) so a 429 degrades rather than errors.

### ⚠️ The free-tier PII rule (read this twice)

**Most free API tiers reserve the right to train on your inputs.**

Birth date + birth time + birth place is effectively a unique identifier, and
conversation content covers health, marriage, money and family.

> **Rule: no real user data ever reaches a free tier.**
>
> - **Development and CI:** free models, synthetic fixtures only. ~30 fabricated birth
>   profiles in `tests/fixtures/charts/` cover every code path.
> - **Staging:** free models, synthetic or fully anonymised data.
> - **Production:** paid provider with a no-training commitment.
>
> Enforced in code, not in a wiki. Phase 4 ships a startup assertion in `ai-service`:
> if `ENV == "production"` and `provider.tier != "paid"`, the process refuses to boot.

Local Ollama is exempt on privacy grounds — nothing leaves the machine — which is a
strong argument for making it the default dev provider over any hosted free tier.

### Tier 2 — Production (paid)

Route by job, not by habit. Per million tokens:

| Job | Model | Model ID | In / Out |
|---|---|---|---|
| Intent classification, memory extraction, summaries | Claude Haiku 4.5 | `claude-haiku-4-5` | $1 / $5 |
| Standard chat, daily horoscopes | Claude Sonnet 5 | `claude-sonnet-5` | $2 / $10 |
| Deep chart interpretation, premium reports | Claude Opus 5 | `claude-opus-5` | $5 / $25 |

Three cost levers that matter enormously for this product:

1. **Prompt caching.** The astrology rule corpus + system prompt + persona is a large,
   byte-stable prefix reused on every request. Place it before the last
   `cache_control` breakpoint, volatile content after. Highest-leverage optimisation
   in the system.
2. **Batch API (50% off).** Daily horoscope generation is a nightly job with no
   latency requirement — a textbook fit. Phase 6.
3. **Effort control.** `output_config={"effort": "low"}` for classification, `"high"`
   for a paid deep reading. Don't pay reasoning prices for a 21-way label.

Use adaptive thinking (`thinking={"type": "adaptive"}`) on interpretation calls; stream
everything user-facing.

### Everything else, free in development

| Need | Free local | Free hosted tier |
|---|---|---|
| PostgreSQL + pgvector | `pgvector/pgvector:pg16` Docker | Neon, Supabase |
| Redis | `redis:7-alpine` Docker | Upstash |
| Object storage | MinIO Docker | Cloudflare R2 |
| Geocoding | **GeoNames dump in your own Postgres** — recommended | Nominatim (strict policy) |
| Timezone lookup | `timezonefinder` (Python, offline) | — |
| SMS OTP | Log to stdout in dev | — (SMS costs money everywhere) |
| Email | Mailpit Docker | Resend, Brevo free tiers |
| Payments | Razorpay / Stripe **test mode** | — |
| Push | Firebase Cloud Messaging | free |
| STT | `faster-whisper` (Python, local) | — |
| TTS | Piper (local) | — |
| Realtime A/V | LiveKit, self-hosted | — |
| Analytics | PostHog self-hosted | PostHog free tier |
| Error tracking | — | Sentry free tier |

Self-hosting the GeoNames dump removes an external dependency from your most
latency-sensitive onboarding step, costs nothing, and has no rate limit. Phase 2
covers the import.

---

## The Claude Code development harness

The product is AI-heavy, so there are **two** AI layers and they must not be confused:

```
┌──────────────────────────────────────────────┐
│  DEVELOPMENT HARNESS                         │
│  Controls the coding agent while BUILDING    │
└───────────────────────┬──────────────────────┘
                        ▼
┌──────────────────────────────────────────────┐
│  THE PRODUCT                                 │
│  ┌────────────────────────────────────────┐  │
│  │ AI Runtime · AI Safety · AI Evaluation │  │
│  └────────────────────────────────────────┘  │
└──────────────────────────────────────────────┘
```

The development harness lives in `.claude/` and is built in Phase 0:

```
.claude/
├── CLAUDE.md                  the constitution of the project
├── architecture.md
├── rules/
│   ├── go.md                  module boundaries, error wrapping, context, sqlc
│   ├── python.md              typing, Pydantic, async, no LLM in astro-service
│   ├── frontend.md
│   ├── database.md            migrations, single-writer rule, indexes
│   ├── ai.md                  THE DETERMINISM RULE, prompt versioning, PII rule
│   ├── security.md
│   └── testing.md
├── agents/    architect · go-backend · python-ai · frontend · qa · security
├── workflows/ feature · bugfix · phase-gate
└── state/     current-phase.md · current-task.md
```

The product's own eval harness (`services/ai/evals/`) is built in Phase 6 and is a
different thing entirely — it tests the astrologer, not the coder.

### Standing rules for the coding agent

1. Do not build multiple phases at once.
2. Do not invent astrology calculations. They come from `astro-service`.
3. **`astro-service` must never gain an LLM client, an API key or model egress.**
4. **Only `api-service` writes to the database.**
5. Do not hardcode secrets.
6. Do not over-engineer — three services is the ceiling until traffic says otherwise.
7. Do not introduce a new technology without an ADR.
8. Money is `int64` paise in Go, `int` paise in Python. Never a float, anywhere.
9. Write tests for business logic; astrology maths gets golden-file tests.
10. Treat privacy and deletion as first-class features.
11. Never expose system prompts, internal reasoning or hidden context to users.
12. Update `docs/PROJECT_STATUS.md` at the end of every task.

### Living documents (maintained continuously, from Phase 0)

`docs/PROJECT_STATUS.md` · `docs/ARCHITECTURE.md` · `docs/DECISIONS.md` ·
`docs/ROADMAP.md` · `docs/decisions/NNN-*.md`

ADR format: Decision · Context · Options considered · Chosen option · Reason · Tradeoffs · Status.

---

## Global Definition of Done

A feature is not complete until **all** of these hold:

- [ ] Implementation complete
- [ ] `go build ./...` and `go vet ./...` clean
- [ ] `mypy --strict` clean on touched Python
- [ ] `tsc --noEmit` clean on touched TypeScript
- [ ] `golangci-lint`, `ruff` and `eslint` all pass
- [ ] Unit tests pass in every language touched
- [ ] Integration tests pass (where applicable)
- [ ] E2E tests pass (where applicable)
- [ ] Cross-service contract regenerated and committed if any API changed
- [ ] Error, loading and empty states handled
- [ ] Mobile responsive
- [ ] Accessibility considered
- [ ] Security reviewed against the phase checklist
- [ ] Analytics events emitted
- [ ] Documentation updated

---

## Design language

The product should feel **modern, premium, mystical, trustworthy, minimal, warm** —
and explicitly *not* like a cheap astrology app.

| Token | Value |
|---|---|
| Base | Midnight navy `#0B1026` |
| Surface | `#141B35` |
| Accent | Deep purple `#6B4FBB` |
| Highlight | Gold `#D4A857` |
| Text | `#F2F3F8` / muted `#9AA3C0` |
| Success / Warn / Error | `#4ADE80` / `#FBBF24` / `#F87171` |

Celestial motifs (stars, orbits, soft gradients) as *texture*, never clutter. Subtle
motion only, honouring `prefers-reduced-motion`. It should read as a premium
technology product that happens to be about astrology.

---

## North star

```
                 PERSONAL AI ASTROLOGER
                          │
          ┌───────────────┼───────────────┐
       YOUR CHART      YOUR LIFE      YOUR HISTORY
          └───────────────┼───────────────┘
                    AI COMPANION
              ┌───────────┴───────────┐
          AI GUIDANCE            HUMAN EXPERT
              └───────────┬───────────┘
                  PERSONALIZED JOURNEY
```
