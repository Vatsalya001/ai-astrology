"""Startup guards for ai-service.

The PII guard below is the single most important piece of code in this
service. It is short on purpose: a rule this important should be
readable in one glance.
"""

from __future__ import annotations

from app.settings import ProviderTier

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
            f"Set LLM_PROVIDER=anthropic and LLM_PROVIDER_TIER=paid."
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

    assert_provider_allowed(
        provider_id=settings.llm_provider,
        tier=settings.llm_provider_tier,
        env=settings.env,
    )
    assert_internal_token_changed(settings.internal_token, settings.env)
    assert_database_is_read_only(settings.ai_database_url, settings.env)

    # In every environment, not just production. A missing crisis
    # response is worse than a missing provider key: detection still
    # fires, which means the astrology path is already bypassed, and
    # there is then nothing at all to send. Booting without it is the
    # one configuration that turns a working guard into silence.
    assert_crisis_responses_present()
