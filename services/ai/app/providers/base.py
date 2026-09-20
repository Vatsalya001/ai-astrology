"""The seam every model call passes through.

Nothing outside this package may import a vendor SDK. That is not a
style preference: the whole point of Phase 4 is that swapping Ollama for
Anthropic is a configuration change, and it stops being one the moment
`from openai import ...` appears in an orchestrator. `.importlinter`
enforces it in CI, because conventions erode and a contract does not.

── Protocol, not ABC ──

Adapters share no implementation and need no common base class. A
`Protocol` gives `mypy --strict` structural conformance at every call
site, which catches a missing method at the point of use rather than at
the point of registration — and it means a test double is just a class
with the right shape, not an inheritance ceremony.
"""

from __future__ import annotations

from collections.abc import AsyncIterator
from typing import Literal, Protocol, runtime_checkable

from pydantic import BaseModel, Field

# Re-exported with `as`, which is how a module tells mypy --strict that a
# name is part of its public surface rather than an implementation
# detail. Written this way because it was DECLARED here and in
# settings.py, identically: two Literal aliases with the same members
# typecheck against each other, so nothing would have reported the drift
# — and the day a fourth tier was added to one of them, the PII guard and
# the adapters would have disagreed about what "paid" means with no test
# failing. Settings owns it, because that is where an operator sets it.
from app.settings import ProviderTier as ProviderTier

# ─── the two axes ────────────────────────────────────────────────────

ModelTier = Literal["fast", "chat", "deep"]
"""What a job needs, not which model provides it.

`fast` for classification and extraction, `chat` for conversation,
`deep` for paid interpretation. Routing tier → concrete model is the
router's job (§4), so a model swap never touches a call site.
"""

FinishReason = Literal["stop", "length", "refusal", "error"]
"""Why generation stopped.

`refusal` is separate from `error` on purpose. A model declining is a
normal, expected outcome with a product response — a safety path, not a
retry path — and collapsing the two makes the retry logic hammer a
provider that is working exactly as designed.
"""

Role = Literal["system", "user", "assistant"]


# ─── requests ────────────────────────────────────────────────────────


class Message(BaseModel):
    """One turn. Deliberately not a vendor type."""

    role: Role
    content: str


class SystemBlock(BaseModel):
    """One system-prompt module, kept separable for caching.

    The system prompt is a LIST rather than one string because prompt
    caching works on a byte-identical prefix: stable modules first,
    volatile ones last. Concatenating early would make every request a
    cache miss, and the cost difference is the single biggest lever this
    service has (§5).
    """

    content: str

    cacheable: bool = False
    """Whether a provider may place a cache breakpoint after this block.

    Only meaningful for providers that support explicit breakpoints;
    others ignore it and still benefit from prefix stability.
    """

    name: str = ""
    """Which module this is, for debugging a prefix that stopped matching."""


class RequestMetadata(BaseModel):
    """Who asked, why, and under which prompt.

    Carried on every request so the telemetry envelope can be assembled
    without the orchestrator threading four more parameters through every
    layer. `user_id` is an ID — never an email, never a name. See
    `.claude/rules/security.md`.
    """

    trace_id: str
    user_id: str = ""
    prompt_version: str = ""
    job: str = ""


class CompletionRequest(BaseModel):
    """What every adapter receives, whatever it talks to."""

    messages: list[Message]
    system: list[SystemBlock] = Field(default_factory=list)
    tier: ModelTier
    max_tokens: int = Field(default=4096, gt=0, le=200_000)
    temperature: float = Field(default=0.7, ge=0.0, le=2.0)
    json_schema: dict[str, object] | None = None
    stop_sequences: list[str] = Field(default_factory=list)
    metadata: RequestMetadata


# ─── responses ───────────────────────────────────────────────────────


class Usage(BaseModel):
    """Tokens and money.

    `cost_micros` is an INTEGER of micro-units, never a float — the same
    rule the ledger follows for paise. Costs are summed across millions
    of calls, and float addition does not associate: the same set of
    charges in a different order gives a different total, which is
    indefensible on a bill. Micro-units because a single cheap call can
    cost fractions of a cent.
    """

    input_tokens: int = Field(default=0, ge=0)
    """Fresh input tokens: neither read from cache nor written to it.

    Disjoint from the two cache counters below, matching how Anthropic
    reports them. Summing all three gives the true input size; using this
    one alone understates a cached request by the whole prefix, which is
    the great majority of it in this product.
    """

    output_tokens: int = Field(default=0, ge=0)

    cached_input_tokens: int = Field(default=0, ge=0)
    """Read from the cache, at roughly a tenth of the input price."""

    cache_write_input_tokens: int = Field(default=0, ge=0)
    """Written to the cache, at roughly 1.25x the input price.

    Tracked separately rather than folded into `input_tokens` because it
    is priced differently and because the ratio between this and
    `cached_input_tokens` is the cache hit rate — the number that says
    whether the biggest cost lever in the service is actually engaged.
    Providers without explicit caching leave it zero.
    """

    cost_micros: int = Field(default=0, ge=0)


class CallStats(BaseModel):
    """What one pipeline step spent at a provider.

    Carried back by the intent classifier and the safety screener so the
    orchestrator can bill them. Without it, both made real model calls
    whose tokens reached nothing: `ai_request_logs.cost_micros` covered
    only the generation call, so every request under-reported — and the
    under-report was largest on the cheap requests the keyword pre-pass
    was supposed to make cheaper, which is exactly where the cost model
    needed to be trusted.

    `calls` counts an ATTEMPT, not a success. A classification whose
    reply was unparseable still cost money, and a counter that only
    recorded parseable answers would show the retry-heavy requests as
    the cheap ones.
    """

    calls: int = Field(default=0, ge=0)
    usage: Usage = Field(default_factory=Usage)
    model: str = Field(default="", description="For pricing. The model that ANSWERED.")

    def plus(self, other: CallStats) -> CallStats:
        """Summed with integers throughout, so the total does not depend
        on which of two concurrent steps finished first."""
        return CallStats(
            calls=self.calls + other.calls,
            usage=Usage(
                input_tokens=self.usage.input_tokens + other.usage.input_tokens,
                output_tokens=self.usage.output_tokens + other.usage.output_tokens,
                cached_input_tokens=(
                    self.usage.cached_input_tokens + other.usage.cached_input_tokens
                ),
                cache_write_input_tokens=(
                    self.usage.cache_write_input_tokens + other.usage.cache_write_input_tokens
                ),
                cost_micros=self.usage.cost_micros + other.usage.cost_micros,
            ),
            model=other.model or self.model,
        )


class CompletionResponse(BaseModel):
    """What every adapter returns, whatever it talked to.

    `model` and `provider_id` are recorded rather than inferred: a
    response cannot be explained three weeks later without knowing which
    model produced it, and the configured model is not always the one
    that answered once fallback exists (§2, provider registry).
    """

    text: str
    finish_reason: FinishReason
    usage: Usage
    model: str
    provider_id: str
    latency_ms: int = Field(default=0, ge=0)


class CompletionChunk(BaseModel):
    """One streamed fragment.

    `usage` arrives only on the final chunk, and only from providers that
    report it — hence optional rather than zero-valued. A zero default
    would be silently summed into a cost total as though the call were
    free.
    """

    text: str = ""
    finish_reason: FinishReason | None = None
    usage: Usage | None = None


class ModelMap(BaseModel):
    """Which concrete model serves each tier, for one provider.

    Carried by the adapter rather than resolved at the call site: a call
    site asks for `fast` because the JOB is cheap and mechanical, and it
    should never have to know that `fast` means `llama3.2:3b` here and
    `claude-haiku-4-5` there. Swapping a model is then a config change in
    one place, which is the whole premise of the phase.

    The three names may be identical — in development they usually are,
    because one 7B model on a laptop serves every tier adequately and
    pulling three is a gigabyte each for no benefit.
    """

    fast: str
    chat: str
    deep: str

    def for_tier(self, tier: ModelTier) -> str:
        # A dict lookup keyed on the Literal, so adding a fourth tier to
        # ModelTier fails here at type-check time rather than falling
        # through to a KeyError on the first request that uses it.
        return {"fast": self.fast, "chat": self.chat, "deep": self.deep}[tier]


class Capabilities(BaseModel):
    """What a provider can actually do.

    Declared rather than discovered, so the router can refuse an
    impossible request before spending a network round trip on it —
    asking a model without JSON mode for structured output fails at the
    edge with a clear message instead of returning prose that fails to
    parse three layers in.
    """

    streaming: bool = True
    json_mode: bool = False
    prompt_caching: bool = False
    vision: bool = False
    max_context_tokens: int = Field(default=8192, gt=0)


# ─── the protocols ───────────────────────────────────────────────────


@runtime_checkable
class LLMProvider(Protocol):
    """Every model this product talks to, behind one shape.

    `runtime_checkable` so the registry can reject a mis-registered
    object with a clear error at startup rather than an AttributeError
    on the first user request. Note that a runtime `isinstance` check
    verifies method NAMES only — signatures are `mypy`'s job, which is
    why `mypy --strict` is not optional here.
    """

    @property
    def id(self) -> str:
        """Stable identifier, recorded in telemetry and logs."""
        ...

    @property
    def tier(self) -> ProviderTier:
        """Read by the PII guard at startup. See `app/guards.py`."""
        ...

    @property
    def capabilities(self) -> Capabilities: ...

    async def complete(self, req: CompletionRequest) -> CompletionResponse: ...

    def stream(self, req: CompletionRequest) -> AsyncIterator[CompletionChunk]:
        """Not `async def`: the method returns the iterator, it does not
        await it. Declaring it `async` would make callers write
        `await (await p.stream(r)).__anext__()`, which is nobody's idea
        of an API."""
        ...

    async def health_check(self) -> bool:
        """Cheap liveness probe. Never raises — a provider that is down is
        a `False`, not an exception the caller has to catch to find out."""
        ...


@runtime_checkable
class EmbeddingProvider(Protocol):
    """Vectors, for Phase 5's retrieval."""

    @property
    def id(self) -> str: ...

    @property
    def tier(self) -> ProviderTier: ...

    @property
    def dimensions(self) -> int:
        """Declared, and checked against `EMBEDDING_DIM` at startup.

        A mismatch here is unrecoverable and silent: vectors of the wrong
        width either fail on insert or, worse, land in a column that
        accepts them and make every similarity score meaningless.
        """
        ...

    async def embed(self, texts: list[str]) -> list[list[float]]:
        """Batch, not single. Every provider charges and rate-limits per
        request, and a loop of one-text calls is the difference between
        one round trip and five hundred."""
        ...

    async def health_check(self) -> bool: ...


class ProviderError(RuntimeError):
    """A provider failed in a way the caller may want to branch on.

    `retryable` is the whole reason this type exists. A 429 or a timeout
    should be retried and then failed over; a 400 from a malformed
    request will fail identically forever, and retrying it three times
    across two providers turns one bad request into six.
    """

    def __init__(
        self,
        message: str,
        *,
        provider_id: str,
        retryable: bool,
        status_code: int | None = None,
    ) -> None:
        super().__init__(message)
        self.provider_id = provider_id
        self.retryable = retryable
        self.status_code = status_code
