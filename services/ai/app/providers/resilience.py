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

# ─── the one retry table ─────────────────────────────────────────────
#
# Every adapter classifies a status code, and the registry's fallback
# loop reads the resulting `retryable` and NOTHING else. Three adapters
# keeping three private tables is therefore three chances to disagree
# about the same outage — and they did: the OpenAI adapter listed 5xx
# code by code and so marked a 529 ("overloaded", which some
# vendors and proxies send) and a
# Cloudflare 520/522/524 from an OpenRouter-style proxy permanent, which
# stops the chain walking to a provider that was up the whole time.
#
# It lives beside the retry policy rather than in `base.py` because it IS
# the policy: `base.py` defines what a failure looks like, this decides
# what to do about one.

_RETRYABLE_CLIENT_STATUS = frozenset(
    {
        # The server gave up waiting for the request, not the other way
        # round. Nothing about the request is wrong.
        408,
        # Transient resource conflict. These APIs use it for state that
        # settles on its own.
        409,
        # "Too Early" — replay protection on an early-data request. The
        # server is asking for the same request later, literally.
        425,
        # Rate limited. The canonical retryable failure.
        429,
    }
)


# ─── which transport phase a failure happened in ─────────────────────
#
# Matched by class NAME, never by isinstance, and shared by both adapters
# so they cannot drift apart on a question this expensive.
#
# The reason is concrete: this environment has TWO httpx distributions
# installed. `httpx` 0.28.1 is what the adapters import; the OpenAI SDK
# raises from `httpx2`, and `google.genai._api_client` imports both. So
#
#     httpx.ConnectError is httpx2.ConnectError   ->  False
#
# and an isinstance check binds to whichever package the checking module
# happened to import. In the OpenAI adapter that made the
# connect-timeout-is-retryable branch dead code — every timeout was
# classified "may have been billed" — while its test passed because the
# test built the cause by hand from the other httpx.
#
# Names are stable across both distributions, and a new vendored copy
# changes nothing.

BEFORE_ANY_BYTE_WAS_SENT = frozenset(
    {
        "ConnectError",  # refused, DNS failure, TLS handshake
        "ConnectTimeout",
        "PoolTimeout",
    }
)
"""Nothing left the process, so nothing could have been generated or billed."""

SENT_THEN_LOST = frozenset(
    {
        "ReadError",  # the mid-run kill: request read in full, then RST
        "ReadTimeout",
        "WriteError",
        "WriteTimeout",
        "RemoteProtocolError",
    }
)
"""The bytes went out and only the answer was lost. May have been billed."""

TIMEOUT_PHASES = frozenset(
    {"ConnectTimeout", "PoolTimeout", "ReadTimeout", "WriteTimeout", "TimeoutException"}
)
"""Every phase name that is a timeout rather than a connection failure."""


def is_retryable_status(status: int) -> bool:
    """Should the chain try again, here or at the next provider?

    Every 5xx, plus the four client codes above. The 5xx clause is a
    RANGE rather than an enumeration on purpose: the enumeration is what
    broke, because the interesting codes are the ones nobody thinks of —
    529 from an overloaded upstream, 520/522/524 from a Cloudflare edge
    in front of a proxy. A range cannot omit next year's.

    501 "Not Implemented" sits inside that range and is deliberately left
    there, even though it is semantically permanent. `retryable` does not
    mean "send the same bytes to the same box again"; it means "let the
    chain try someone else", and a backend that has not implemented an
    endpoint or a parameter is precisely the case where the NEXT provider
    has. Excluding it would dead-end the request at the one provider that
    structurally cannot serve it. The price of including it is bounded at
    `max_attempts` wasted calls; the price of excluding it is an outage
    with a working fallback sitting idle.
    """
    return status >= 500 or status in _RETRYABLE_CLIENT_STATUS


class BreakerState(StrEnum):
    CLOSED = "closed"
    OPEN = "open"
    HALF_OPEN = "half_open"


class UnsafeToReplayError(ProviderError):
    """The request reached the provider; only the answer was lost.

    A read timeout is not a failed call — it is a call with an UNKNOWN
    outcome. The completion may have run to its last token and been
    billed in full, with the response dropped on the way back. Sending it
    again buys a second bill for an answer already paid for, and
    `internal/platform/clients/ai_complete.go` refuses to replay a
    completion for exactly this reason ("replaying a completion spends
    money again and may produce a different answer").

    Still `retryable=True`, and the distinction matters: that flag
    belongs to the REGISTRY and means "let the chain try someone else",
    which is a different provider and a different, single bill. What this
    type forbids is the narrower thing — `ResilientProvider` sending an
    identical request straight back to the provider that may have just
    taken our money.

    A connect or pool timeout is NOT this: no request left the process,
    so nothing could have been billed. Adapters raise a plain
    `ProviderError` there and it is retried normally.
    """

    def __init__(self, message: str, *, provider_id: str, status_code: int | None = None) -> None:
        super().__init__(
            message,
            provider_id=provider_id,
            retryable=True,
            status_code=status_code,
        )


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
            except UnsafeToReplayError:
                # Counted toward the breaker — a provider that stopped
                # answering is unhealthy whether or not we may call it
                # again — but never re-sent from here. The request may
                # already have been generated and billed in full; see the
                # class docstring. The registry may still fail over to a
                # DIFFERENT provider, which is one more bill rather than
                # the same one twice.
                self._record_failure()
                raise
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
