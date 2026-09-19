"""Vectors, and the two ways a batch can silently go wrong."""

from __future__ import annotations

import httpx
import pytest

from app.providers import ProviderError
from app.providers.ollama_embeddings import OllamaEmbeddingProvider


def build(handler: httpx.MockTransport, **kwargs: object) -> OllamaEmbeddingProvider:
    return OllamaEmbeddingProvider(
        client=httpx.AsyncClient(transport=handler),
        **kwargs,  # type: ignore[arg-type]
    )


def vectors(count: int, width: int = 768) -> httpx.MockTransport:
    body = {"embeddings": [[0.1] * width for _ in range(count)]}
    return httpx.MockTransport(lambda _r: httpx.Response(200, json=body))


async def test_a_batch_returns_one_vector_per_text() -> None:
    provider = build(vectors(3))

    result = await provider.embed(["a", "b", "c"])

    assert len(result) == 3
    assert all(len(v) == 768 for v in result)


async def test_the_whole_batch_goes_in_one_request() -> None:
    """Not a loop of single-text calls.

    Every provider rate-limits per request. The difference between one
    round trip and five hundred is the difference between a usable ingest
    and an overnight one.
    """
    calls: list[int] = []

    def count(request: httpx.Request) -> httpx.Response:
        import json

        calls.append(len(json.loads(request.content)["input"]))
        return httpx.Response(200, json={"embeddings": [[0.1] * 768] * 5})

    provider = build(httpx.MockTransport(count))
    await provider.embed(["a", "b", "c", "d", "e"])

    assert calls == [5], "the batch was split — one request per text is a rate-limit bill"


async def test_a_wrong_width_vector_is_refused() -> None:
    """The failure this class exists to prevent.

    A 1024-wide vector in a 768 column either errors on insert — fine —
    or lands in a column that accepts it, at which point every similarity
    score in the product is meaningless and nothing anywhere reports a
    problem.
    """
    provider = build(vectors(1, width=1024))

    with pytest.raises(ProviderError, match="1024-dimension"):
        await provider.embed(["a"])


async def test_a_short_batch_is_refused() -> None:
    """Two vectors for three texts misaligns everything after the gap.

    Silently zipping a short response against the inputs pairs text 3
    with vector 2 and so on, which produces a retrieval index that is
    subtly and permanently wrong.
    """
    provider = build(vectors(2))

    with pytest.raises(ProviderError, match="vectors for 3 inputs"):
        await provider.embed(["a", "b", "c"])


async def test_an_empty_batch_makes_no_request() -> None:
    def explode(_request: httpx.Request) -> httpx.Response:
        raise AssertionError("a request was sent for an empty batch")

    assert await build(httpx.MockTransport(explode)).embed([]) == []


async def test_a_dead_server_is_retryable() -> None:
    def refuse(_request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("down")

    with pytest.raises(ProviderError) as caught:
        await build(httpx.MockTransport(refuse)).embed(["a"])

    assert caught.value.retryable is True


async def test_a_missing_model_is_not_retryable() -> None:
    """404 means the model was never pulled. Retrying cannot pull it."""
    provider = build(httpx.MockTransport(lambda _r: httpx.Response(404, json={})))

    with pytest.raises(ProviderError) as caught:
        await provider.embed(["a"])

    assert caught.value.retryable is False


async def test_health_check_does_not_raise() -> None:
    def refuse(_request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("down")

    assert await build(httpx.MockTransport(refuse)).health_check() is False
