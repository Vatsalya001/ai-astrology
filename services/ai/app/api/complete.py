"""`POST /v1/complete` — the internal completion route.

Internal only. `middleware.install` requires `X-Internal-Token` on every
path outside `PUBLIC_PATHS`, so step 1 of PHASE-04 §9 is handled before
this module is reached — which is the right place for it, because a
guard that each route remembers to apply is a guard one route forgets.

── Why the provider chain is assembled once, at startup ──

Constructing it per request would rebuild the circuit breaker every
time, and a breaker with no memory is not a breaker: it would re-probe a
dead provider on every single request and pay the full timeout to
rediscover what the previous request already learned.

── Why this module owns the chain rather than the orchestrator ──

`app/guards.py` checks the tier of every provider that will serve, and
it can only do that against the objects that will actually serve. Both
it and the route call `get_provider_chain()`, which is `lru_cache`d, so
the provider the startup guard approved is the identical object the
first request reaches — no window in which the checked configuration and
the serving configuration differ.
"""

from __future__ import annotations

import logging
from collections.abc import AsyncIterator
from functools import lru_cache
from pathlib import Path

from fastapi import APIRouter, Request

from app.classification import IntentClassifier
from app.orchestrator import (
    AIResponseEnvelope,
    CompleteRequest,
    CompleteResult,
    Orchestrator,
)
from app.providers import (
    AnthropicProvider,
    Capabilities,
    CompletionChunk,
    CompletionRequest,
    CompletionResponse,
    GoogleProvider,
    LLMProvider,
    MockProvider,
    ModelMap,
    OpenAICompatibleProvider,
    ProviderRegistry,
    ProviderTier,
    ResilientProvider,
)
from app.routing import ModelRouter
from app.safety import SafetyClassifier
from app.settings import settings

router = APIRouter(tags=["ai"], prefix="/v1")
log = logging.getLogger(__name__)


def _models() -> ModelMap:
    return ModelMap(
        fast=settings.llm_model_fast,
        chat=settings.llm_model_chat,
        deep=settings.llm_model_deep,
    )


def _mock(provider_id: str) -> MockProvider:
    """The offline adapter, pointed at the committed fixture set."""
    return MockProvider(
        Path(__file__).parent.parent.parent / "tests" / "fixtures" / "ai",
        provider_id=provider_id,
        allow_unknown=True,
    )


def _raw_provider() -> LLMProvider:
    """Whichever backend configuration names.

    A match on a `Literal`, so adding a fifth provider to the setting
    without adding it here is a `mypy --strict` failure rather than a
    runtime `else` branch that quietly serves the wrong thing.
    """
    match settings.llm_provider:
        case "anthropic":
            return AnthropicProvider(
                api_key=settings.llm_api_key,
                models=_models(),
                timeout_seconds=settings.effective_llm_timeout_seconds,
            )
        case "google":
            return GoogleProvider(
                api_key=settings.llm_api_key,
                models=_models(),
                tier=settings.llm_provider_tier,
                timeout_seconds=settings.effective_llm_timeout_seconds,
            )
        case "mock":
            return _mock("mock")
        case "openai-compatible":
            return OpenAICompatibleProvider(
                base_url=settings.llm_base_url,
                api_key=settings.llm_api_key,
                tier=settings.llm_provider_tier,
                models=_models(),
                timeout_seconds=settings.effective_llm_timeout_seconds,
            )


def _fallback_provider() -> LLMProvider | None:
    """The second link in the chain, or nothing at all.

    Absent unless `LLM_FALLBACK_PROVIDER` names one. A fallback that
    appeared by default would be a provider nobody chose, taking real
    traffic on the one day the primary is down — with a tier, a price
    and a retention policy no one reviewed.

    Note what is NOT passed: `tier`. Each adapter reports its own
    (`AnthropicProvider` is `paid`, `GoogleProvider` is `free-hosted`,
    `MockProvider` is `local`), and handing it the primary's DECLARED
    tier is precisely how a free Gemini key gets blessed as "paid" and
    receives birth data in production. `ProviderRegistry.register` reads
    that tier, so a free-tier fallback refuses to boot in production
    instead of waiting for an outage to leak.

    Model names come from the same `ModelMap` as the primary — §11
    defines no per-fallback models — so a fallback must be a provider
    that accepts them.
    """
    match settings.llm_fallback_provider:
        case "":
            return None
        case "anthropic":
            return AnthropicProvider(
                api_key=settings.llm_fallback_api_key,
                models=_models(),
                timeout_seconds=settings.effective_llm_timeout_seconds,
            )
        case "google":
            return GoogleProvider(
                api_key=settings.llm_fallback_api_key,
                models=_models(),
                timeout_seconds=settings.effective_llm_timeout_seconds,
            )
        case "mock":
            # A distinct id: the registry refuses the same id twice, and
            # a chain whose two links report the same name cannot be read
            # in telemetry either.
            return _mock("mock-fallback")


def _resilient(provider: LLMProvider) -> ResilientProvider:
    """One retry budget and one breaker, per provider.

    Per provider and not per chain, because §2 says so for a reason: a
    shared breaker opened by the primary's outage would fail the
    fallback's calls too, and the chain would have no fallback at exactly
    the moment it needs one.
    """
    return ResilientProvider(
        provider,
        # +1 because the setting counts RETRIES and the budget counts
        # ATTEMPTS. Passing it through raw would make the documented
        # "2 retries" mean one retry and a confusing latency graph.
        max_attempts=settings.llm_max_retries + 1,
        failure_threshold=settings.llm_circuit_breaker_threshold,
        open_seconds=settings.llm_circuit_breaker_reset_seconds,
    )


@lru_cache(maxsize=1)
def get_provider_chain() -> ProviderRegistry:
    """`chain = [primary, *fallbacks]` — PHASE-04 §2, actually wired.

    Built through `ProviderRegistry` rather than assembled inline
    because registration is where the PII guard runs: a provider that
    must not serve in production cannot enter the chain at all, even if
    a caller swallows the exception.

    `lru_cache` so the breakers remember. It is also what lets
    `app/guards.py` check the very objects that will serve.
    """
    chain = ProviderRegistry(env=settings.env)
    chain.register(_resilient(_raw_provider()))

    fallback = _fallback_provider()
    if fallback is not None:
        chain.register(_resilient(fallback))

    return chain


class _ChainProvider:
    """The whole chain, wearing the shape of one provider.

    `Orchestrator`, `IntentClassifier` and `SafetyClassifier` each take a
    single `LLMProvider`, and `ProviderRegistry` deliberately is not one
    — it has no single id, tier or health to report. Without this
    adapter the only thing there is to hand them is the primary, which
    was the defect: the registry existed, the tests exercised it, and the
    running service had nothing to fall back to.

    `id`, `tier` and `capabilities` report the PRIMARY's, because that is
    what a caller reaches first. Telemetry does not read them: every
    `CompletionResponse` carries the `provider_id` of whichever provider
    answered, so a request served by the fallback is recorded as served
    by the fallback.
    """

    def __init__(self, chain: ProviderRegistry) -> None:
        self._chain = chain

    @property
    def id(self) -> str:
        return self._chain.primary.id

    @property
    def tier(self) -> ProviderTier:
        return self._chain.primary.tier

    @property
    def capabilities(self) -> Capabilities:
        return self._chain.primary.capabilities

    async def complete(self, req: CompletionRequest) -> CompletionResponse:
        return await self._chain.complete(req)

    def stream(self, req: CompletionRequest) -> AsyncIterator[CompletionChunk]:
        return self._chain.stream(req)

    async def health_check(self) -> bool:
        """Up if ANY link is up.

        A chain whose primary is down but whose fallback is answering is
        still serving users, and reporting it unhealthy would take a
        working service out of a load balancer. Per-provider detail is
        `ProviderRegistry.health()`, which the health endpoint can show.
        """
        return any((await self._chain.health()).values())


@lru_cache(maxsize=1)
def get_orchestrator() -> Orchestrator:
    """Built once per process, and cached for the breaker's sake.

    `lru_cache` rather than a module-level constant so that importing
    this module does not construct an SDK client — which would make
    every test that imports a route need a provider key.
    """
    provider: LLMProvider = _ChainProvider(get_provider_chain())

    return Orchestrator(
        provider=provider,
        # Classification and screening run on the same chain. They are
        # `fast`-tier jobs and the tier is chosen per request by the
        # router, so one provider serves all three — and all three fail
        # over together.
        classifier=IntentClassifier(provider, prompt_version=settings.prompt_version_intent),
        screener=SafetyClassifier(provider),
        router=ModelRouter(),
        prompt_version=settings.prompt_version_chat,
    )


@router.post("/complete", response_model=AIResponseEnvelope[CompleteResult])
async def complete(body: CompleteRequest, request: Request) -> AIResponseEnvelope[CompleteResult]:
    """Run the pipeline and return the result with its telemetry.

    The envelope goes back whole. Go writes `telemetry` to
    `ai_request_logs` in the same transaction as the message, which is
    what keeps a single writer and a single transaction boundary — and
    means a request that cost money can never be missing from the bill
    because a separate logging call failed.
    """
    trace_id = getattr(request.state, "trace_id", "")

    envelope = await get_orchestrator().complete(body, trace_id=trace_id)

    # IDs and counts. Never the message, never the answer — the same
    # rule the telemetry row itself follows, applied to the log line,
    # because a log is the place the rule is easiest to forget.
    log.info(
        "completion",
        extra={
            "trace_id": trace_id,
            "user_id": envelope.telemetry.user_id,
            "job": envelope.telemetry.job_type,
            "intent": envelope.telemetry.intent,
            "provider": envelope.telemetry.provider_id,
            "model_calls": envelope.telemetry.model_calls,
            "cost_micros": envelope.telemetry.cost_micros,
            "validation_passed": envelope.telemetry.validation_passed,
            "crisis": envelope.result.is_crisis_response,
        },
    )

    return envelope
