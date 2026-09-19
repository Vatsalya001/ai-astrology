"""The workhorse adapter, driven against a fake transport.

── Why httpx mocking rather than mocking the SDK ──

Patching `AsyncOpenAI.chat.completions.create` would test that the
adapter calls a method — which it obviously does — and would keep
passing if the SDK changed the shape it returns. Serving real HTTP
responses through the SDK's own transport exercises the SDK's parsing,
which is where the version-skew bugs actually live.
"""

from __future__ import annotations

import httpx
import pytest

from app.providers import (
    Capabilities,
    CompletionRequest,
    Message,
    ModelMap,
    OpenAICompatibleProvider,
    ProviderError,
    RequestMetadata,
    SystemBlock,
)

MODELS = ModelMap(fast="llama3.2:3b", chat="qwen2.5:7b", deep="qwen2.5:7b")


def build(handler: httpx.MockTransport, **kwargs: object) -> OpenAICompatibleProvider:
    provider = OpenAICompatibleProvider(
        base_url="http://localhost:11434/v1",
        api_key="",
        tier="local",
        models=MODELS,
        provider_id="ollama",
        **kwargs,  # type: ignore[arg-type]
    )
    # Swap the transport under the SDK's own client, so everything above
    # the socket — auth headers, retries, parsing — is the real thing.
    provider._client._client = httpx.AsyncClient(transport=handler)
    return provider


def a_request(**kwargs: object) -> CompletionRequest:
    return CompletionRequest(
        messages=[Message(role="user", content="hello")],
        tier="chat",
        metadata=RequestMetadata(trace_id="t-1"),
        **kwargs,  # type: ignore[arg-type]
    )


def completion(text: str = "hi", *, finish: str = "stop", model: str = "qwen2.5:7b") -> dict:
    return {
        "id": "chatcmpl-1",
        "object": "chat.completion",
        "created": 1,
        "model": model,
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": text},
                "finish_reason": finish,
            }
        ],
        "usage": {"prompt_tokens": 11, "completion_tokens": 7, "total_tokens": 18},
    }


def responding(status: int, body: dict | None = None) -> httpx.MockTransport:
    return httpx.MockTransport(
        lambda _req: httpx.Response(status, json=body or {"error": {"message": "nope"}})
    )


# ─── error classification ────────────────────────────────────────────


class TestRetryClassification:
    """The most consequential mapping in the adapter.

    `retryable` decides whether the registry walks to the next provider,
    and it is expensive in both directions: a retryable error marked
    permanent turns a blip into an outage, and a permanent one marked
    retryable sends the same malformed request to every provider in the
    chain.
    """

    @pytest.mark.parametrize("status", [408, 429, 500, 502, 503, 504])
    async def test_transient_statuses_are_retryable(self, status: int) -> None:
        provider = build(responding(status))

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        assert caught.value.retryable is True, (
            f"HTTP {status} was marked permanent — the chain will not fail over, "
            f"so a rate limit or a restart becomes a user-visible failure"
        )

    @pytest.mark.parametrize("status", [400, 401, 403, 404, 422])
    async def test_client_errors_are_not_retryable(self, status: int) -> None:
        provider = build(responding(status))

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        assert caught.value.retryable is False, (
            f"HTTP {status} was marked retryable — the same request that just "
            f"failed will now be sent to every provider in the chain, and the "
            f"log will blame the last one for the first one's mistake"
        )

    async def test_a_dead_backend_is_retryable(self) -> None:
        """Ollama not running is the everyday case, and it must fail over.

        Nothing about the request is wrong, so the next provider is very
        likely to answer. This is precisely what the chain exists for.
        """

        def refuse(_req: httpx.Request) -> httpx.Response:
            raise httpx.ConnectError("connection refused")

        provider = build(httpx.MockTransport(refuse))

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        assert caught.value.retryable is True
        assert "unreachable" in str(caught.value)

    async def test_the_status_code_is_carried(self) -> None:
        # So a caller — or a log — can distinguish 429 from 503 without
        # parsing the message text.
        provider = build(responding(429))

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        assert caught.value.status_code == 429


# ─── translation ─────────────────────────────────────────────────────


class TestTranslation:
    async def test_a_normal_completion_is_translated(self) -> None:
        provider = build(
            httpx.MockTransport(lambda _r: httpx.Response(200, json=completion("Saturn.")))
        )

        response = await provider.complete(a_request())

        assert response.text == "Saturn."
        assert response.finish_reason == "stop"
        assert response.usage.input_tokens == 11
        assert response.usage.output_tokens == 7
        assert response.provider_id == "ollama"

    async def test_content_filter_becomes_refusal_not_error(self) -> None:
        """A model declining is a product outcome, not a retry path.

        Mapping it to `error` would make the chain retry a refusal at
        every provider and then show a failure to a user whose question
        the product has a written answer for.
        """
        provider = build(
            httpx.MockTransport(
                lambda _r: httpx.Response(200, json=completion(finish="content_filter"))
            )
        )

        assert (await provider.complete(a_request())).finish_reason == "refusal"

    async def test_length_is_preserved(self) -> None:
        provider = build(
            httpx.MockTransport(lambda _r: httpx.Response(200, json=completion(finish="length")))
        )

        assert (await provider.complete(a_request())).finish_reason == "length"

    async def test_the_model_the_backend_reports_is_recorded(self) -> None:
        """Not the model we asked for.

        OpenRouter silently routes to whatever is cheapest, so the two
        differ — and a log recording the requested model cannot explain
        the answer that came back.
        """
        provider = build(
            httpx.MockTransport(
                lambda _r: httpx.Response(200, json=completion(model="something-else"))
            )
        )

        assert (await provider.complete(a_request())).model == "something-else"

    async def test_missing_usage_becomes_zeros_not_an_error(self) -> None:
        """Some Ollama versions omit it entirely.

        Zeros are correct — nothing was billed — and safe to sum, which a
        `None` would not be.
        """
        body = completion()
        del body["usage"]
        provider = build(httpx.MockTransport(lambda _r: httpx.Response(200, json=body)))

        usage = (await provider.complete(a_request())).usage
        assert usage.input_tokens == 0 and usage.cost_micros == 0


# ─── request assembly ────────────────────────────────────────────────


class TestRequestAssembly:
    async def test_system_blocks_are_sent_in_order_as_one_message(self) -> None:
        """Order is the only thing this wire format preserves.

        There is no per-block cache control here, so a stable prefix is
        purely a matter of the blocks arriving in the same sequence every
        time. The Anthropic adapter, which does have breakpoints, keeps
        them separate.
        """
        seen: dict[str, object] = {}

        def capture(request: httpx.Request) -> httpx.Response:
            import json

            seen.update(json.loads(request.content))
            return httpx.Response(200, json=completion())

        provider = build(httpx.MockTransport(capture))
        await provider.complete(
            a_request(
                system=[
                    SystemBlock(content="rules", name="safety"),
                    SystemBlock(content="persona", name="voice"),
                ]
            )
        )

        messages = seen["messages"]
        assert isinstance(messages, list)
        assert messages[0]["role"] == "system"
        assert messages[0]["content"] == "rules\n\npersona"

    async def test_the_tier_selects_the_model(self) -> None:
        seen: dict[str, object] = {}

        def capture(request: httpx.Request) -> httpx.Response:
            import json

            seen.update(json.loads(request.content))
            return httpx.Response(200, json=completion())

        provider = build(httpx.MockTransport(capture))
        await provider.complete(
            CompletionRequest(
                messages=[Message(role="user", content="x")],
                tier="fast",
                metadata=RequestMetadata(trace_id="t"),
            )
        )

        assert seen["model"] == "llama3.2:3b"

    async def test_json_is_refused_at_the_edge_when_unsupported(self) -> None:
        """Rather than sent and hoped for.

        A backend without JSON mode returns prose, which fails to parse
        three layers away from the cause — in a parser that has no idea
        the provider was never capable.
        """
        provider = build(
            httpx.MockTransport(lambda _r: httpx.Response(200, json=completion())),
            capabilities=Capabilities(json_mode=False),
        )

        with pytest.raises(ProviderError, match="structured output"):
            await provider.complete(a_request(json_schema={"type": "object"}))

    async def test_refusing_json_is_not_retryable(self) -> None:
        # Failing over would ask the next provider the same impossible
        # question.
        provider = build(
            httpx.MockTransport(lambda _r: httpx.Response(200, json=completion())),
            capabilities=Capabilities(json_mode=False),
        )

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request(json_schema={"type": "object"}))

        assert caught.value.retryable is False


async def test_health_check_returns_false_rather_than_raising() -> None:
    """A probe that raises is a probe nobody calls.

    The registry reports health for every provider at once; one dead
    backend must not take the whole health endpoint down with it.
    """

    def refuse(_req: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("down")

    assert await build(httpx.MockTransport(refuse)).health_check() is False


async def test_health_check_returns_true_when_the_backend_answers() -> None:
    # The negative case for the check itself: one that always returns
    # False is not a health check, it is a permanent outage.
    provider = build(
        httpx.MockTransport(lambda _r: httpx.Response(200, json={"object": "list", "data": []}))
    )
    assert await provider.health_check() is True
