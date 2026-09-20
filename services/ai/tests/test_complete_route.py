"""The provider chain as the running service actually builds it.

Every test here goes through `get_provider_chain()` and
`get_orchestrator()` — the functions the route calls — rather than
assembling a `ProviderRegistry` by hand. `tests/test_provider_registry.py`
already proves the registry falls back; what it cannot prove is that the
service uses it, and for the whole of Phase 4 the service did not: it
built one `ResilientProvider` and handed that to the orchestrator, so a
primary outage in production had nothing to fall back TO.

A hand-built chain would pass every test below while that was true.
"""

from __future__ import annotations

import socket
from collections.abc import Iterator
from typing import Any

import pytest

from app.api.complete import get_orchestrator, get_provider_chain
from app.guards import UnsafeConfigurationError
from app.orchestrator import CompleteRequest
from app.providers import (
    CompletionRequest,
    Message,
    NoProviderAvailableError,
    RequestMetadata,
)
from app.settings import settings


@pytest.fixture(autouse=True)
def _fresh_chain() -> Iterator[None]:
    """Both caches cleared around every test.

    `get_provider_chain` and `get_orchestrator` are `lru_cache`d on
    purpose — a breaker with no memory is not a breaker — so without
    this a test that changes a setting is served the previous test's
    chain and asserts nothing about its own configuration.
    """
    get_provider_chain.cache_clear()
    get_orchestrator.cache_clear()
    yield
    get_provider_chain.cache_clear()
    get_orchestrator.cache_clear()


def _configure(monkeypatch: pytest.MonkeyPatch, **overrides: Any) -> None:
    """Pin every setting this suite depends on.

    Explicit rather than relying on the declared defaults: pydantic reads
    the real OS environment, so a developer with `LLM_PROVIDER` exported
    would otherwise run a different test than CI does.
    """
    base: dict[str, Any] = {
        "env": "development",
        "llm_provider": "mock",
        "llm_api_key": "",
        "llm_provider_tier": "local",
        "llm_fallback_provider": "",
        "llm_fallback_api_key": "",
        # One attempt, so an unreachable primary fails over immediately
        # instead of sleeping through a backoff schedule in a unit test.
        "llm_max_retries": 0,
        "llm_timeout_seconds": 2.0,
    }
    for name, value in {**base, **overrides}.items():
        monkeypatch.setattr(settings, name, value)


def _closed_port() -> int:
    """A TCP port with nothing listening on it.

    Bound and released rather than hardcoded. A hardcoded port that
    something else on the machine happens to be using would turn "the
    primary is unreachable" into "the primary answered something
    unexpected" — a different test wearing this one's name.
    """
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        return int(probe.getsockname()[1])


def _request() -> CompletionRequest:
    return CompletionRequest(
        messages=[Message(role="user", content="what does the week look like")],
        tier="chat",
        metadata=RequestMetadata(trace_id="t-1"),
    )


class TestChainConstruction:
    def test_no_fallback_appears_unless_one_is_configured(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """A fallback nobody configured must not silently exist.

        The negative case for the whole feature: a default second link
        would take real traffic the day the primary degrades, at a tier,
        a price and a retention policy nobody reviewed.
        """
        _configure(monkeypatch)

        chain = get_provider_chain()

        assert [p.id for p in chain.chain] == ["mock"]

    def test_the_configured_fallback_is_second_in_the_chain(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """Order is the configuration: first registered is the primary."""
        _configure(
            monkeypatch,
            llm_provider="openai-compatible",
            llm_fallback_provider="mock",
        )

        chain = get_provider_chain()

        assert [p.id for p in chain.chain] == ["openai-compatible", "mock-fallback"]

    def test_production_refuses_a_free_tier_fallback(self, monkeypatch: pytest.MonkeyPatch) -> None:
        """A paid primary with a free-hosted backup is one outage from a leak.

        `ProviderRegistry.register` runs the PII guard per provider, and
        building the chain through it is what makes that guard reach the
        fallback at all. Google's adapter declares its own
        `free-hosted` tier, so no declared `LLM_PROVIDER_TIER=paid` can
        bless it.
        """
        _configure(
            monkeypatch,
            env="production",
            llm_provider="anthropic",
            llm_provider_tier="paid",
            llm_api_key="sk-ant-not-a-real-key",
            llm_fallback_provider="google",
            llm_fallback_api_key="not-a-real-key",
        )

        with pytest.raises(UnsafeConfigurationError, match="google"):
            get_provider_chain()

    def test_development_allows_the_same_free_tier_fallback(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """The safe path still works, or the test above proves nothing.

        Identical fallback, one setting different. Free models in
        development are the entire premise of this phase, so a refusal
        here would mean the guard is rejecting the provider rather than
        the environment.
        """
        _configure(
            monkeypatch,
            env="development",
            llm_provider="openai-compatible",
            llm_fallback_provider="google",
            llm_fallback_api_key="not-a-real-key",
        )

        chain = get_provider_chain()

        assert [p.id for p in chain.chain] == ["openai-compatible", "google"]

    def test_one_adapter_twice_is_refused_as_a_chain(self, monkeypatch: pytest.MonkeyPatch) -> None:
        """Not a fallback: the same endpoint, retried under another name.

        Reached by the only production-legal fallback the settings can
        express today — `anthropic` behind `anthropic` — because §11
        defines no fallback tier, base URL or model map. The refusal
        comes from the registry, not the PII guard, and asserting which
        one fired is the point: a chain that silently registered the same
        provider twice would report a fallback it does not have.
        """
        _configure(
            monkeypatch,
            env="production",
            llm_provider="anthropic",
            llm_provider_tier="paid",
            llm_api_key="sk-ant-not-a-real-key",
            llm_fallback_provider="anthropic",
            llm_fallback_api_key="sk-ant-also-not-real",
        )

        with pytest.raises(ValueError) as caught:
            get_provider_chain()

        assert not isinstance(caught.value, UnsafeConfigurationError)
        assert "registered twice" in str(caught.value)


class TestFallbackThroughTheRealConstructionPath:
    async def test_a_dead_primary_is_served_by_the_fallback(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """Task 4.9: killing Ollama mid-request falls back cleanly.

        The primary points at a port with nothing on it, which is exactly
        what a stopped Ollama looks like: `APIConnectionError`, mapped to
        a retryable `ProviderError`, which is the registry's signal to
        walk to the next link.
        """
        _configure(
            monkeypatch,
            llm_provider="openai-compatible",
            llm_base_url=f"http://127.0.0.1:{_closed_port()}/v1",
            llm_fallback_provider="mock",
        )

        response = await get_provider_chain().complete(_request())

        assert response.provider_id == "mock-fallback"

    async def test_a_live_primary_is_not_bypassed(self, monkeypatch: pytest.MonkeyPatch) -> None:
        """The safe path: a working primary answers and the fallback never runs.

        Without this, the test above would also pass if the chain always
        served from the last link.
        """
        _configure(monkeypatch, llm_provider="mock", llm_fallback_provider="mock")

        response = await get_provider_chain().complete(_request())

        assert response.provider_id == "mock"

    async def test_a_dead_primary_with_no_fallback_fails_loudly(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """No silent success, and the primary's failure is in the message.

        Go maps this to a 503 with a retryable flag; an error that named
        nothing would leave an operator guessing which provider died.
        """
        _configure(
            monkeypatch,
            llm_provider="openai-compatible",
            llm_base_url=f"http://127.0.0.1:{_closed_port()}/v1",
        )

        with pytest.raises(NoProviderAvailableError) as caught:
            await get_provider_chain().complete(_request())

        assert "openai-compatible" in str(caught.value)

    async def test_the_orchestrator_runs_on_the_chain_not_the_primary(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """The assertion that cannot pass while the defect is present.

        Telemetry records the provider that ANSWERED, so a run whose
        primary is unreachable and whose envelope still reports a
        provider id proves the orchestrator was handed the chain rather
        than a single `ResilientProvider`.
        """
        _configure(
            monkeypatch,
            llm_provider="openai-compatible",
            llm_base_url=f"http://127.0.0.1:{_closed_port()}/v1",
            llm_fallback_provider="mock",
        )

        envelope = await get_orchestrator().complete(
            CompleteRequest(message="what does my week at work look like"),
            trace_id="t-2",
        )

        assert envelope.telemetry.provider_id == "mock-fallback"
        assert envelope.telemetry.model_calls >= 1


# ─── one factory, or the scripts measure something else ──────────────


class TestTheProviderFactory:
    """The construction lived in two places and they disagreed.

    `app/api/complete.py` matched on `LLM_PROVIDER`; both measurement
    scripts hardcoded `OpenAICompatibleProvider`. Invisible until it
    mattered: with `LLM_PROVIDER=google` the service runs on Gemini and
    `measure_intent_accuracy.py` quietly keeps talking to
    `http://localhost:11434` — printing a number for one model under a
    heading naming another.

    A measurement that silently measures something else is worse than no
    measurement.
    """

    @pytest.mark.parametrize(
        ("provider", "expected"),
        [
            ("openai-compatible", "OpenAICompatibleProvider"),
            ("google", "GoogleProvider"),
            ("anthropic", "AnthropicProvider"),
            ("mock", "MockProvider"),
        ],
    )
    def test_every_configured_provider_is_built(
        self, monkeypatch: pytest.MonkeyPatch, provider: str, expected: str
    ) -> None:
        from app.providers import provider_from_settings
        from app.settings import settings

        monkeypatch.setattr(settings, "llm_provider", provider)
        monkeypatch.setattr(settings, "llm_provider_tier", "local")
        # Anthropic refuses to construct without one, by design.
        monkeypatch.setattr(settings, "llm_api_key", "not-a-real-key-for-tests")

        assert type(provider_from_settings()).__name__ == expected

    def test_the_route_and_the_scripts_build_the_same_thing(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """The property, not just the existence of a helper.

        A factory that only the scripts used would leave the
        duplication exactly where it was.
        """
        from app.api import complete
        from app.providers import provider_from_settings
        from app.settings import settings

        monkeypatch.setattr(settings, "llm_provider", "google")
        monkeypatch.setattr(settings, "llm_api_key", "not-a-real-key-for-tests")

        assert type(complete._raw_provider()) is type(provider_from_settings())

    def test_the_model_override_reaches_every_tier(self) -> None:
        """Scripts compare models on one job.

        Pointing only `fast` would leave a script that measures a `chat`
        job silently running the configured model instead of the one
        named on the command line.
        """
        from app.providers import models_from
        from app.settings import Settings

        # A name that is NOT any tier's default. The first version used
        # "qwen2.5:7b", which IS the chat and deep default — so the
        # assertion held whether or not the override reached those
        # tiers, and the break that pointed only `fast` left it green.
        defaults = Settings(_env_file=None)  # type: ignore[call-arg]
        override = "a-model-no-tier-defaults-to"
        assert override not in (defaults.llm_model_fast, defaults.llm_model_chat)

        models = models_from(defaults, override)

        assert (models.fast, models.chat, models.deep) == (override,) * 3

    def test_describe_names_the_provider_actually_configured(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """Printed by every script before it starts.

        The failure it prevents is reading a number off a report whose
        heading names one model and whose calls went to another.
        """
        from app.providers import describe
        from app.settings import settings

        monkeypatch.setattr(settings, "llm_provider", "google")
        monkeypatch.setattr(settings, "llm_model_fast", "gemini-2.5-flash")

        line = describe()

        assert "google" in line
        assert "gemini-2.5-flash" in line
        assert "11434" not in line, "it still names the local Ollama endpoint"
