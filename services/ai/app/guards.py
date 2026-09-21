"""Startup guards for ai-service.

The PII guard below is the single most important piece of code in this
service. It is short on purpose: a rule this important should be
readable in one glance.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

from app.settings import ProviderTier

if TYPE_CHECKING:
    # Type-checking only. `app/providers/__init__.py` executes every
    # adapter, so a runtime import here would pull three vendor SDKs into
    # the module that is supposed to decide whether they may run — and
    # app/providers/registry.py imports THIS module, which would make the
    # cycle real rather than theoretical.
    from collections.abc import Iterable

    from app.providers.base import LLMProvider

# The development default. If this is still the token in production,
# service-to-service auth is effectively off.
DEV_INTERNAL_TOKEN = "dev-internal-token-change-me"


class UnsafeConfigurationError(RuntimeError):
    """Raised at startup for a configuration that must never run."""


def assert_provider_allowed(
    provider_id: str,
    tier: ProviderTier,
    env: str,
) -> None:
    """Refuse to run a non-paid model provider in production.

    Why this exists
    ---------------
    Requests from this service carry birth date, birth time and birth
    place — which in combination are close to a unique identifier for a
    person — plus conversation content about health, marriage, money and
    family.

    Most free API tiers reserve the right to train on their inputs.

    So: free models for development and CI, a paid provider with a
    no-training commitment in production. Enforced by the process
    refusing to boot, because a wiki page is not an enforcement
    mechanism.

    Local Ollama is exempt from the privacy concern — nothing leaves the
    machine — but is still blocked in production on reliability grounds.
    The single `tier != "paid"` check covers both cases.
    """
    if env == "production" and tier != "paid":
        raise UnsafeConfigurationError(
            f'Refusing to start: LLM provider "{provider_id}" is tier "{tier}". '
            f"Production requires a paid provider with a no-training commitment, "
            f"because requests carry birth data and personal conversation content. "
            f"Point LLM_PROVIDER at a vendor you pay, and set "
            f"LLM_PROVIDER_TIER=paid. Note that `google` cannot be blessed "
            f"this way: its adapter declares its own free-hosted tier, "
            f"because a key string cannot be inspected for whether billing "
            f"is attached."
        )


def assert_chain_providers_allowed(chain: Iterable[LLMProvider], env: str) -> None:
    """The same rule, applied to the objects that will actually serve.

    `LLM_PROVIDER_TIER` is a DECLARATION, and a declaration can be wrong.
    `MockProvider.tier` is hardcoded `"local"` whatever the environment
    says it is, so

        LLM_PROVIDER=mock LLM_PROVIDER_TIER=paid ENV=production

    passed a startup guard that read the setting, and booted a
    production service answering real questions with canned fixtures.
    `ProviderRegistry.register` has always read `provider.tier`; this is
    the startup guard agreeing with it.

    Every link is checked, not just the primary: a paid primary with a
    free-tier fallback is one outage away from being a free-tier
    service.
    """
    for provider in chain:
        assert_provider_allowed(provider.id, provider.tier, env)


def assert_safety_layers_enabled(
    validation_enabled: bool,
    block_on_fabricated_fact: bool,
) -> None:
    """Refuse a configuration that asks for the safety layer to be off.

    §11 documents both switches and this phase implements neither as a
    switch — output validation is unconditional in
    `app/orchestrator/pipeline.py`. Accepting `false` and ignoring it
    would be the worst of the three options: an operator would read
    `SAFETY_VALIDATION_ENABLED=false` in their own `.env`, believe
    fabricated-placement blocking was off for their load test, and be
    wrong about what shipped.

    So it fails here, loudly, naming the file that would have to change.
    """
    if not validation_enabled or not block_on_fabricated_fact:
        raise UnsafeConfigurationError(
            "Refusing to start: SAFETY_VALIDATION_ENABLED and "
            "SAFETY_BLOCK_ON_FABRICATED_FACT cannot be disabled. Output validation "
            "is unconditional in app/orchestrator/pipeline.py — there is no code "
            "path behind these flags, and a response that invents a planetary "
            "placement must never reach a user. Remove the setting rather than "
            "believing it took effect."
        )


def assert_internal_token_changed(token: str, env: str) -> None:
    """Refuse the development shared secret in production."""
    if env == "production" and token == DEV_INTERNAL_TOKEN:
        raise UnsafeConfigurationError(
            "Refusing to start: INTERNAL_TOKEN is still the development default "
            "in production. Generate one with: openssl rand -hex 32"
        )


def assert_database_is_read_only(dsn: str, env: str) -> None:
    """Warn loudly if ai-service is pointed at a writable role.

    Only a heuristic — the real enforcement is the Postgres grant in
    infrastructure/docker/init/01-init.sql, which cannot be bypassed by
    application code. This catches the honest mistake of copying the
    api-service DSN into this service's environment.
    """
    if env == "production" and "astro_ro" not in dsn:
        raise UnsafeConfigurationError(
            "Refusing to start: AI_DATABASE_URL does not use the read-only role. "
            "ai-service must never write to the database (ADR-001). "
            "Expected a DSN using the 'astro_ro' role."
        )


def run_all_startup_guards() -> None:
    """Run every guard. Called from the FastAPI lifespan hook."""
    from app.safety import assert_crisis_responses_present
    from app.settings import settings

    # The DECLARED tier, checked first because it is free: it needs no
    # provider constructed and no API key present, so the common
    # misconfiguration fails before anything expensive happens.
    assert_provider_allowed(
        provider_id=settings.llm_provider,
        tier=settings.llm_provider_tier,
        env=settings.env,
    )
    assert_internal_token_changed(settings.internal_token, settings.env)
    assert_database_is_read_only(settings.ai_database_url, settings.env)
    assert_safety_layers_enabled(
        settings.safety_validation_enabled,
        settings.safety_block_on_fabricated_fact,
    )

    # In every environment, not just production. A missing crisis
    # response is worse than a missing provider key: detection still
    # fires, which means the astrology path is already bypassed, and
    # there is then nothing at all to send. Booting without it is the
    # one configuration that turns a working guard into silence.
    assert_crisis_responses_present()

    # Last, because it is the only guard that constructs anything. The
    # chain is `lru_cache`d, so these are the exact provider objects the
    # first request will reach.
    #
    # Both checks stand rather than one replacing the other: the
    # declaration above needs no API key and fails on the cheap mistake,
    # while this one is the only thing that notices when the provider's
    # real tier is not what the declaration claimed. In production a
    # disagreement between them fails at whichever check sees the
    # non-paid side, which is the safe direction.
    #
    # Imported inside the function: app.api.complete imports
    # app.providers, which imports app.providers.registry, which imports
    # this module. At module scope that is an import cycle.
    from app.api.complete import get_provider_chain

    assert_chain_providers_allowed(get_provider_chain().chain, settings.env)
