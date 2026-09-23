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
```

**Production `.env` looks like this instead** — and nothing else changes:

```bash
# ⚠️ SUPERSEDED 2026-09-23 — this block no longer boots.
# `Settings.llm_provider` is Literal["openai-compatible", "google", "mock"],
# so `anthropic` is rejected by pydantic at startup. Kept visible rather
# than deleted because ADR-011 is a decision worth being able to see the
# shape of. Do not copy this.
#
# LLM_PROVIDER=anthropic
# LLM_PROVIDER_TIER=paid
# LLM_API_KEY=${ANTHROPIC_API_KEY}
# LLM_MODEL_FAST=claude-haiku-4-5
# LLM_MODEL_CHAT=claude-sonnet-5
# LLM_MODEL_DEEP=claude-opus-5

# What production actually takes, as of ADR-011. `openai-compatible`
# with a declared `paid` tier is the ONLY production-legal
# configuration: `google` and `mock` declare their own non-paid tiers
# and the guard refuses them whatever the setting claims.
#
# The declaration is load-bearing and unverifiable — this adapter
# reaches a paid inference host and a laptop's Ollama through the same
# code path, so there is no vendor identity to infer a tier from. The
# guard cannot see LLM_BASE_URL. Point it somewhere you pay.
LLM_PROVIDER=openai-compatible
LLM_PROVIDER_TIER=paid
LLM_BASE_URL=https://<a-vendor-you-pay>/v1
LLM_API_KEY=${LLM_API_KEY}
# Tier models: leave unset and each follows DEFAULT_MODELS for the
# provider. Pinned here, one value wins for every provider.
```

That diff is the whole point of this phase. **The choice of production
vendor is deferred to Phase 7** — ADR-011 removed the only paid adapter
and nothing has replaced it, so today this block describes a shape
rather than a deployment.

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

**Ticked 2026-09-23 against a re-runnable command each**, after an execution audit
found five of these false. The command is named on every line; the evidence and the
defects it found are in [`docs/PHASE-04-GATE.md`](../PHASE-04-GATE.md).

- [x] API keys from env only; never logged, never in error messages, never in telemetry
      **Was false.** `_scrub_event` scanned `llm_api_key` and `internal_token` only;
      `llm_fallback_api_key` reached Sentry verbatim — the credential used precisely
      when the primary is failing, which is when events are collected. Now
      parametrised over all three, so a fourth added to Settings fails a test.
      `pytest tests/test_observability.py -k every_configured_credential`
- [x] `LLM_API_KEY` absent from every log line and Sentry payload in both services
      `pytest tests/test_observability.py -q`
- [x] **PII guard blocks non-paid providers in production — tested**
      `pytest tests/test_guards.py -q`. Note the accepted limit, now pinned by
      `TestTheGuardCannotSeeABaseURL`: the guard reads the declared tier and cannot
      see `LLM_BASE_URL`, so `openai-compatible` + `paid` boots against a free
      endpoint. Deliberate per ADR-011 — that adapter has no vendor identity to infer
      from — and no longer undocumented in the suite.
- [x] User input never interpolated into the system prompt section
      `pytest tests/test_orchestrator.py -k never_in_the_system_prompt`
- [x] Prompt-injection attempts flagged and neutralised
      **Was false in the half that matters.** NEUTRALISE worked; FLAGGING rested
      entirely on the model screener, and every test fed it a stubbed verdict — so it
      was asserted against a mock of itself and did not work at all during a provider
      outage. `app/safety/injection.py` is the offline pass, shaped like the crisis
      one and applied only as an upgrade from NONE so a CRISIS verdict cannot be
      downgraded. `pytest tests/test_safety.py -k Injection`
- [x] System prompts never returned to clients, including in error responses
      **Was false on a 200.** Error responses were clean; the corrective retry
      returned the internal correction text to the user as their reading. See the
      next line — same fix.
- [x] `prompt_leak` validation active on all output
      **Was false.** The validator was built once from the FIRST prompt and re-used
      on the regenerated answer, which was produced from a different one.
      `pytest tests/test_orchestrator.py -k parrots_the_correction`
- [x] `ai_request_logs` stores IDs and token counts — **never message content**
      No content column exists, so it cannot leak what it cannot store.
      `go test ./internal/ailogs/... -count=1`, and the schema itself in
      `services/api/db/migrations/000006_ai_request_logs.up.sql` — which is committed
      and readable with the stack down, unlike a `psql` session.
      (This line first cited `psql -c "\d ai_request_logs"`. That is not runnable:
      `psql` is not installed on the host here, and the container form needs the
      stack up. Caught by re-running every command on this page — which is the only
      thing that distinguishes a citation from a claim.)
- [x] `ai-service` not publicly reachable; `X-Internal-Token` required
      `pytest tests/test_http_surface.py -q` — constant-time compare, every route
      except the four `PUBLIC_PATHS`.
- [x] `ai-service` DB role remains read-only (regression-tested)
      Enforced by Postgres grants, not convention.
      `REQUIRE_CONTAINERS=1 go test ./internal/platform/db/... -tags=integration`
- [x] Admin AI routes `SUPER_ADMIN` only, audit-logged in Go
      `go test ./internal/httpapi/... -tags=integration -run AdminAI`. The route
      table is walked and set-compared to the test's list, so a new admin route
      cannot be added without appearing in it.
- [x] Playground cannot be pointed at real user data
      Asserted on what crosses the boundary, not on what the handler accepts.
      `go test ./internal/ailogs/... -run Playground`
- [x] Rate limiting on the internal completion path (a runaway loop is a real cost event)
      `ratelimit.AIPlaygroundPerAdmin` — 20 per 5 minutes, keyed on the
      SUPER_ADMIN's user id. **This was ticked in the gate table while the route
      had no limit of its own**: it inherited only `GlobalPerIP` at 1200/minute,
      the backstop sized for ordinary API traffic, which at this service's prompt
      size is roughly 1.7 million tokens a minute. Fails CLOSED, unlike the
      global throttle — those protect availability, this protects a bill.
- [x] Crisis responses are static, human-written text — never model-generated
      Asserted on the PROVIDER (`generator.requests == []`), not on the text: a test
      checking the response string passes while the bypass is broken, as long as
      something eventually produces the right words.
      `pytest tests/test_safety.py tests/test_orchestrator.py -q`
      **How this line got from "ticked" to actually true**, because the sequence is
      the point:

      1. The phrase list was half unprotected — 26 of 50 phrases were matched by no
         test, including `suicide`, `self-harm` and both feminine Hinglish forms.
         Deleting them left the suite green. `CRISIS_CORPUS` now pins one sentence
         per phrase.
      2. **Devanagari matched nothing at all**, and no test asserted it either way,
         so the gap was invisible rather than known. Fourteen direct forms added,
         with nuqta normalisation so `ज़िंदगी` and `जिंदगी` are one string.
      3. A native speaker reviewed the list on 2026-09-23 and confirmed every
         pattern's meaning. That closed the TRANSLATION question — and could not
         close coverage, because absent things are not on the page. Twenty-four
         generated phrasings were then run against the detector and **all
         twenty-four missed**: the Hinglish and Devanagari sides had **no method
         statements at all**, a category the English side had carried from the
         start. Twenty-two added.
      4. Seven indirect and farewell phrasings, approved individually by the same
         reviewer, with the false positives they buy measured and pinned by tests.

      The list is now 100 phrases — 70 Latin, 30 Devanagari — from 50, all Latin.

      **Still open and not closeable here:** whether these are the phrasings real
      users write is a question production data answers.
      [`docs/HINGLISH-CRISIS-REVIEW.md`](../HINGLISH-CRISIS-REVIEW.md) records what
      was reviewed, what it closed, and which two patterns to revisit first if the
      flag rate is too high. **Nobody has dialled the helpline** the static response
      points at — that remains an open item, not a done one.

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

- [x] Whole AI stack runs on local free models at zero cost
      Via `task dev:ai`, which stops the container and runs ai-service from source —
      the documented way to work on this service. All three tiers answer from Ollama
      at `cost_micros=0`; see §17 for the run and the container caveat.
      **The classifier accuracy number is the exception, and it is not a local one.**
      180/200 came from Groq's free tier, because no free LOCAL model reaches the
      ≥85% bar: 40% on `llama3.2:3b`, 60.5% on `qwen2.5:7b`. Free, but hosted. §15
      predicted exactly this and named the mitigation that was used.
- [x] ~~Switching to Claude requires changing only env vars~~
      **Superseded by [ADR-011](../decisions/011-remove-anthropic-adapter.md).**
      The property the line asks for — swapping provider without touching code —
      holds and is tested: `LLM_PROVIDER` selects the adapter, and any
      OpenAI-compatible vendor (Groq, Cerebras, OpenRouter, a local Ollama) needs
      only `LLM_BASE_URL` and a key. It is Claude specifically that now needs an
      adapter written, because there is no Anthropic subscription and no free tier.
- [x] CI runs the full pipeline with `MockProvider` and ~~makes no network calls~~
      **makes no network call to a model**
      **Wording amended 2026-09-23.** As written this line is false on its face: CI
      calls npm, PyPI, Docker Hub and the Playwright CDN on every run. §17's phrasing
      — "no CI job makes a network call **to a model**" — is the one that can be
      true, and is: `tests/conftest.py` blocks every non-loopback `connect`,
      `connect_ex` and `create_connection`, autouse, so a new test file is covered
      without opting in. The orchestrator integration suite drives generator,
      classifier and screener as separate `MockProvider`s.
      Loopback stays open deliberately, and that is the residual risk: a CI job with
      an Ollama on localhost would not be blocked. No CI job starts one.
      `pytest tests/test_no_external_network.py -q`
- [x] PII guard verified to block production + non-paid provider
      `pytest tests/test_guards.py -q` — and see the base-URL limit noted in §14.
- [ ] Every AI call logged by Go with model, prompt version, tokens, latency, integer cost
      **NOT SATISFIABLE IN THIS PHASE. Moved to
      [PHASE-05 §15](./PHASE-05-RAG-AND-CHAT.md#15-definition-of-done) on 2026-09-23**,
      where chat becomes the first production caller. Left unticked here rather than
      deleted, so the move is visible instead of looking like an item that quietly
      vanished.
      Phase 4 has exactly one Go AI call site — the admin playground — and it
      deliberately writes no row, with a comment saying why: *"a playground run is an
      operator experimenting, and mixing it into the usage table would corrupt the
      cost-per-request figure that table exists to produce"*. `ai_request_logs` has
      0 rows and every caller of `ailogs.Service.Record` is a test.
      The machinery is built and tested; there is nothing for it to record until
      Phase 5 ships chat. The `cost_micros`-is-an-integer half is met — see §17.
- [x] Intent classifier ≥85% on the labelled set
      **180/200 = 90.0%**, independently reproduced at **183/200 = 91.5%**, both on
      `qwen/qwen3.8-27b` via Groq with prompt v4. Two runs agreeing within noise is
      what makes the number usable on a machine with confirmed RAM faults.
      A free LOCAL model does not reach the bar: 40% (`llama3.2:3b`), 60.5%
      (`qwen2.5:7b`). §15 predicted this and named the mitigation that was used.
- [x] Output validator blocks fabricated chart facts
      `pytest tests/test_validation.py -q`. Note that in Phase 4 the fact index is
      always empty (`NoChartContext`), so every extracted personal placement blocks —
      correct, and a weaker test than it will be once Phase 5 supplies a real index.

---

## 17. Phase Gate 🔒

- [x] `LLMProvider` implemented by all ~~four~~ **three** adapters, all passing the parity suite
      **Amended 2026-09-23.** [ADR-011](../decisions/011-remove-anthropic-adapter.md)
      removed the Anthropic adapter and struck through two gate lines below while
      walking past this one, which kept asserting a cardinality that stopped being
      true the same day. The adapters are `openai-compatible`, `google` and `mock`;
      `anthropic` is not installed and the parity suite collects exactly three ids.
      The parity half of this line holds and is not vacuous — seven mutations, one
      per behaviour it claims to guard, each turns the suite red.
- [x] Ollama runs `fast`, `chat` and `deep` tiers locally at zero cost
      Verified by running all three through the SHIPPING factory —
      `provider_from_settings()`, the one `app/api/complete.py` calls — rather than a
      provider the script built for itself:

      ```
      LLM_BASE_URL=http://localhost:11434/v1 LLM_PROVIDER_TIER=local LLM_API_KEY= \
        uv run python -m scripts.verify_local_tiers
      ✓ fast  llama3.2:3b   in=32 out=2 cost_micros=0   8104 ms
      ✓ chat  qwen2.5:7b    in=36 out=2 cost_micros=0  13470 ms
      ✓ deep  qwen2.5:7b    in=36 out=2 cost_micros=0    779 ms
      ```

      `cost_micros=0` is the half a successful call does not establish on its own: a
      tier that quietly answered from a hosted provider would print a number here.

      **One run mode does not work, and it is worth knowing.** The containerised `ai`
      service cannot reach Ollama on this machine, because Ollama is bound to
      `127.0.0.1:11434` — `ConnectionRefusedError` on both `host.docker.internal` and
      the bridge gateway. The repo's wiring is correct; compose defaults to
      `host.docker.internal` with `extra_hosts`. Use `task dev:ai`, which is the
      documented way to run this service from source anyway, or start Ollama with
      `OLLAMA_HOST=0.0.0.0` to expose it beyond loopback — an operator's decision.
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
- [x] `MockProvider` powers all CI; no CI job makes a network call to a model
      `pytest tests/test_no_external_network.py -q`. Autouse socket blocker, so a
      new test file is covered without opting in. Loopback stays open by design and
      is the residual risk; no CI job starts a local model.
- [x] **PII guard blocks `production` + non-paid provider — test proves it**
      `pytest tests/test_guards.py -q`. Three guards also stopped failing open on
      any environment name but the exact string `production` — `"prod"`,
      `"Production"`, `""` disabled the PII guard, the internal-token check and the
      read-only-DB check at once. Now an allowlist, so an unknown env fails closed.
- [x] Model router maps all 10 job types; overridable from admin without deploy
      **The override had no test.** Making `Orchestrator.router` return a copy — the
      defect its own docstring warns about, "would return 200 and change nothing" —
      left all 124 tests passing. Now asserted on the tier a job RESOLVES to after
      the PATCH, not on the 200 or the echoed body, both of which a copy produces
      correctly. `pytest tests/test_complete_route.py -k RoutingOverride`
- [x] Prompt registry immutable; editing a published module fails CI
      Verified by doing it: appending one line to `astrology_rules.v1.md` fails
      `TestImmutability::test_no_published_module_has_changed`, which runs in the
      `Python — ai` CI job. `pytest tests/test_prompt_registry.py -q`
- [x] Cache breakpoint ordering verified by a prefix-stability test
      `pytest tests/ -k prefix` — two different users' charts produce a
      byte-identical prefix, and changing a stable module moves it.
      **The breakpoint itself has never engaged**: the prefix is ~770 tokens against
      the ~1024 minimum below which `cache_control` is ignored. Ordering is what this
      line asks for and it holds; the caching it enables starts in Phase 5, and a
      test fails on the day the corpus crosses that line.
- [x] Intent classifier ≥85% on 200 labelled messages, keyword pre-pass working
      180/200 = 90.0%, reproduced at 183/200 = 91.5%. Keyword pre-pass: 82/82
      correct — 100% precision at 41% coverage, asserted in CI. The labelled set is
      200 synthetic messages, committed. See §16 for the local-model caveat.
- [x] Crisis input short-circuits to a static human-written response
      Asserted on the provider receiving nothing, both via the offline keyword pass
      (zero model calls) and the model screener. Verified live with every provider
      unreachable. Covers Latin, Hinglish and Devanagari — **see §14 for how that
      list got from 50 phrases to 100, and for what is still open.**
- [x] Output validator blocks fabricated chart facts, ~~unsupported certainty~~
      **harmful predictions**, and prompt leaks
      **Wording amended 2026-09-23, and a real gap closed behind it.**

      The line as written was false on its middle third: unsupported certainty
      *warns* by design — `validator.py` sets severity `warn`, and
      `test_predictive_framing_warns_rather_than_blocks` asserts
      `not blocks(violations)`. That is deliberate. Blocking every predictive
      phrasing regenerates a large share of otherwise good answers, which spends
      money and latency on a tone problem.

      But auditing the line found the design had drawn the boundary in the wrong
      place. Severity was tracking **how confident a sentence sounds** rather than
      **what happens if the reader believes it**, so:

          "You will die in 2030."                      warn only, served
          "You will fall seriously ill in 2027."       not flagged at all
          "Your business will fail within two years."  not flagged at all

      `.claude/rules/ai.md` names those domains explicitly — *"No guaranteed
      outcomes: medical, marriage, pregnancy, death, legal, financial"* — so
      `_HARMFUL_PREDICTION` now **blocks** death, serious illness, infertility and
      financial ruin, hedged or not. Marriage, promotion and wealth stay in the warn
      tier: being told you will be promoted and then not being promoted is a
      disappointment, not a harm.

      Three tiers, each with its own control test:
      `pytest tests/test_validation.py -k HarmRegardless`
- [x] Telemetry envelope persisted by Go; `cost_micros` is an integer
      `BIGINT`, proven against real Postgres by round-tripping 2^53+1 — a value no
      `float64` holds. The persistence machinery is built and tested; §16 records
      that Phase 4 has nothing for it to record yet.
- [x] Retry, circuit breaker and fallback verified by killing the primary provider mid-run
      **Was claimed and was not true.** Nothing in the repo killed anything; the
      breaker tests ran against an in-memory stub with a fake clock, and making the
      SHIPPED chain's breaker incapable of opening left 881 tests green.
      A real mid-run kill — a server that reads the completion in full, then sends
      RST — showed the dying provider receiving the same request three times, because
      the connection branch assumed "never accepted the request". That also exposed
      a dead branch: two httpx distributions are installed and
      `httpx.ConnectError is not httpx2.ConnectError`, so the phase check never
      matched a real SDK failure.
      `pytest tests/test_openai_compatible.py tests/test_complete_route.py -q`
- [x] Generated Go client compiles from the AI service OpenAPI; contract diff passes
      Exercised for real this session: adding a query parameter to `GET /health`
      regenerated the client, changed its signature and broke the Go call site —
      caught by `task verify`, which is what generating from the spec is for.
- [x] Admin config, usage, incidents and playground all working
      `go test ./internal/httpapi/... -tags=integration`. Rows seeded and read back
      through HTTP, asserting totals, cache hit rate, grouping, and that incidents
      selects only the failure.
- [x] No message content in `ai_request_logs`
      No content column exists, so it cannot leak what it cannot store. Asserted
      against the serialised envelope, so a field added later is caught.
- [x] `ai-service` DB role still read-only
      Postgres grants, not convention.
      `REQUIRE_CONTAINERS=1 go test ./internal/platform/db/... -tags=integration`
- [x] `task verify` green — lint, test and build across Go, Python and TypeScript.
- [x] `docs/PROJECT_STATUS.md` and `.claude/state/current-phase.md` updated
      Ticked last, deliberately: it is the only item whose truth depends on every
      other one being settled first.
      `current-phase.md` no longer carries a per-item count. It carried
      "✅ 19 of 19 closed" while eighteen of these boxes were unticked, and a count in
      a state file is the thing that rots — the boxes are the artifact. It now points
      here and records only what does not move.
