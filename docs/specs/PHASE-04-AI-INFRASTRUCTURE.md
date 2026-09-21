# Phase 4 — AI Infrastructure (Python)

| | |
|---|---|
| **Goal** | Build the AI plumbing properly, on free local models, before any user-facing AI ships. |
| **Deliverable** | `ai-service`: a provider-agnostic LLM layer, versioned prompt registry, intent classifier, model router, safety classifier and full cost/token telemetry — running on Ollama at zero cost, swappable to Claude with one env var. |
| **Depends on** | Phase 3 |
| **Unlocks** | Phase 5 |
| **Estimated size** | 10–14 days |
| **Cost to run** | ₹0 in development. This entire phase runs on local free models. |

> This is the phase where the free-model requirement is actually satisfied. Everything
> here is designed so that development and CI never touch a paid API, and production is
> a configuration change rather than a rewrite.
>
> Python carries this phase for a reason: every LLM, embedding and evaluation library in
> this space is Python-first. Go's role is to call it, persist what it returns, and
> enforce quotas.

---

## 1. Scope

### In scope (`ai-service`)
- `LLMProvider` protocol + adapters: OpenAI-compatible (covers Ollama, Groq, OpenRouter, Cerebras, LM Studio), Google, Mock
  — Anthropic was built, tested and then removed; see [ADR-011](../decisions/011-remove-anthropic-adapter.md)
- `EmbeddingProvider` protocol + adapters
- Model router: job type → model tier → concrete model
- Prompt registry with immutable versioning and composable modules
- Intent classification (the 21-label taxonomy)
- Safety classifier + output validation
- Retry, timeout, circuit breaker, provider fallback
- **The production PII guard**
- Telemetry *reported* to Go in every response envelope

### In scope (`api-service`)
- Persisting `ai_request_logs` — Python is read-only, so Go writes
- Admin endpoints for model/prompt configuration
- Rate limiting the internal AI path

### Out of scope
- User-facing chat (Phase 5) — nothing is exposed to users here
- RAG / knowledge base (Phase 5)
- Memory (Phase 6)

---

## 2. The provider abstraction

Nothing outside `app/providers/` may import a vendor SDK. Enforced by an
`import-linter` contract in CI, not by convention — conventions erode.

```python
# app/providers/base.py
from typing import Protocol, AsyncIterator, Literal

ModelTier = Literal["fast", "chat", "deep"]
ProviderTier = Literal["local", "free-hosted", "paid"]


class CompletionRequest(BaseModel):
    messages: list[Message]
    system: list[SystemBlock] = []      # list so cacheable prefixes stay separable
    tier: ModelTier
    max_tokens: int = 4096
    json_schema: dict | None = None
    stop_sequences: list[str] = []
    metadata: RequestMetadata            # trace_id, user_id, prompt_version


class Usage(BaseModel):
    input_tokens: int
    output_tokens: int
    cached_input_tokens: int = 0
    cost_micros: int = 0                 # INTEGER micro-units. Never a float.


class CompletionResponse(BaseModel):
    text: str
    finish_reason: Literal["stop", "length", "refusal", "error"]
    usage: Usage
    model: str
    provider_id: str
    latency_ms: int


class LLMProvider(Protocol):
    id: str
    tier: ProviderTier                   # ← the PII guard reads this
    capabilities: Capabilities

    async def complete(self, req: CompletionRequest) -> CompletionResponse: ...
    def stream(self, req: CompletionRequest) -> AsyncIterator[CompletionChunk]: ...
    async def health_check(self) -> bool: ...
```

A `Protocol` rather than an ABC: adapters need no common base class, and `mypy --strict`
verifies conformance structurally at every call site.

### The four adapters

**1. `OpenAICompatibleProvider`** — the workhorse. One adapter, many backends, because
the official `openai` Python SDK speaks to all of them:

| Backend | `LLM_BASE_URL` | Tier | Cost |
|---|---|---|---|
| Ollama (default dev) | `http://localhost:11434/v1` | `local` | free |
| LM Studio | `http://localhost:1234/v1` | `local` | free |
| Groq | `https://api.groq.com/openai/v1` | `free-hosted` | free tier |
| OpenRouter | `https://openrouter.ai/api/v1` | `free-hosted` | `:free` models |
| Cerebras | `https://api.cerebras.ai/v1` | `free-hosted` | free tier |

```python
class OpenAICompatibleProvider:
    def __init__(self, base_url: str, api_key: str, tier: ProviderTier, models: ModelMap):
        self._client = AsyncOpenAI(base_url=base_url, api_key=api_key or "not-needed")
        self.tier = tier
```

Building this one adapter gets you five backends. Highest-leverage code in the phase.

**2. `AnthropicProvider`** — production.

```python
from anthropic import AsyncAnthropic

res = await self._client.messages.create(
    model="claude-sonnet-5",
    max_tokens=4096,
    thinking={"type": "adaptive"},
    output_config={"effort": "medium"},
    system=[
        # stable prefix — cached
        {"type": "text", "text": astrology_rules,
         "cache_control": {"type": "ephemeral"}},
        # volatile — after the breakpoint
        {"type": "text", "text": user_chart_context},
    ],
    messages=messages,
)
```

Three things this adapter must do that the generic one doesn't:

- **Prompt caching.** Put the byte-stable astrology rule corpus and persona before the
  last `cache_control` breakpoint; the user's chart and question after it. For this
  product the stable prefix is large and reused on every single request — the biggest
  cost lever you have.
- **Effort per tier.** `low` for classification, `medium` for chat, `high` for a paid
  deep reading. Don't pay deep-reasoning prices for a 21-way label.
- **`stop_reason == "refusal"`.** Check it before reading content; surface it as a
  graceful "let's approach this differently", never as an error.

**3. `GoogleProvider`** — the most generous free hosted tier, and free embeddings.
Useful as a fallback when the dev machine is loaded, and for CI smoke tests.

**4. `MockProvider`** — deterministic canned responses keyed by prompt hash, loaded
from `tests/fixtures/ai/`. **CI uses this exclusively.** Tests depending on a real model
are slow, flaky, non-deterministic and eventually expensive. A mock returning a fixture
makes the entire pipeline testable in milliseconds.

### Provider registry and fallback

```python
chain = [primary, *fallbacks]     # [ollama, groq] in dev · [anthropic, google] in prod
```

Circuit breaker per provider: 5 consecutive failures → open 60 s → half-open probe.
Retry with exponential backoff and jitter on 429 and 5xx; **never** retry a 400. On
exhaustion raise `AIUnavailableError`, which Go maps to a 503 with a retryable flag so
the UI shows a real message rather than a spinner that never resolves.

---

## 3. ⚠️ The production PII guard

The most important 15 lines in this phase.

```python
# app/guards/pii.py

def assert_provider_allowed(provider: LLMProvider, env: str) -> None:
    if env == "production" and provider.tier != "paid":
        raise RuntimeError(
            f'Refusing to start: provider "{provider.id}" is tier "{provider.tier}". '
            f"Production requires a paid provider with a no-training commitment, "
            f"because requests carry birth data and personal conversation content."
        )
```

Called at FastAPI startup (`lifespan`), and again per request in the orchestrator.
Reasoning:

- Birth date + birth time + birth place is effectively a unique identifier
- Conversation content covers health, marriage, money and family
- Most free API tiers reserve the right to train on inputs

So: **free models for development and CI, paid provider in production, enforced by the
process refusing to boot.** A wiki page is not an enforcement mechanism.

Local Ollama is exempt from the privacy concern — nothing leaves the machine — but is
still blocked in production for reliability. The single `tier != "paid"` check covers
both cases.

---

## 4. Model routing

```python
ROUTING: dict[JobType, ModelTier] = {
    JobType.INTENT_CLASSIFICATION: "fast",
    JobType.SAFETY_CLASSIFICATION: "fast",
    JobType.MEMORY_EXTRACTION:     "fast",
    JobType.CONVERSATION_SUMMARY:  "fast",
    JobType.SUGGESTED_QUESTIONS:   "fast",
    JobType.DAILY_HOROSCOPE:       "chat",
    JobType.CHAT_RESPONSE:         "chat",
    JobType.CHART_INTERPRETATION:  "deep",
    JobType.PREMIUM_REPORT:        "deep",
    JobType.COMPATIBILITY:         "deep",
}
```

| Tier | Dev (free, local) | Production | Prod cost /1M |
|---|---|---|---|
| `fast` | `llama3.2:3b` | `claude-haiku-4-5` | $1 in / $5 out |
| `chat` | `qwen2.5:7b` | `claude-sonnet-5` | $2 in / $10 out |
| `deep` | `qwen2.5:7b` (or `14b`) | `claude-opus-5` | $5 in / $25 out |

The mapping lives in config, overridable per-environment and per-job from the Go admin
panel, so you can A/B a tier change without a deploy.

**Measure before you cascade.** The cheapest model that holds quality on a given route
is an empirical question, and the answer differs per route. Phase 6's eval harness turns
this table from a guess into a measurement. Until then it is a starting configuration,
not a conclusion.

---

## 5. Prompt registry

Prompts are **versioned, immutable artifacts**. You cannot debug a bad response from
three weeks ago if the prompt that produced it has been edited since.

```
services/ai/app/prompts/
├── registry.py
├── modules/
│   ├── system_base.v1.md
│   ├── astrology_rules.v1.md        ← large, byte-stable → the cache prefix
│   ├── safety_rules.v1.md
│   ├── output_format.v1.md
│   └── personas/
│       ├── vedic_guide.v1.md
│       ├── career_guide.v1.md
│       ├── relationship_guide.v1.md
│       └── spiritual_guide.v1.md
└── composed/
    ├── intent_classification.v1.py
    ├── chat_response.v1.py
    ├── memory_extraction.v1.py
    └── daily_horoscope.v1.py
```

**Rule: never edit a published version.** `chat_response.v1` is frozen; improvements
become `v2`. A test asserts this by comparing a committed hash of every published
module — editing one fails CI. The registry resolves the active version from config, so
rollback is a config change and A/B testing is a percentage split.

### Composable construction

```python
prompt = (
    PromptBuilder()
    .add("system_base", "v1")            # ┐
    .add("astrology_rules", "v1")        # ├─ stable prefix, cached
    .add("safety_rules", "v1")           # │
    .persona(user.persona, "v1")         # ┘
    .cache_breakpoint()                  # ←── everything above is cached
    .chart_context(chart_ctx)            # ┐
    .user_context(user_ctx)              # ├─ volatile
    .conversation_context(summary, recent)#│
    .knowledge_context(rag_docs)         # │
    .user_question(message)              # ┘
    .build()
)
```

The ordering is deliberate: stable content first, volatile last. A single byte changing
early in the prefix invalidates everything downstream. Put the timestamp at the end,
never at the beginning.

Every response records `{model, provider_id, prompt_version, context_version}`. Without
this, "why did the AI say that?" is unanswerable.

---

## 6. Intent classification

Every user message is classified first — this drives context selection, prompt choice,
model tier and safety routing.

```
GENERAL_ASTROLOGY  CAREER      RELATIONSHIP    MARRIAGE     FINANCE
EDUCATION          FAMILY      TRAVEL          RELOCATION   DAILY_HOROSCOPE
KUNDLI             DASHA       TRANSIT         COMPATIBILITY TAROT
NUMEROLOGY         HUMAN_ASTROLOGER            EMOTIONAL_SUPPORT
MEDICAL            LEGAL       OTHER
```

```python
class IntentResult(BaseModel):
    primary: Intent
    secondary: Intent | None = None
    confidence: float = Field(ge=0, le=1)
    entities: Entities                    # timeframe, person, topic
    requires_safety_review: bool
```

**Hybrid classification, and this matters for cost.** Run a keyword/regex pre-pass
first: roughly 40% of real messages ("what's my career look like", "when will I get
married") are unambiguous and never need a model call. Only ambiguous messages reach the
`fast` tier. On free local models this saves latency; in production it saves real money
at the highest-volume call site in the system.

Structured output via JSON schema where the provider supports it; validated with
Pydantic regardless. `confidence < 0.6` → fall back to `GENERAL_ASTROLOGY` with broad
context rather than guessing narrowly and retrieving the wrong chart facts.

**Build a labelled test set of ~200 messages now** — it is the classifier's regression
suite, it costs nothing to run against a local model, and it is what lets you swap
models later with confidence.

---

## 7. Safety

Three layers, in order.

### Layer 1 — Input classification (before any generation)

| Category | Action |
|---|---|
| Self-harm / crisis | **Bypass astrology entirely.** Supportive message + region-appropriate helpline. Never predict. |
| Medical | Cultural/spiritual framing only; direct to a qualified professional |
| Legal | Same posture |
| Prompt injection | Strip/neutralise; user text is never interpolated into the system section |
| Abuse / harassment | Decline, log, rate-limit |

Crisis detection is not a place for cleverness. Keyword list plus classifier, biased
heavily toward false positives, routed to a **static, human-written** response. An
astrological reading is never the right answer to a crisis message.

### Layer 2 — Prompt-level rules (`safety_rules.v1.md`)

Never state as certain: medical diagnosis · guaranteed financial returns · guaranteed
marriage or pregnancy outcomes · death predictions · legal outcomes.

Always: traditional framing ("this combination is traditionally read as…"), not
predictive framing ("you will…").

### Layer 3 — Output validation (after generation, before the user sees it)

```python
class Violation(BaseModel):
    type: Literal["unsupported_certainty", "medical_claim", "fabricated_chart_fact",
                  "prompt_leak", "guaranteed_outcome"]
    severity: Literal["warn", "block"]
    excerpt: str
```

**`fabricated_chart_fact` is the one unique to this product.** The validator extracts
every astrological claim in the response ("Saturn in your 10th house") and checks it
against the `fact_index` supplied with the chart context. A mismatch is a hard block —
this is the automated enforcement of the determinism principle, and the check that
catches an LLM quietly inventing a planetary position.

A `block` triggers one regeneration with a corrective instruction; a second failure
returns a graceful fallback and raises a safety incident for admin review.

`prompt_leak` scans for fragments of the system prompt in output.

---

## 8. Telemetry — reported by Python, written by Go

`ai-service` holds a **read-only** database role, so it cannot write its own logs. That
constraint turns out to be a feature: Python returns telemetry in the response envelope
and Go persists it, keeping one writer and one transaction boundary.

```python
class AIResponseEnvelope(BaseModel):
    result: Any
    telemetry: Telemetry     # Go writes this to ai_request_logs
```

```sql
CREATE TABLE ai_request_logs (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    trace_id         TEXT NOT NULL,
    user_id          UUID,
    conversation_id  UUID,
    job_type         TEXT NOT NULL,
    intent           TEXT,
    provider_id      TEXT NOT NULL,
    model            TEXT NOT NULL,
    tier             TEXT NOT NULL,
    prompt_version   TEXT NOT NULL,
    context_version  TEXT NOT NULL,
    input_tokens     INTEGER NOT NULL,
    output_tokens    INTEGER NOT NULL,
    cached_tokens    INTEGER NOT NULL DEFAULT 0,
    latency_ms       INTEGER NOT NULL,
    cost_micros      BIGINT  NOT NULL DEFAULT 0,   -- integer. Never float.
    finish_reason    TEXT NOT NULL,
    safety_flags     JSONB   NOT NULL DEFAULT '[]',
    validation_passed BOOLEAN NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ai_logs_user_idx ON ai_request_logs (user_id, created_at DESC);
CREATE INDEX ai_logs_job_idx  ON ai_request_logs (job_type, created_at DESC);
```

**Money as integers.** `cost_micros` is `BIGINT` micro-currency units in Postgres and
`int` in Python. Floats accumulate rounding error, and this table is what Phase 7's
usage billing reads. Get it right here and Phase 7 is straightforward; get it wrong and
you reconcile invoices by hand forever.

`cost_micros` is 0 for local free models, which makes the dev/prod cost delta visible in
one table rather than requiring a separate code path.

Dashboards: p50/p95 latency by job type · token volume by tier · **cost per completed
conversation** (not per request — a cheap request needing three retries isn't cheap) ·
cache hit rate · validation failure rate · provider error rate.

---

## 9. The orchestrator

```
POST /v1/complete          (internal only; Phase 5 adds the user-facing chat route)

 1. Verify X-Internal-Token
 2. Assert PII guard for the resolved provider
 3. Keyword pre-pass → maybe skip classification entirely
 4. Classify intent (fast tier)
 5. Safety classify input → crisis short-circuit
 6. Select prompt version + persona
 7. Build context   (Phase 5 fills chart/RAG; stubs return empty here)
 8. Compose prompt via PromptBuilder
 9. Call provider (retry · circuit breaker · fallback)
10. Validate output
11. Return result + telemetry envelope
```

Step 7's context builders are **Protocols with stub implementations** in this phase.
Phase 5 fills them in. Defining the seams now makes Phase 5 additive rather than a
refactor.

---

## 10. Admin endpoints (Go, proxying to Python)

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/v1/admin/ai/config` | Current provider, model per tier, prompt versions |
| `PATCH` | `/api/v1/admin/ai/config` | Change routing/versions without a deploy |
| `GET` | `/api/v1/admin/ai/prompts` | List prompts + versions |
| `GET` | `/api/v1/admin/ai/prompts/{key}/{version}` | View a frozen version |
| `GET` | `/api/v1/admin/ai/usage` | Tokens, cost, latency, grouped |
| `GET` | `/api/v1/admin/ai/incidents` | Safety incidents |
| `POST` | `/api/v1/admin/ai/test` | Run a prompt against any provider — the playground |

`SUPER_ADMIN` only, all audit-logged in Go. The playground is how you compare a free
local model against Claude on the same prompt side by side, which is exactly the
workflow this phase is built for.

---

## 11. Environment variables

### `ai-service`

```bash
ENV=development

LLM_PROVIDER=openai-compatible          # openai-compatible | google | mock
LLM_BASE_URL=http://localhost:11434/v1
LLM_API_KEY=
LLM_PROVIDER_TIER=local                 # local | free-hosted | paid ← guard reads this

LLM_MODEL_FAST=llama3.2:3b
LLM_MODEL_CHAT=qwen2.5:7b
LLM_MODEL_DEEP=qwen2.5:7b

LLM_FALLBACK_PROVIDER=                  # e.g. google (empty in dev)
LLM_FALLBACK_API_KEY=

EMBEDDING_PROVIDER=ollama
EMBEDDING_MODEL=nomic-embed-text
EMBEDDING_DIM=768

LLM_TIMEOUT_SECONDS=60
LLM_MAX_RETRIES=2
LLM_CIRCUIT_BREAKER_THRESHOLD=5
LLM_CIRCUIT_BREAKER_RESET_SECONDS=60

PROMPT_VERSION_CHAT=v1
PROMPT_VERSION_INTENT=v1
PROMPT_VERSION_MEMORY=v1
DEFAULT_PERSONA=vedic_guide

SAFETY_VALIDATION_ENABLED=true
SAFETY_BLOCK_ON_FABRICATED_FACT=true
CRISIS_HELPLINE_REGION=IN
```

**Production `.env` looks like this instead** — and nothing else changes:

```bash
LLM_PROVIDER=anthropic
LLM_PROVIDER_TIER=paid
LLM_API_KEY=${ANTHROPIC_API_KEY}
LLM_MODEL_FAST=claude-haiku-4-5
LLM_MODEL_CHAT=claude-sonnet-5
LLM_MODEL_DEEP=claude-opus-5
```

That diff is the whole point of this phase.

### `api-service`

```bash
AI_TIMEOUT=90s
AI_MAX_RETRIES=1                        # Python already retries the provider
AI_RATE_LIMIT_PER_USER_HOUR=60
```

---

## 12. Task list

| # | Service | Task | Done when |
|---|---|---|---|
| 4.1 | Py | `LLMProvider` / `EmbeddingProvider` protocols + registry | `mypy --strict` clean; import-linter blocks vendor imports outside `providers/` |
| 4.2 | Py | `OpenAICompatibleProvider` (complete + stream) | Works against local Ollama and Groq free tier |
| 4.3 | Py | `OllamaEmbeddingProvider` | Returns 768-dim vectors |
| 4.4 | Py | ~~`AnthropicProvider`~~ — **removed, [ADR-011](../decisions/011-remove-anthropic-adapter.md)** | Built and tested during the phase, then deleted: no Anthropic subscription, and no free tier exists |
| 4.5 | Py | `GoogleProvider` | Free-tier fallback works |
| 4.6 | Py | `MockProvider` + `tests/fixtures/ai/` | CI runs the full pipeline with zero network calls |
| 4.7 | Py | **PII guard** | `ENV=production` + `tier=local` refuses to start |
| 4.8 | Py | Model router + per-env overrides | Changing a tier mapping needs no deploy |
| 4.9 | Py | Retry, timeout, circuit breaker, fallback chain | Killing Ollama mid-request falls back cleanly |
| 4.10 | Py | Prompt registry, immutable versions, `PromptBuilder` | Editing a published module fails the hash test |
| 4.11 | Py | Cache-breakpoint ordering + prefix-stability test | Two identical requests produce a byte-identical prefix |
| 4.12 | Py | Intent classifier + keyword pre-pass | ≥85% accuracy on the 200-message set |
| 4.13 | Py | Safety input classifier + crisis short-circuit | Crisis phrasing never reaches the astrology path |
| 4.14 | Py | Output validator incl. `fabricated_chart_fact` | A response naming an absent planet is blocked |
| 4.15 | Py | Orchestrator with stubbed context builders + telemetry envelope | Internal call succeeds end to end |
| 4.16 | Py | OpenAPI export for the AI service | `task contracts` generates a compiling Go client |
| 4.17 | Go | `ai_request_logs` migration + persistence from the envelope | Every call logged with cost, tokens, latency |
| 4.18 | Go | Typed AI client with timeout and rate limiting | |
| 4.19 | Go | Admin config, usage, incidents, playground endpoints | |
| 4.20 | Py | 200-message labelled intent dataset | Committed, synthetic |
| 4.21 | Py | Provider parity test suite | Same request across mock/ollama/google is schema-valid |

---

## 13. Testing

**Unit (Python)** — prompt composition order and determinism; cache-breakpoint
placement; router mapping; retry/backoff maths; circuit-breaker state machine; cost
arithmetic (integer, no float); Pydantic validation of every structured output.

**Integration (MockProvider only — no network)** — full orchestrator pipeline; crisis
short-circuit; fabricated-fact block; fallback chain on primary failure; telemetry
envelope shape.

**Provider parity** — one suite run against every adapter, asserting schema-valid output
and consistent exception types. Mock and Ollama runs are free and run in CI; Anthropic
and Google runs are manual, on demand.

**Classifier accuracy** — the 200-message set against the local free model. Target ≥85%
primary-intent accuracy. Cheap, repeatable, free — and what gives you confidence to
change models later.

**Guard tests** — assert the process refuses to start in every disallowed
provider/environment combination. Test the negative case; a guard never observed to fire
is a guard you should not trust.

**Go integration** — envelope telemetry lands in `ai_request_logs` correctly; a Python
5xx maps to a clean 503 with a retryable flag; rate limiting fires.

---

## 14. Security checklist

- [ ] API keys from env only; never logged, never in error messages, never in telemetry
- [ ] `LLM_API_KEY` absent from every log line and Sentry payload in both services
- [ ] **PII guard blocks non-paid providers in production — tested**
- [ ] User input never interpolated into the system prompt section
- [ ] Prompt-injection attempts flagged and neutralised
- [ ] System prompts never returned to clients, including in error responses
- [ ] `prompt_leak` validation active on all output
- [ ] `ai_request_logs` stores IDs and token counts — **never message content**
- [ ] `ai-service` not publicly reachable; `X-Internal-Token` required
- [ ] `ai-service` DB role remains read-only (regression-tested)
- [ ] Admin AI routes `SUPER_ADMIN` only, audit-logged in Go
- [ ] Playground cannot be pointed at real user data
- [x] Rate limiting on the internal completion path (a runaway loop is a real cost event)
      `ratelimit.AIPlaygroundPerAdmin` — 20 per 5 minutes, keyed on the
      SUPER_ADMIN's user id. **This was ticked in the gate table while the route
      had no limit of its own**: it inherited only `GlobalPerIP` at 1200/minute,
      the backstop sized for ordinary API traffic, which at this service's prompt
      size is roughly 1.7 million tokens a minute. Fails CLOSED, unlike the
      global throttle — those protect availability, this protects a bill.
- [ ] Crisis responses are static, human-written text — never model-generated

---

## 15. Risks

| Risk | Mitigation |
|---|---|
| Free local models behave differently from Claude, so dev quality misleads | Phase 6's eval harness runs against both; the playground makes side-by-side comparison routine |
| Ollama too slow on the dev machine | Groq free tier via the same adapter — one env var |
| Embedding dimension changes on provider swap | `EMBEDDING_DIM` is config; Phase 5 documents the re-embedding migration |
| Prompt caching silently stops working in prod | Assert `cached_input_tokens > 0` in the prod smoke test; alert if the hit rate drops |
| Someone points production at a free tier to save money | The guard makes the process refuse to boot. Not a policy — a crash. |
| Telemetry lost because Python can't write it | Go persists it transactionally alongside the message write in Phase 5; a failed write is logged and retried, never silently dropped |
| Over-engineering the abstraction | Four adapters, one protocol, no plugin system. Resist anything more elaborate. |

---

## 16. Definition of Done

Global DoD **plus**:

- [ ] Whole AI stack runs on local free models at zero cost
- [x] ~~Switching to Claude requires changing only env vars~~
      **Superseded by [ADR-011](../decisions/011-remove-anthropic-adapter.md).**
      The property the line asks for — swapping provider without touching code —
      holds and is tested: `LLM_PROVIDER` selects the adapter, and any
      OpenAI-compatible vendor (Groq, Cerebras, OpenRouter, a local Ollama) needs
      only `LLM_BASE_URL` and a key. It is Claude specifically that now needs an
      adapter written, because there is no Anthropic subscription and no free tier.
- [ ] CI runs the full pipeline with `MockProvider` and makes no network calls
- [ ] PII guard verified to block production + non-paid provider
- [ ] Every AI call logged by Go with model, prompt version, tokens, latency, integer cost
- [ ] Intent classifier ≥85% on the labelled set
- [ ] Output validator blocks fabricated chart facts

---

## 17. Phase Gate 🔒

- [ ] `LLMProvider` implemented by all four adapters, all passing the parity suite
- [ ] Ollama runs `fast`, `chat` and `deep` tiers locally at zero cost
- [x] ~~`AnthropicProvider` verified once against a real key, incl. prompt caching~~
      **Superseded by [ADR-011](../decisions/011-remove-anthropic-adapter.md).**
      The adapter is removed; the production provider is deferred to Phase 7.
      The line's purpose — verify an adapter against a real vendor once, rather
      than against a response shape we wrote down — is met by
      `scripts/verify_provider.py openai-compatible`, run against Groq.
      The prompt-caching half was never satisfiable here: the stable prefix is
      ~770 tokens against the ~1024 minimum below which `cache_control` is
      ignored outright, so there was no cache behaviour to observe even with a
      key. Phase 5's corpus crosses that line and a test fails on the day it does.
- [ ] `MockProvider` powers all CI; no CI job makes a network call to a model
- [ ] **PII guard blocks `production` + non-paid provider — test proves it**
- [ ] Model router maps all 10 job types; overridable from admin without deploy
- [ ] Prompt registry immutable; editing a published module fails CI
- [ ] Cache breakpoint ordering verified by a prefix-stability test
- [ ] Intent classifier ≥85% on 200 labelled messages, keyword pre-pass working
- [ ] Crisis input short-circuits to a static human-written response
- [ ] Output validator blocks fabricated chart facts, unsupported certainty and prompt leaks
- [ ] Telemetry envelope persisted by Go; `cost_micros` is an integer
- [ ] Retry, circuit breaker and fallback verified by killing the primary provider mid-run
- [ ] Generated Go client compiles from the AI service OpenAPI; contract diff passes
- [ ] Admin config, usage, incidents and playground all working
- [ ] No message content in `ai_request_logs`
- [ ] `ai-service` DB role still read-only
- [ ] `task verify` green
- [ ] `docs/PROJECT_STATUS.md` and `.claude/state/current-phase.md` updated
