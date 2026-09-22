"""Which provider answers, and what happens when it cannot.

── Why a registry rather than a module-level singleton ──

Fallback. A singleton can only be the one provider that was configured,
and §2 requires a chain: try the primary, and on a *retryable* failure
move to the next. That needs an ordered collection and somewhere to hold
the decision about which failures justify moving on.

── Why the PII guard runs here ──

`app/guards.py` owns the rule; this owns the moment. Every provider is
checked as it is registered, so a misconfiguration is a startup crash
with a named provider rather than a request that quietly leaves birth
data at a free tier. Checking at call time would be too late: the process
would already be serving.
"""

from __future__ import annotations

from collections.abc import AsyncIterator

from app.guards import assert_provider_allowed
from app.providers.base import (
    CompletionChunk,
    CompletionRequest,
    CompletionResponse,
    EmbeddingProvider,
    LLMProvider,
    ProviderError,
)


class NoProviderAvailableError(ProviderError):
    """Every provider in the chain failed, or none was registered.

    Carries the per-provider failures rather than only the last one: with
    a fallback chain, the last error is usually the least informative —
    the primary's timeout is the thing worth reading, not the backup's
    "model not found".

    ── Why it is a ProviderError and not a bare RuntimeError ──

    It used to be a sibling of `ProviderError`, and every caller that
    degrades gracefully catches `ProviderError`:

        classification/classifier.py:200
            "Every provider in the chain has already been tried by the
             time this raises."
        safety/classifier.py:166
            "Fail open ... the keyword pass has already run."

    Both comments describe exactly this exception, and neither caught
    it. So with Ollama unreachable, a completion did not degrade — the
    screener raised, nothing handled it, and `POST /v1/complete`
    returned an unhandled 500 with a traceback, instead of the clean
    5xx→503 the spec asks for. Reproduced against the running stack.

    `retryable=False`: by the time this is raised every provider in the
    chain has already been tried and has already failed. Retrying the
    chain that just exhausted itself turns one outage into several.
    """

    def __init__(self, failures: dict[str, str]) -> None:
        self.failures = failures
        message = (
            "no LLM provider is registered"
            if not failures
            else "every provider failed — "
            + "; ".join(f"{name}: {why}" for name, why in failures.items())
        )
        super().__init__(
            message,
            # The whole chain, so a log line names who was tried rather
            # than inventing a single provider that did not exist.
            provider_id=",".join(failures) or "none",
            retryable=False,
        )


class ProviderRegistry:
    """An ordered fallback chain of LLM providers.

    Order is registration order, and it is the whole configuration: the
    first registered is the primary. Nothing re-ranks by latency or cost
    at runtime, deliberately — a chain that reorders itself is a chain
    whose behaviour cannot be reproduced from the config file when
    somebody asks why a request went somewhere unexpected.
    """

    def __init__(self, env: str) -> None:
        self._env = env
        self._providers: list[LLMProvider] = []
        self._embeddings: dict[str, EmbeddingProvider] = {}

    # ─── registration ────────────────────────────────────────────────

    def register(self, provider: LLMProvider) -> None:
        """Add a provider to the end of the chain.

        The guard runs BEFORE the provider is stored. A provider that
        fails it must not end up in the chain even if the caller catches
        the exception — half-registered state is how a "blocked" provider
        ends up serving.
        """
        assert_provider_allowed(provider.id, provider.tier, self._env)

        if any(existing.id == provider.id for existing in self._providers):
            raise ValueError(
                f"provider {provider.id!r} is registered twice — the chain would "
                f"retry the same failing endpoint and call it a fallback"
            )

        self._providers.append(provider)

    def register_embedding(self, provider: EmbeddingProvider) -> None:
        assert_provider_allowed(provider.id, provider.tier, self._env)
        self._embeddings[provider.id] = provider

    # ─── access ──────────────────────────────────────────────────────

    @property
    def primary(self) -> LLMProvider:
        if not self._providers:
            raise NoProviderAvailableError({})
        return self._providers[0]

    @property
    def chain(self) -> tuple[LLMProvider, ...]:
        return tuple(self._providers)

    def embedding(self, provider_id: str) -> EmbeddingProvider:
        try:
            return self._embeddings[provider_id]
        except KeyError:
            raise NoProviderAvailableError(
                {provider_id: "no embedding provider registered under this id"}
            ) from None

    # ─── the fallback chain ──────────────────────────────────────────

    async def complete(self, req: CompletionRequest) -> CompletionResponse:
        """Try each provider in order until one answers.

        Only a RETRYABLE failure moves to the next provider. A 400 from a
        malformed request will fail identically everywhere, and walking a
        three-provider chain with it turns one bad request into three —
        three times the latency, and a log that blames the last provider
        for the first one's mistake.
        """
        failures: dict[str, str] = {}

        for provider in self._providers:
            try:
                return await provider.complete(req)
            except ProviderError as err:
                failures[provider.id] = str(err)
                if not err.retryable:
                    raise
            except Exception as err:
                # A vendor SDK can raise anything. An adapter SHOULD
                # translate to ProviderError, and this catch exists for
                # the day one does not: an unclassified error is treated
                # as retryable, because the alternative is that a single
                # unmapped exception type takes down every request
                # instead of failing over.
                # The type, never the exception. This map is rendered
                # into NoProviderAvailableError, so it is the catch-all
                # path by which an SDK body — request echo and all —
                # could reach a log even with every adapter clean.
                failures[provider.id] = type(err).__name__

        raise NoProviderAvailableError(failures)

    def stream(self, req: CompletionRequest) -> AsyncIterator[CompletionChunk]:
        """Stream from the primary only.

        No fallback, and that is a decision rather than an omission: once
        the first chunk has reached the user, switching providers
        mid-response would splice two different models' prose together.
        A stream that fails before its first chunk is the caller's to
        retry, which it can do against the chain via `complete`.
        """
        return self.primary.stream(req)

    async def health(self) -> dict[str, bool]:
        """Every provider's liveness, for the health endpoint.

        Reported per provider rather than reduced to one boolean: with a
        fallback chain, "the primary is down but the backup is up" is a
        materially different situation from "everything is down", and a
        single flag cannot say which one is happening.
        """
        return {p.id: await p.health_check() for p in self._providers}
