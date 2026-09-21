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
        fallback at all.

        This covers the FALLBACK position only. An earlier version of
        this docstring generalised to "no declared `LLM_PROVIDER_TIER=
        paid` can bless it", which was false of the PRIMARY until
        `TestADeclaredTierCannotBlessAFreeKey` was written — the
        fallback withheld the tier and the primary passed it. Keep the
        claim here scoped to what this test actually exercises.
        """
        _configure(
            monkeypatch,
            env="production",
            # `openai-compatible` with a DECLARED paid tier is the only
            # production-legal primary now that the Anthropic adapter is
            # gone: it is the one adapter with no vendor identity to
            # infer a tier from, so the declaration is the only signal.
            llm_provider="openai-compatible",
            llm_provider_tier="paid",
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

        §11 defines no fallback tier, base URL or model map, so a
        fallback naming the provider the primary already uses is the
        same endpoint wearing a second name. The refusal must come from
        the REGISTRY, not the PII guard, and asserting which one fired
        is the point: a chain that silently registered the same provider
        twice would report a fallback it does not have.
        """
        # Development, not production: the PII guard would otherwise
        # refuse `google` first and this test would assert the wrong
        # refusal. Which guard fires IS the point — see below.
        _configure(
            monkeypatch,
            env="development",
            llm_provider="google",
            llm_provider_tier="free-hosted",
            llm_api_key="not-a-real-key",
            llm_fallback_provider="google",
            llm_fallback_api_key="also-not-real",
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


class TestTheModelsFollowTheProvider:
    """Selecting a provider selects its models.

    The three model names used to be plain field defaults naming Ollama
    tags, so `LLM_PROVIDER=google` plus a key — the whole documented
    setup for a free Google run — sent the model name `llama3.2:3b` to
    the Gemini API. A 404 `model not found`, whose obvious readings are
    "my key is bad" and "the adapter is broken". Neither is true.

    This was not caught by the factory work that preceded it: that
    checked the adapter TYPE was right, and it was. The adapter was
    correct and being handed a model name from a different vendor.
    """

    def test_every_provider_in_the_literal_has_models(self) -> None:
        """A fifth provider cannot ship without a row.

        `DEFAULT_MODELS[self.llm_provider]` would raise KeyError at
        import time — but only for whoever configured the new provider,
        which is exactly the person least able to diagnose it. Read off
        the Literal rather than a hand-kept list, so the two cannot
        drift.
        """
        from typing import get_args, get_type_hints

        from app.settings import DEFAULT_MODELS, Settings

        declared = set(get_args(get_type_hints(Settings)["llm_provider"]))

        assert declared == set(DEFAULT_MODELS), (
            f"providers without a DEFAULT_MODELS row: {declared - set(DEFAULT_MODELS)}"
        )

    def test_every_default_model_has_a_price(self) -> None:
        """The guard that found two bugs in the row above it.

        `cost_micros` looks up an exact string and RAISES on a miss —
        deliberately, so an unpriced model can never bill zero on the
        dashboard and something real on the invoice. That makes a
        default model missing from `pricing.json` a hard failure on the
        first call to that tier.

        It caught `claude-haiku-4-5-20251001` (the table carries the
        undated alias) and a mock row naming models that do not exist.
        Neither was visible from reading either file alone; both are
        obvious the moment the two are compared.
        """
        from app.pricing import PRICES
        from app.settings import DEFAULT_MODELS

        unpriced = {
            f"{provider}:{model}"
            for provider, models in DEFAULT_MODELS.items()
            for model in models
            if model not in PRICES
        }

        assert not unpriced, f"default models with no price: {sorted(unpriced)}"

    @pytest.mark.parametrize(
        ("provider", "expected"),
        [
            ("google", ("gemini-3.5-flash-lite", "gemini-3.6-flash", "gemini-3.1-pro-preview")),
            ("openai-compatible", ("llama3.2:3b", "qwen2.5:7b", "qwen2.5:7b")),
            ("mock", ("mock-fast", "mock-chat", "mock-deep")),
        ],
    )
    def test_an_unconfigured_model_follows_the_provider(
        self,
        monkeypatch: pytest.MonkeyPatch,
        provider: str,
        expected: tuple[str, str, str],
    ) -> None:
        """Asserted on all three tiers, by exact name.

        Not `"gemini" in fast`: a substring check passes for a provider
        whose three tiers are wired to one model, which is the mistake
        §3's routing exists to prevent. The first version of this test
        made exactly that error — and caught itself on
        `openai-compatible`, whose chat tier is qwen, not llama.
        """
        from app.settings import Settings

        # Cleared so a developer's own exported vars cannot make this
        # pass — the bug being fixed is precisely about what happens
        # when NOTHING is configured.
        for var in ("LLM_MODEL_FAST", "LLM_MODEL_CHAT", "LLM_MODEL_DEEP"):
            monkeypatch.delenv(var, raising=False)

        config = Settings(_env_file=None, llm_provider=provider)  # type: ignore[call-arg]

        assert (
            config.llm_model_fast,
            config.llm_model_chat,
            config.llm_model_deep,
        ) == expected

    def test_an_explicit_model_is_never_overridden(self, monkeypatch: pytest.MonkeyPatch) -> None:
        """The other half, and the one that makes this safe.

        Filling in a default must not become silently rewriting a
        deliberate choice — an operator pinning a preview model, or
        pointing a local proxy at a Gemini-compatible endpoint.
        """
        # CHAT and DEEP cleared first. Without this the test passes on a
        # bare `pytest` and fails under `task verify`, which loads the
        # repo-root `.env` — where these were pinned. That is not a flake
        # to retry: it is this test reading the developer's machine
        # instead of its own fixture.
        for var in ("LLM_MODEL_CHAT", "LLM_MODEL_DEEP"):
            monkeypatch.delenv(var, raising=False)
        monkeypatch.setenv("LLM_MODEL_FAST", "gemini-3-preview-i-chose-this")

        from app.settings import Settings

        config = Settings(_env_file=None, llm_provider="google")  # type: ignore[call-arg]

        assert config.llm_model_fast == "gemini-3-preview-i-chose-this"
        # ...and the two nobody set still follow the provider.
        assert "gemini" in config.llm_model_chat

    def test_an_explicit_model_equal_to_the_field_default_survives(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """The case a value-comparison implementation gets wrong.

        "Fill it if it still looks like the default" passes every other
        test in this class and loses this one: running llama3.2:3b
        against a local Gemini-compatible proxy is a real configuration,
        and it must not be rewritten to `gemini-2.5-flash` because the
        chosen value happened to equal a field default.
        """
        monkeypatch.setenv("LLM_MODEL_FAST", "llama3.2:3b")

        from app.settings import Settings

        config = Settings(_env_file=None, llm_provider="google")  # type: ignore[call-arg]

        assert config.llm_model_fast == "llama3.2:3b"

    @pytest.mark.parametrize(
        "example",
        [".env.example", "services/ai/.env.example"],
    )
    def test_no_env_example_re_pins_a_model(self, example: str) -> None:
        """The hole the settings fix alone left open.

        `_default_models_to_the_provider` fills only what nobody set —
        correctly, since overriding a deliberate choice would be the
        worse bug. But both `.env.example` files shipped all three model
        names UNCOMMENTED, so the documented setup path produced a
        config where they ARE set.

        Copy the example, change `LLM_PROVIDER` to `google`, and
        `llama3.2:3b` still goes to Gemini — the exact bug, surviving the
        fix, via the file everyone starts from. Nothing in the settings
        module can catch that; only this can.
        """
        import pathlib
        import re

        root = pathlib.Path(__file__).resolve().parents[3]
        text = (root / example).read_text()

        pinned = [
            line
            for line in text.splitlines()
            if re.match(r"\s*LLM_MODEL_(FAST|CHAT|DEEP)\s*=", line)
        ]

        assert not pinned, (
            f"{example} pins {pinned}, so changing only LLM_PROVIDER leaves the "
            f"previous provider's model names in place. Comment them out."
        )

    def test_describe_names_the_model_that_will_be_called(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """`describe()` is what a measurement run prints as its heading.

        It printed `fast=llama3.2:3b` for a Google run, which is the
        report line that would have made the mismatch obvious — and
        would itself have been wrong.
        """
        for var in ("LLM_MODEL_FAST", "LLM_MODEL_CHAT", "LLM_MODEL_DEEP"):
            monkeypatch.delenv(var, raising=False)

        from app.providers import describe
        from app.settings import Settings

        line = describe(Settings(_env_file=None, llm_provider="google"))  # type: ignore[call-arg]

        assert "gemini" in line
        assert "llama" not in line

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


class TestADeclaredTierCannotBlessAFreeKey:
    """§17's PII guard, checked in the PRIMARY position.

    `TestChainConstruction.test_production_refuses_a_free_tier_fallback`
    covers the FALLBACK, and its docstring generalised from that to "no
    declared `LLM_PROVIDER_TIER=paid` can bless it". Executing the check
    rather than reading it showed that was false where it matters most:

        ENV=production LLM_PROVIDER=google LLM_PROVIDER_TIER=paid  -> BOOTED

    `_fallback_provider` withholds `tier` on purpose and says why —
    "handing it the primary's DECLARED tier is precisely how a free
    Gemini key gets blessed as paid and receives birth data in
    production" — while the primary passed it. The hole the fallback
    refused to open was open one line away, in the position that takes
    ALL the traffic rather than only the outage traffic.

    A key string cannot be inspected for whether billing is attached, so
    the only safe reading of a Google key is the free one. Invariant 3
    is not an assertion an operator gets to make on the vendor's behalf.
    """

    @pytest.mark.parametrize(
        ("provider", "key"),
        [("google", "not-a-real-key"), ("mock", "")],
    )
    def test_production_refuses_a_free_provider_declared_paid(
        self, monkeypatch: pytest.MonkeyPatch, provider: str, key: str
    ) -> None:
        _configure(
            monkeypatch,
            env="production",
            llm_provider=provider,
            # The lie under test.
            llm_provider_tier="paid",
            llm_api_key=key,
        )

        with pytest.raises(UnsafeConfigurationError):
            get_provider_chain()

    def test_a_genuinely_paid_provider_still_boots(self, monkeypatch: pytest.MonkeyPatch) -> None:
        """Without this the test above is satisfied by refusing everything,
        and production could not run at all."""
        _configure(
            monkeypatch,
            env="production",
            llm_provider="openai-compatible",
            llm_provider_tier="paid",
        )

        assert [p.id for p in get_provider_chain().chain] == ["openai-compatible"]

    def test_development_still_allows_the_free_provider(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """Free models in development are the premise of this phase.

        A refusal here would mean the guard is rejecting the PROVIDER
        rather than the environment.
        """
        _configure(
            monkeypatch,
            env="development",
            llm_provider="google",
            llm_provider_tier="free-hosted",
            llm_api_key="not-a-real-key",
        )

        assert [p.id for p in get_provider_chain().chain] == ["google"]

    def test_openai_compatible_still_takes_its_declared_tier(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """The deliberate exception, pinned so it is not "fixed" later.

        `openai-compatible` has no vendor identity to infer a tier from
        — the same adapter reaches Ollama on localhost and a paid
        inference host. Its tier can only be declared, so unlike Google
        the declaration is the only signal there is.

        Stated here rather than left implicit, because the asymmetry
        looks like an oversight until you know why.
        """
        _configure(
            monkeypatch,
            env="production",
            llm_provider="openai-compatible",
            llm_provider_tier="paid",
        )

        assert [p.id for p in get_provider_chain().chain] == ["openai-compatible"]
