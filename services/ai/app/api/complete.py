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
"""

from __future__ import annotations

import logging
from functools import lru_cache

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
    GoogleProvider,
    LLMProvider,
    MockProvider,
    ModelMap,
    OpenAICompatibleProvider,
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


def _raw_provider() -> LLMProvider:
    """Whichever backend configuration names.

    A match on a `Literal`, so adding a fifth provider to the setting
    without adding it here is a `mypy --strict` failure rather than a
    runtime `else` branch that quietly serves the wrong thing.
    """
    match settings.llm_provider:
        case "anthropic":
            return AnthropicProvider(api_key=settings.llm_api_key, models=_models())
        case "google":
            return GoogleProvider(
                api_key=settings.llm_api_key,
                models=_models(),
                tier=settings.llm_provider_tier,
            )
        case "mock":
            from pathlib import Path

            return MockProvider(
                Path(__file__).parent.parent.parent / "tests" / "fixtures" / "ai",
                allow_unknown=True,
            )
        case "openai-compatible":
            return OpenAICompatibleProvider(
                base_url=settings.llm_base_url,
                api_key=settings.llm_api_key,
                tier=settings.llm_provider_tier,
                models=_models(),
            )


@lru_cache(maxsize=1)
def get_orchestrator() -> Orchestrator:
    """Built once per process, and cached for the breaker's sake.

    `lru_cache` rather than a module-level constant so that importing
    this module does not construct an SDK client — which would make
    every test that imports a route need a provider key.
    """
    provider = ResilientProvider(_raw_provider())

    return Orchestrator(
        provider=provider,
        # Classification and screening run on the same chain. They are
        # `fast`-tier jobs and the tier is chosen per request by the
        # router, so one provider serves all three.
        classifier=IntentClassifier(provider),
        screener=SafetyClassifier(provider),
        router=ModelRouter(),
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
