"""Proof that the network blocker fires, and that it is not too broad.

`.claude/rules/testing.md`: a guard never observed to fire is a guard
you cannot trust. This one protects a real API key, so "it is probably
working" is not good enough.
"""

from __future__ import annotations

import socket

import pytest

from tests.conftest import ExternalNetworkCallError


class TestTheBlockerFires:
    @pytest.mark.parametrize(
        "address",
        [
            ("api.groq.com", 443),
            ("api.anthropic.com", 443),
            ("generativelanguage.googleapis.com", 443),
            ("8.8.8.8", 53),
        ],
    )
    def test_an_external_connection_is_refused(self, address: tuple[str, int]) -> None:
        with pytest.raises(ExternalNetworkCallError):
            socket.create_connection(address, timeout=1)

    def test_the_socket_method_is_blocked_too(self) -> None:
        """`create_connection` is the common path; `connect` is not.

        httpx and the vendor SDKs reach the kernel through several
        routes, so blocking only the convenience function would leave a
        hole the next dependency upgrade walks through.
        """
        with socket.socket() as s, pytest.raises(ExternalNetworkCallError):
            s.connect(("api.groq.com", 443))

    def test_connect_ex_is_blocked_too(self) -> None:
        # `connect_ex` returns an errno instead of raising, so a caller
        # using it would otherwise slip past a guard that only covers
        # `connect`.
        with socket.socket() as s, pytest.raises(ExternalNetworkCallError):
            s.connect_ex(("api.groq.com", 443))


class TestTheBlockerIsNotTooBroad:
    def test_loopback_is_still_allowed(self) -> None:
        """Otherwise this guard breaks the tests that need a real socket.

        `test_a_dead_primary_is_served_by_the_fallback` connects to a
        closed local port to reproduce a stopped Ollama, and must get a
        genuine `ConnectionRefusedError` — if it got this guard's error
        instead it would be exercising the guard, not the fallback.
        """
        with socket.socket() as probe:
            probe.bind(("127.0.0.1", 0))
            port = probe.getsockname()[1]

        with socket.socket() as s:
            s.settimeout(1)
            # Refused, not blocked: the distinction is the whole point.
            with pytest.raises(ConnectionRefusedError):
                s.connect(("127.0.0.1", port))

    def test_binding_and_listening_still_work(self) -> None:
        # The guard covers outbound connections only. TestClient and
        # several fixtures bind local sockets.
        with socket.socket() as s:
            s.bind(("127.0.0.1", 0))
            s.listen(1)
            assert s.getsockname()[0] == "127.0.0.1"


def _caused_by_the_guard(err: BaseException | None, seen: list[str]) -> bool:
    """Is `ExternalNetworkCallError` anywhere in this exception's history?

    A linear `__cause__` walk is not enough. anyio runs the SDK's
    transport in a task group, so the guard's exception arrives nested
    inside a `BaseExceptionGroup`, and following `__cause__` from the
    outside walks into the `CancelledError` sibling instead. The real
    chain observed here is:

        ProviderError -> APIConnectionError -> ExceptionGroup -> [ ... ]

    which is why the first attempt at tightening this test failed against
    a guard that was working perfectly.
    """
    if err is None:
        return False
    seen.append(type(err).__name__)
    if isinstance(err, ExternalNetworkCallError):
        return True
    for nested in getattr(err, "exceptions", ()):  # BaseExceptionGroup
        if _caused_by_the_guard(nested, seen):
            return True
    return _caused_by_the_guard(err.__cause__ or err.__context__, seen)


class TestTheRealProviderIsNotReachable:
    async def test_a_live_provider_call_is_stopped(self, monkeypatch: pytest.MonkeyPatch) -> None:
        """The scenario this file exists for, end to end.

        The vendor endpoint is pinned HERE rather than read from
        settings. The first version built the provider "the way a
        careless test would" — straight from settings — and that made it
        read the machine instead of its own fixture: run directly it
        pointed at a vendor and raised, run under `task verify` it
        pointed at `localhost:11434`, which this guard deliberately
        ALLOWS, and Ollama answered. Green alone, red in the suite.

        It was also asserting the wrong property. The rule is "no
        EXTERNAL call", not "no call" — a local Ollama answering is
        correct behaviour, not a leak.
        """
        from app.providers import (
            CompletionRequest,
            Message,
            RequestMetadata,
            provider_from_settings,
        )
        from app.settings import settings

        monkeypatch.setattr(settings, "llm_provider", "openai-compatible")
        monkeypatch.setattr(settings, "llm_base_url", "https://api.groq.com/openai/v1")
        monkeypatch.setattr(settings, "llm_api_key", "gsk-not-a-real-key")
        monkeypatch.setattr(settings, "llm_provider_tier", "free-hosted")

        provider = provider_from_settings(model_override="openai/gpt-oss-120b")
        request = CompletionRequest(
            messages=[Message(role="user", content="what about my career")],
            tier="fast",
            metadata=RequestMetadata(trace_id="net-guard"),
        )

        # ONLY ExternalNetworkCallError, and the tuple it replaced is the
        # reason. `(ExternalNetworkCallError, ProviderError, OSError)`
        # was satisfied by the fake key's 401 (wrapped as ProviderError)
        # and by any runner without egress (OSError) — so this test
        # PASSED with the guard fully neutered, while making a real
        # outbound TCP attempt to the vendor. The one test whose
        # docstring calls itself "the scenario this file exists for" was
        # the one that proved nothing.
        #
        # The adapter wraps the guard's exception, so unwrap the cause
        # chain rather than widening the expectation again.
        with pytest.raises(Exception) as caught:
            await provider.complete(request)

        seen: list[str] = []
        assert _caused_by_the_guard(caught.value, seen), (
            f"the call failed, but not because the guard stopped it: {seen}. "
            f"A 401 from a fake key or a runner with no egress would look "
            f"identical, which is how this test used to pass with the guard off."
        )

    async def test_a_local_provider_is_still_reachable(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """The other half, which the first version silently inverted.

        A loopback endpoint must NOT be blocked. Asserted on the
        exception type rather than on a successful answer, so this does
        not require Ollama to be running: a refused connection is proof
        the attempt reached the kernel, whereas
        `ExternalNetworkCallError` would mean the guard over-reached.
        """
        from app.providers import (
            CompletionRequest,
            Message,
            RequestMetadata,
            provider_from_settings,
        )
        from app.settings import settings

        monkeypatch.setattr(settings, "llm_provider", "openai-compatible")
        monkeypatch.setattr(settings, "llm_base_url", "http://127.0.0.1:1/v1")
        monkeypatch.setattr(settings, "llm_provider_tier", "local")

        provider = provider_from_settings(model_override="llama3.2:3b")
        request = CompletionRequest(
            messages=[Message(role="user", content="hi")],
            tier="fast",
            metadata=RequestMetadata(trace_id="net-guard-local"),
        )

        with pytest.raises(Exception) as caught:
            await provider.complete(request)

        # Searched through the cause chain and any ExceptionGroup, for
        # the same reason as the sibling test. The old assertion was
        # `not isinstance(caught.value, ExternalNetworkCallError)` —
        # which could NEVER fail: the adapter always wraps, so the
        # outermost exception is a ProviderError whatever happened
        # underneath. It would have stayed green with the guard
        # over-reaching onto loopback, which is the one thing it exists
        # to detect.
        seen: list[str] = []
        assert not _caused_by_the_guard(caught.value, seen), (
            f"the guard blocked a LOOPBACK connection ({seen}). It must not: "
            f"test_a_dead_primary_is_served_by_the_fallback needs a real "
            f"ConnectionRefusedError from a closed local port."
        )
