"""One place that turns configuration into a provider.

── Why this exists ──

It was in `app/api/complete.py` as a private function, and the
measurement scripts each hardcoded `OpenAICompatibleProvider`. That was
invisible until it mattered: with `LLM_PROVIDER=google` and a Google
key, the service would run on Gemini and
`scripts/measure_intent_accuracy.py` would quietly keep talking to
`http://localhost:11434` — reporting a number for a model that was not
the configured one, under a heading naming the configured one.

A measurement that silently measures something else is worse than no
measurement, so the construction lives once and everything reads it.

── Why the match is on a Literal ──

`settings.llm_provider` is a `Literal`, so adding a fifth provider
without adding it here is a `mypy --strict` failure at this line rather
than a runtime `else` branch that serves the wrong thing.
"""

from __future__ import annotations

from pathlib import Path

from app.providers.anthropic_provider import AnthropicProvider
from app.providers.base import LLMProvider, ModelMap
from app.providers.google_provider import GoogleProvider
from app.providers.mock import MockProvider
from app.providers.openai_compatible import OpenAICompatibleProvider
from app.settings import Settings, settings

FIXTURES = Path(__file__).parent.parent.parent / "tests" / "fixtures" / "ai"


def models_from(config: Settings, override: str | None = None) -> ModelMap:
    """The tier→model map, or one model for all three.

    `override` is for the measurement scripts, which compare models on
    the same job and need `fast` pointed somewhere specific without
    inventing three env vars to do it.
    """
    if override:
        return ModelMap(fast=override, chat=override, deep=override)
    return ModelMap(
        fast=config.llm_model_fast,
        chat=config.llm_model_chat,
        deep=config.llm_model_deep,
    )


def provider_from_settings(
    config: Settings | None = None,
    *,
    model_override: str | None = None,
    provider_id: str | None = None,
) -> LLMProvider:
    """Build the provider `LLM_PROVIDER` names.

    Takes the settings object rather than reading the global, so a test
    or a script can build one for a configuration that is not the
    process's own.
    """
    config = config or settings
    models = models_from(config, model_override)
    timeout = config.effective_llm_timeout_seconds

    match config.llm_provider:
        case "anthropic":
            return AnthropicProvider(
                api_key=config.llm_api_key,
                models=models,
                timeout_seconds=timeout,
                provider_id=provider_id or "anthropic",
            )
        case "google":
            return GoogleProvider(
                api_key=config.llm_api_key,
                models=models,
                tier=config.llm_provider_tier,
                timeout_seconds=timeout,
                provider_id=provider_id or "google",
            )
        case "mock":
            # `allow_unknown` so a script can run the whole labelled set
            # without a fixture per message. The mock's own tests cover
            # the strict behaviour.
            return MockProvider(
                FIXTURES,
                allow_unknown=True,
                provider_id=provider_id or "mock",
            )
        case "openai-compatible":
            return OpenAICompatibleProvider(
                base_url=config.llm_base_url,
                api_key=config.llm_api_key,
                tier=config.llm_provider_tier,
                models=models,
                timeout_seconds=timeout,
                provider_id=provider_id or "openai-compatible",
            )


def describe(config: Settings | None = None, model_override: str | None = None) -> str:
    """One line naming what a run is actually talking to.

    Printed by every script before it starts. The failure this prevents
    is reading a number off a report whose heading names one model and
    whose calls went to another.
    """
    config = config or settings
    models = models_from(config, model_override)
    where = config.llm_base_url if config.llm_provider == "openai-compatible" else "vendor API"
    return (
        f"provider={config.llm_provider} tier={config.llm_provider_tier} "
        f"fast={models.fast} at {where}"
    )
