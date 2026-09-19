"""Retry, backoff and a circuit breaker, as a provider decorator.

── Why a decorator and not logic inside the registry ──

The registry chooses WHICH provider; this decides how hard to try one.
Keeping them separate means a provider can be wrapped or not, wrapped
with different budgets, or tested on its own — and it keeps the
registry's fallback loop readable, which matters because that loop is
where a wrong `retryable` decision does the most damage.

A wrapped provider is still an `LLMProvider`, so the registry cannot tell
the difference and nothing downstream changes.

── Why the breaker exists at all ──

Retrying a provider that is down is worse than not retrying: every
request pays the full timeout before failing over, so a dead Ollama turns
a 200 ms fallback into a 60-second one for every user at once. The
breaker remembers that it is down and fails immediately, which is the
difference between degraded and unusable.
"""

from __future__ import annotations

import asyncio
import random
import time
from collections.abc import AsyncIterator, Awaitable, Callable
from enum import StrEnum

from app.providers.base import (
    Capabilities,
    CompletionChunk,
    CompletionRequest,
    CompletionResponse,
    LLMProvider,
    ProviderError,
    ProviderTier,
)


class BreakerState(StrEnum):
    CLOSED = "closed"
    OPEN = "open"
    HALF_OPEN = "half_open"


class CircuitOpenError(ProviderError):
    """The breaker refused before the call was made.

    Retryable, because the point is to fail over NOW rather than after a
    timeout — a closed circuit elsewhere in the chain is exactly what
    should serve this request.
    """

    def __init__(self, provider_id: str, opens_for: float) -> None:
        super().__init__(
            f"{provider_id} circuit is open for another {opens_for:.1f}s — failing "
            f"fast instead of paying the timeout again",
            provider_id=provider_id,
            retryable=True,
        )


class ResilientProvider:
    """Any provider, with a retry budget and a breaker in front of it."""

    def __init__(
        self,
        inner: LLMProvider,
        *,
        max_attempts: int = 3,
        base_delay_seconds: float = 0.25,
        max_delay_seconds: float = 8.0,
        failure_threshold: int = 5,
        open_seconds: float = 60.0,
        # Injected so tests do not sleep and do not depend on wall-clock.
        # `time.monotonic` rather than `time.time`: a clock adjustment
        # mid-outage must not make a breaker think an hour has passed.
        now: Callable[[], float] = time.monotonic,
        sleep: Callable[[float], Awaitable[None]] = asyncio.sleep,
    ) -> None:
        self._inner = inner
        self._max_attempts = max_attempts
        self._base_delay = base_delay_seconds
        self._max_delay = max_delay_seconds
        self._failure_threshold = failure_threshold
        self._open_seconds = open_seconds
        self._now = now
        self._sleep = sleep

        self._consecutive_failures = 0
        self._opened_at: float | None = None

    # ─── the protocol, forwarded ─────────────────────────────────────

    @property
    def id(self) -> str:
        return self._inner.id

    @property
    def tier(self) -> ProviderTier:
        return self._inner.tier

    @property
    def capabilities(self) -> Capabilities:
        return self._inner.capabilities

    # ─── the breaker ─────────────────────────────────────────────────

    @property
    def state(self) -> BreakerState:
        if self._opened_at is None:
            return BreakerState.CLOSED
        if self._now() - self._opened_at >= self._open_seconds:
            # Half-open: the next call is a probe. Not reset to CLOSED
            # here, because a provider that is still down would then take
            # `failure_threshold` more full-timeout requests to reopen —
            # and every one of those is a user waiting.
            return BreakerState.HALF_OPEN
        return BreakerState.OPEN

    def _record_success(self) -> None:
        self._consecutive_failures = 0
        self._opened_at = None

    def _record_failure(self) -> None:
        self._consecutive_failures += 1
        if self._consecutive_failures >= self._failure_threshold:
            self._opened_at = self._now()

    def _delay_for(self, attempt: int) -> float:
        """Exponential backoff with full jitter.

        Full jitter — a uniform draw from [0, capped] rather than
        `capped ± a bit` — because the failure mode being avoided is
        synchronised retry. Without it, every request that failed at the
        same moment retries at the same moment, and a provider recovering
        from overload is immediately knocked over again by the thundering
        herd it just shed.
        """
        capped = min(self._base_delay * (2**attempt), self._max_delay)
        return random.uniform(0, capped)

    # ─── calls ───────────────────────────────────────────────────────

    async def complete(self, req: CompletionRequest) -> CompletionResponse:
        state = self.state
        if state is BreakerState.OPEN:
            assert self._opened_at is not None
            remaining = self._open_seconds - (self._now() - self._opened_at)
            raise CircuitOpenError(self.id, remaining)

        last: ProviderError | None = None

        for attempt in range(self._max_attempts):
            try:
                response = await self._inner.complete(req)
            except ProviderError as err:
                if not err.retryable:
                    # A permanent failure says nothing about the
                    # provider's health — a malformed request would fail
                    # at a perfectly healthy backend. Counting it toward
                    # the breaker would let one bad caller open the
                    # circuit for everybody.
                    raise
                last = err
                self._record_failure()
                if attempt < self._max_attempts - 1:
                    await self._sleep(self._delay_for(attempt))
            else:
                self._record_success()
                return response

        assert last is not None
        raise last

    async def stream(self, req: CompletionRequest) -> AsyncIterator[CompletionChunk]:
        """Breaker respected; retries deliberately not applied.

        Retrying a stream means either replaying chunks the consumer has
        already seen or silently dropping the first part of an answer.
        The breaker still guards it, so a dead provider fails fast rather
        than hanging — but a stream that fails mid-flight is the caller's
        to handle.
        """
        state = self.state
        if state is BreakerState.OPEN:
            assert self._opened_at is not None
            raise CircuitOpenError(self.id, self._open_seconds - (self._now() - self._opened_at))

        try:
            async for chunk in self._inner.stream(req):
                yield chunk
        except ProviderError:
            self._record_failure()
            raise
        else:
            self._record_success()

    async def health_check(self) -> bool:
        return await self._inner.health_check()
