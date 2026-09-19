"""Retry and the breaker, on a fake clock so nothing sleeps."""

from __future__ import annotations

from collections.abc import AsyncIterator

import pytest

from app.providers import (
    BreakerState,
    Capabilities,
    CircuitOpenError,
    CompletionChunk,
    CompletionRequest,
    CompletionResponse,
    Message,
    ProviderError,
    ProviderTier,
    RequestMetadata,
    ResilientProvider,
    Usage,
)


class Clock:
    """A clock the test advances by hand.

    Real sleeps would make this suite take minutes for a 60-second
    breaker window, and a `time.sleep`-based test is a test people
    eventually mark slow and stop running.
    """

    def __init__(self) -> None:
        self.t = 1000.0
        self.slept: list[float] = []

    def now(self) -> float:
        return self.t

    async def sleep(self, seconds: float) -> None:
        # Recorded, not performed — the backoff schedule is asserted on.
        self.slept.append(seconds)


class Flaky:
    """Fails a given number of times, then succeeds."""

    def __init__(self, failures: int, *, retryable: bool = True) -> None:
        self._remaining = failures
        self._retryable = retryable
        self.calls = 0

    @property
    def id(self) -> str:
        return "flaky"

    @property
    def tier(self) -> ProviderTier:
        return "local"

    @property
    def capabilities(self) -> Capabilities:
        return Capabilities()

    async def complete(self, req: CompletionRequest) -> CompletionResponse:
        self.calls += 1
        if self._remaining > 0:
            self._remaining -= 1
            raise ProviderError("transient", provider_id="flaky", retryable=self._retryable)
        return CompletionResponse(
            text="ok",
            finish_reason="stop",
            usage=Usage(),
            model="m",
            provider_id="flaky",
        )

    async def stream(self, req: CompletionRequest) -> AsyncIterator[CompletionChunk]:
        self.calls += 1
        if self._remaining > 0:
            self._remaining -= 1
            raise ProviderError("transient", provider_id="flaky", retryable=True)
        yield CompletionChunk(text="ok", finish_reason="stop")

    async def health_check(self) -> bool:
        return True


def a_request() -> CompletionRequest:
    return CompletionRequest(
        messages=[Message(role="user", content="hi")],
        tier="fast",
        metadata=RequestMetadata(trace_id="t"),
    )


def wrap(inner: Flaky, clock: Clock, **kwargs: object) -> ResilientProvider:
    return ResilientProvider(
        inner,
        now=clock.now,
        sleep=clock.sleep,
        **kwargs,  # type: ignore[arg-type]
    )


class TestRetry:
    async def test_a_transient_failure_is_retried_and_then_succeeds(self) -> None:
        clock = Clock()
        inner = Flaky(failures=2)

        response = await wrap(inner, clock).complete(a_request())

        assert response.text == "ok"
        assert inner.calls == 3

    async def test_retries_stop_at_the_budget(self) -> None:
        clock = Clock()
        inner = Flaky(failures=99)

        with pytest.raises(ProviderError):
            await wrap(inner, clock, max_attempts=3).complete(a_request())

        assert inner.calls == 3, "the retry budget was not honoured"

    async def test_a_permanent_failure_is_not_retried(self) -> None:
        """A 400 fails identically however many times it is sent.

        Retrying it burns the budget and the latency for a request that
        cannot succeed, and the user waits three timeouts for an answer
        that was impossible at the first.
        """
        clock = Clock()
        inner = Flaky(failures=99, retryable=False)

        with pytest.raises(ProviderError):
            await wrap(inner, clock).complete(a_request())

        assert inner.calls == 1

    async def test_backoff_grows_and_is_capped(self) -> None:
        clock = Clock()
        inner = Flaky(failures=99)

        with pytest.raises(ProviderError):
            await wrap(
                inner, clock, max_attempts=5, base_delay_seconds=1.0, max_delay_seconds=4.0
            ).complete(a_request())

        # Full jitter means each delay is a draw from [0, cap], so the
        # assertion is on the CAP rather than on the value: 1, 2, 4, 4.
        assert len(clock.slept) == 4
        assert all(d <= 4.0 for d in clock.slept)

    async def test_no_sleep_after_the_final_attempt(self) -> None:
        # Sleeping after the last try delays the failure the caller is
        # already waiting for, and buys nothing.
        clock = Clock()

        with pytest.raises(ProviderError):
            await wrap(Flaky(failures=99), clock, max_attempts=3).complete(a_request())

        assert len(clock.slept) == 2


class TestTheBreaker:
    async def test_it_opens_after_the_threshold(self) -> None:
        clock = Clock()
        provider = wrap(Flaky(failures=99), clock, max_attempts=1, failure_threshold=3)

        for _ in range(3):
            with pytest.raises(ProviderError):
                await provider.complete(a_request())

        assert provider.state is BreakerState.OPEN

    async def test_an_open_circuit_fails_without_calling_the_provider(self) -> None:
        """The whole point.

        Retrying a dead provider is worse than not retrying: every
        request pays the full timeout before failing over, so one dead
        backend turns a fast fallback into a slow one for every user at
        once.
        """
        clock = Clock()
        inner = Flaky(failures=99)
        provider = wrap(inner, clock, max_attempts=1, failure_threshold=2)

        for _ in range(2):
            with pytest.raises(ProviderError):
                await provider.complete(a_request())

        calls_before = inner.calls

        with pytest.raises(CircuitOpenError):
            await provider.complete(a_request())

        assert inner.calls == calls_before, "the provider was called through an open circuit"

    async def test_an_open_circuit_is_retryable_so_the_chain_moves_on(self) -> None:
        # Failing fast is only useful if the registry then tries someone
        # else. A non-retryable CircuitOpenError would turn a healthy
        # fallback into an outage.
        clock = Clock()
        provider = wrap(Flaky(failures=99), clock, max_attempts=1, failure_threshold=1)

        with pytest.raises(ProviderError):
            await provider.complete(a_request())

        with pytest.raises(CircuitOpenError) as caught:
            await provider.complete(a_request())

        assert caught.value.retryable is True

    async def test_it_half_opens_after_the_window(self) -> None:
        clock = Clock()
        provider = wrap(
            Flaky(failures=99), clock, max_attempts=1, failure_threshold=1, open_seconds=60
        )

        with pytest.raises(ProviderError):
            await provider.complete(a_request())
        assert provider.state is BreakerState.OPEN

        clock.t += 61
        assert provider.state is BreakerState.HALF_OPEN

    async def test_a_success_closes_it_again(self) -> None:
        clock = Clock()
        inner = Flaky(failures=1)
        provider = wrap(inner, clock, max_attempts=1, failure_threshold=1, open_seconds=60)

        with pytest.raises(ProviderError):
            await provider.complete(a_request())

        clock.t += 61
        await provider.complete(a_request())

        assert provider.state is BreakerState.CLOSED

    async def test_a_permanent_failure_does_not_open_the_circuit(self) -> None:
        """One bad caller must not take the provider down for everybody.

        A malformed request fails at a perfectly healthy backend. Counting
        it toward the breaker lets a client with a bug open the circuit
        for every other user of that provider.
        """
        clock = Clock()
        provider = wrap(
            Flaky(failures=99, retryable=False), clock, max_attempts=1, failure_threshold=2
        )

        for _ in range(5):
            with pytest.raises(ProviderError):
                await provider.complete(a_request())

        assert provider.state is BreakerState.CLOSED


async def test_a_wrapped_provider_is_still_a_provider() -> None:
    # The registry must not be able to tell the difference, or wrapping
    # becomes a decision every call site has to know about.
    clock = Clock()
    provider = wrap(Flaky(failures=0), clock)

    assert provider.id == "flaky"
    assert provider.tier == "local"
    assert await provider.health_check() is True
