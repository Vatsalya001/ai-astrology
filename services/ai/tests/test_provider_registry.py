"""The fallback chain, and the guard that sits in front of it.

Every test here asserts a decision that could plausibly have gone the
other way. There is no test that "registering a provider stores it" —
that is the language working, not the code.
"""

from __future__ import annotations

from collections.abc import AsyncIterator

import pytest

from app.guards import UnsafeConfigurationError
from app.providers import (
    Capabilities,
    CompletionChunk,
    CompletionRequest,
    CompletionResponse,
    Message,
    NoProviderAvailableError,
    ProviderError,
    ProviderRegistry,
    ProviderTier,
    RequestMetadata,
    Usage,
)


class FakeProvider:
    """A provider shaped exactly like the protocol and nothing more.

    Structural typing means this needs no base class — which is the point
    of `Protocol` over ABC, and is why a test double here is ten lines
    rather than an inheritance ceremony.
    """

    def __init__(
        self,
        provider_id: str,
        *,
        tier: ProviderTier = "local",
        fail: Exception | None = None,
        healthy: bool = True,
    ) -> None:
        self._id = provider_id
        self._tier: ProviderTier = tier
        self._fail = fail
        self._healthy = healthy
        self.calls = 0

    @property
    def id(self) -> str:
        return self._id

    @property
    def tier(self) -> ProviderTier:
        return self._tier

    @property
    def capabilities(self) -> Capabilities:
        return Capabilities()

    async def complete(self, req: CompletionRequest) -> CompletionResponse:
        self.calls += 1
        if self._fail is not None:
            raise self._fail
        return CompletionResponse(
            text=f"answered by {self._id}",
            finish_reason="stop",
            usage=Usage(input_tokens=1, output_tokens=1),
            model="fake",
            provider_id=self._id,
            latency_ms=1,
        )

    async def stream(self, req: CompletionRequest) -> AsyncIterator[CompletionChunk]:
        self.calls += 1
        yield CompletionChunk(text=f"streamed by {self._id}", finish_reason="stop")

    async def health_check(self) -> bool:
        return self._healthy


def a_request() -> CompletionRequest:
    return CompletionRequest(
        messages=[Message(role="user", content="hello")],
        tier="fast",
        metadata=RequestMetadata(trace_id="t-1"),
    )


# ─── the guard, at the moment of registration ────────────────────────


class TestTheGuardRunsAtRegistration:
    """`guards.py` owns the rule; the registry owns the moment.

    Checking at call time would be too late — the process would already
    be serving, and the first request to reach a free tier would carry
    the birth data this guard exists to keep off it.
    """

    def test_production_refuses_a_local_provider(self) -> None:
        registry = ProviderRegistry(env="production")

        with pytest.raises(UnsafeConfigurationError):
            registry.register(FakeProvider("ollama", tier="local"))

    def test_production_refuses_a_free_hosted_provider(self) -> None:
        # The one that actually matters: 'local' is blocked on
        # reliability grounds, but 'free-hosted' is the privacy case —
        # most free tiers reserve the right to train on their inputs.
        registry = ProviderRegistry(env="production")

        with pytest.raises(UnsafeConfigurationError):
            registry.register(FakeProvider("groq", tier="free-hosted"))

    def test_production_accepts_a_paid_provider(self) -> None:
        # The negative case for the guard itself: a guard that blocks
        # everything is not a guard, it is an outage.
        registry = ProviderRegistry(env="production")
        registry.register(FakeProvider("anthropic", tier="paid"))

        assert registry.primary.id == "anthropic"

    def test_development_accepts_a_local_provider(self) -> None:
        registry = ProviderRegistry(env="development")
        registry.register(FakeProvider("ollama", tier="local"))

        assert registry.primary.id == "ollama"

    def test_a_blocked_provider_does_not_enter_the_chain(self) -> None:
        """Even if the caller swallows the exception.

        The guard runs BEFORE the append, so a caller that catches
        `UnsafeConfigurationError` and carries on cannot end up with a
        half-registered provider quietly serving traffic. That ordering
        is the whole protection and is invisible from the outside.
        """
        registry = ProviderRegistry(env="production")

        with pytest.raises(UnsafeConfigurationError):
            registry.register(FakeProvider("ollama", tier="local"))

        assert registry.chain == ()


def test_the_same_provider_cannot_be_registered_twice() -> None:
    """A duplicate is not a fallback.

    Registering one endpoint twice produces a chain that "fails over"
    from a provider to itself: the second attempt hits the same dead
    endpoint, waits the same timeout, and fails the same way — while the
    logs read as though a backup was tried.
    """
    registry = ProviderRegistry(env="development")
    registry.register(FakeProvider("ollama"))

    with pytest.raises(ValueError, match="registered twice"):
        registry.register(FakeProvider("ollama"))


# ─── the fallback chain ──────────────────────────────────────────────


class TestFallback:
    async def test_a_retryable_failure_moves_to_the_next_provider(self) -> None:
        primary = FakeProvider(
            "ollama",
            fail=ProviderError("timeout", provider_id="ollama", retryable=True),
        )
        backup = FakeProvider("google")

        registry = ProviderRegistry(env="development")
        registry.register(primary)
        registry.register(backup)

        response = await registry.complete(a_request())

        assert response.provider_id == "google"
        assert primary.calls == 1, "the primary was never tried"

    async def test_a_non_retryable_failure_stops_the_chain(self) -> None:
        """The decision this class exists for.

        A 400 from a malformed request fails identically at every
        provider. Walking a three-provider chain with it turns one bad
        request into three — three times the latency and the token spend,
        and a log that blames the last provider for the first one's
        mistake.
        """
        primary = FakeProvider(
            "ollama",
            fail=ProviderError(
                "bad request", provider_id="ollama", retryable=False, status_code=400
            ),
        )
        backup = FakeProvider("google")

        registry = ProviderRegistry(env="development")
        registry.register(primary)
        registry.register(backup)

        with pytest.raises(ProviderError):
            await registry.complete(a_request())

        assert backup.calls == 0, (
            "a non-retryable failure was failed over — the same malformed request "
            "is now being sent to every provider in the chain"
        )

    async def test_an_unclassified_exception_is_treated_as_retryable(self) -> None:
        """Adapters SHOULD translate to ProviderError. This is for when one does not.

        A vendor SDK can raise anything, and a single unmapped exception
        type should degrade one provider rather than take down every
        request in the service.
        """
        primary = FakeProvider("ollama", fail=RuntimeError("SDK exploded"))
        backup = FakeProvider("google")

        registry = ProviderRegistry(env="development")
        registry.register(primary)
        registry.register(backup)

        response = await registry.complete(a_request())

        assert response.provider_id == "google"

    async def test_every_failure_is_reported_not_just_the_last(self) -> None:
        """With a chain, the LAST error is usually the least informative.

        The primary's timeout is the thing worth reading; the backup's
        "model not found" is a configuration detail about a provider
        nobody intended to use today.
        """
        registry = ProviderRegistry(env="development")
        registry.register(
            FakeProvider(
                "ollama", fail=ProviderError("timed out", provider_id="ollama", retryable=True)
            )
        )
        registry.register(
            FakeProvider(
                "google",
                fail=ProviderError("model not found", provider_id="google", retryable=True),
            )
        )

        with pytest.raises(NoProviderAvailableError) as caught:
            await registry.complete(a_request())

        assert set(caught.value.failures) == {"ollama", "google"}
        assert "timed out" in str(caught.value)
        assert "model not found" in str(caught.value)

    async def test_an_empty_registry_says_so_rather_than_indexing(self) -> None:
        registry = ProviderRegistry(env="development")

        with pytest.raises(NoProviderAvailableError, match="no LLM provider is registered"):
            await registry.complete(a_request())


class TestStreaming:
    async def test_streaming_does_not_fail_over(self) -> None:
        """Deliberate, not an omission.

        Once the first chunk has reached the user, switching providers
        mid-response splices two different models' prose together. A
        stream that fails before its first chunk is the caller's to
        retry — against the chain, via `complete`.
        """
        primary = FakeProvider("ollama")
        backup = FakeProvider("google")

        registry = ProviderRegistry(env="development")
        registry.register(primary)
        registry.register(backup)

        chunks = [chunk async for chunk in registry.stream(a_request())]

        assert chunks[0].text == "streamed by ollama"
        assert backup.calls == 0


async def test_health_is_reported_per_provider() -> None:
    """Not reduced to one boolean.

    "The primary is down but the backup is up" is a materially different
    situation from "everything is down", and a single flag cannot say
    which one is happening — which is exactly what an operator needs at
    the moment they look.
    """
    registry = ProviderRegistry(env="development")
    registry.register(FakeProvider("ollama", healthy=False))
    registry.register(FakeProvider("google", healthy=True))

    assert await registry.health() == {"ollama": False, "google": True}
