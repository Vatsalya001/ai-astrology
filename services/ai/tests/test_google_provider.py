"""Gemini, offline.

Note `httpx` here and `httpx2` in the Anthropic tests — google-genai has
not moved yet, and the two live side by side in the lockfile. Using the
wrong one fails deep inside the SDK's request builder with an error that
names neither library.
"""

from __future__ import annotations

import json
from typing import Any

import httpx
import pytest

from app.providers import (
    CompletionRequest,
    GoogleEmbeddingProvider,
    GoogleProvider,
    Message,
    ModelMap,
    ProviderError,
    RequestMetadata,
    SystemBlock,
)

MODELS = ModelMap(fast="gemini-2.0-flash", chat="gemini-2.5-flash", deep="gemini-2.5-pro")


def build(handler: httpx.MockTransport, **kwargs: object) -> GoogleProvider:
    provider = GoogleProvider(api_key="test-key", models=MODELS, **kwargs)  # type: ignore[arg-type]
    provider._client._api_client._async_httpx_client = httpx.AsyncClient(transport=handler)
    return provider


def a_request(**kwargs: object) -> CompletionRequest:
    return CompletionRequest(
        messages=[Message(role="user", content="hello")],
        tier="chat",
        metadata=RequestMetadata(trace_id="t-1"),
        **kwargs,  # type: ignore[arg-type]
    )


def response(
    *,
    text: str = "Mars.",
    finish: str | None = "STOP",
    usage: dict[str, int] | None = None,
    candidates: list[dict[str, Any]] | None = None,
    **extra: Any,
) -> dict[str, Any]:
    if candidates is None:
        candidate: dict[str, Any] = {"content": {"role": "model", "parts": [{"text": text}]}}
        if finish is not None:
            candidate["finishReason"] = finish
        candidates = [candidate]

    body: dict[str, Any] = {
        "candidates": candidates,
        "usageMetadata": usage or {"promptTokenCount": 10, "candidatesTokenCount": 3},
        "modelVersion": "gemini-2.5-flash",
    }
    body.update(extra)
    return body


def serving(body: dict[str, Any], status: int = 200) -> httpx.MockTransport:
    return httpx.MockTransport(lambda _req: httpx.Response(status, json=body))


def capturing(seen: dict[str, Any], body: dict[str, Any] | None = None) -> httpx.MockTransport:
    def handler(request: httpx.Request) -> httpx.Response:
        seen.update(json.loads(request.content))
        seen["_url"] = str(request.url)
        return httpx.Response(200, json=body or response())

    return httpx.MockTransport(handler)


# ─── the vocabulary differences that bite ────────────────────────────


class TestRequestAssembly:
    async def test_assistant_becomes_model(self) -> None:
        """Google's only two roles are `user` and `model`.

        Sending `assistant` is a 400 on every multi-turn request — so it
        works perfectly in a smoke test and fails on the second message
        of a real conversation.
        """
        seen: dict[str, Any] = {}
        provider = build(capturing(seen))

        await provider.complete(
            CompletionRequest(
                messages=[
                    Message(role="user", content="hi"),
                    Message(role="assistant", content="hello"),
                    Message(role="user", content="and?"),
                ],
                tier="chat",
                metadata=RequestMetadata(trace_id="t"),
            )
        )

        assert [c["role"] for c in seen["contents"]] == ["user", "model", "user"]

    async def test_system_blocks_go_to_system_instruction_in_order(self) -> None:
        # There is one slot and no per-block cache control here, so order
        # is the only thing that makes a prefix stable for Gemini's
        # implicit caching.
        seen: dict[str, Any] = {}
        provider = build(capturing(seen))

        await provider.complete(
            a_request(system=[SystemBlock(content="rules"), SystemBlock(content="persona")])
        )

        assert seen["systemInstruction"]["parts"][0]["text"] == "rules\n\npersona"
        assert not any(c["role"] == "system" for c in seen["contents"])

    async def test_the_tier_selects_the_model(self) -> None:
        seen: dict[str, Any] = {}
        provider = build(capturing(seen))

        await provider.complete(
            CompletionRequest(
                messages=[Message(role="user", content="x")],
                tier="fast",
                metadata=RequestMetadata(trace_id="t"),
            )
        )

        assert "gemini-2.0-flash" in seen["_url"]

    async def test_a_json_schema_is_sent_as_a_constraint(self) -> None:
        seen: dict[str, Any] = {}
        provider = build(capturing(seen))

        await provider.complete(a_request(json_schema={"type": "object"}))

        assert seen["generationConfig"]["responseMimeType"] == "application/json"
        assert seen["generationConfig"]["responseJsonSchema"] == {"type": "object"}


# ─── the safety signal, which is why this is not the OpenAI adapter ──


class TestSafetySignals:
    async def test_a_blocked_prompt_is_a_refusal_not_an_empty_answer(self) -> None:
        """HTTP 200, no candidates, reason on `prompt_feedback`.

        Reading only the candidate gives `stop` with empty text, which is
        indistinguishable from a model that had nothing to say — and
        would put an empty bubble in front of a user instead of the
        safety response the product has written for exactly this.
        """
        provider = build(
            serving(
                {
                    "candidates": [],
                    "promptFeedback": {"blockReason": "SAFETY"},
                    "usageMetadata": {"promptTokenCount": 10},
                }
            )
        )

        assert (await provider.complete(a_request())).finish_reason == "refusal"

    async def test_no_candidates_at_all_is_a_refusal(self) -> None:
        # Google omits `promptFeedback` on some block paths. An empty
        # candidate list is the only signal left, and treating it as a
        # normal stop is the same silent-empty-bubble failure.
        provider = build(serving({"candidates": [], "usageMetadata": {"promptTokenCount": 10}}))

        assert (await provider.complete(a_request())).finish_reason == "refusal"

    @pytest.mark.parametrize(
        "reason", ["SAFETY", "PROHIBITED_CONTENT", "BLOCKLIST", "SPII", "RECITATION"]
    )
    async def test_safety_finish_reasons_are_refusals(self, reason: str) -> None:
        """Refusal, not error — retrying produces the same verdict.

        RECITATION is the non-obvious one: the model was reproducing
        training data. It is not a fault, it will not recover on retry,
        and the product has a graceful answer for it.
        """
        provider = build(serving(response(finish=reason)))

        assert (await provider.complete(a_request())).finish_reason == "refusal"

    async def test_a_malformed_tool_call_is_an_error(self) -> None:
        """This service sends no tools, so one of these is a bug.

        Rendering it as `stop` would quietly show a user whatever partial
        text came back from a request that was not the one we built.
        """
        provider = build(serving(response(finish="MALFORMED_FUNCTION_CALL")))

        assert (await provider.complete(a_request())).finish_reason == "error"

    async def test_max_tokens_becomes_length(self) -> None:
        provider = build(serving(response(finish="MAX_TOKENS")))

        assert (await provider.complete(a_request())).finish_reason == "length"

    async def test_text_is_read_without_the_sdk_text_property(self) -> None:
        """`.text` raises when there are no candidates.

        That is exactly the safety-block shape, so the convenient
        accessor fails on the one path that most needs to return
        something rather than throw.
        """
        provider = build(serving({"candidates": [], "promptFeedback": {"blockReason": "SAFETY"}}))

        assert (await provider.complete(a_request())).text == ""


# ─── usage: the opposite convention from Anthropic ───────────────────


class TestUsage:
    async def test_cached_tokens_are_subtracted_from_the_prompt_count(self) -> None:
        """Google reports `promptTokenCount` INCLUSIVE of cached tokens.

        Anthropic reports them disjointly. Copying the field straight
        across counts the cached prefix twice — once as fresh input at
        full price and once as a cache read — which is the most expensive
        direction to be wrong in, on the largest part of the prompt.
        """
        provider = build(
            serving(
                response(
                    usage={
                        "promptTokenCount": 1000,
                        "candidatesTokenCount": 50,
                        "cachedContentTokenCount": 900,
                    }
                )
            )
        )

        usage = (await provider.complete(a_request())).usage

        assert usage.input_tokens == 100
        assert usage.cached_input_tokens == 900
        assert usage.input_tokens + usage.cached_input_tokens == 1000

    async def test_a_cached_count_exceeding_the_prompt_count_clamps_to_zero(self) -> None:
        # Never observed, but `Usage` declares `ge=0` and a negative here
        # would raise a validation error three layers from the cause.
        provider = build(
            serving(response(usage={"promptTokenCount": 10, "cachedContentTokenCount": 40}))
        )

        assert (await provider.complete(a_request())).usage.input_tokens == 0

    async def test_no_cache_means_all_input_is_fresh(self) -> None:
        provider = build(
            serving(response(usage={"promptTokenCount": 77, "candidatesTokenCount": 5}))
        )

        usage = (await provider.complete(a_request())).usage

        assert usage.input_tokens == 77
        assert usage.cached_input_tokens == 0


# ─── streaming ───────────────────────────────────────────────────────


def sse(chunks: list[dict[str, Any]]) -> httpx.MockTransport:
    body = "".join(f"data: {json.dumps(chunk)}\r\n\r\n" for chunk in chunks)
    return httpx.MockTransport(
        lambda _req: httpx.Response(200, text=body, headers={"content-type": "text/event-stream"})
    )


class TestStreaming:
    async def test_cumulative_usage_is_not_summed(self) -> None:
        """Google repeats running totals on every chunk, not deltas.

        Adding them up multiplies the recorded cost by the number of
        chunks — so a long answer, which is the expensive kind, is
        overstated the most.
        """
        provider = build(
            sse(
                [
                    response(text="Mars ", finish=None, usage={"promptTokenCount": 100}),
                    response(
                        text="rules.",
                        usage={"promptTokenCount": 100, "candidatesTokenCount": 40},
                    ),
                ]
            )
        )

        chunks = [chunk async for chunk in provider.stream(a_request())]

        assert "".join(c.text for c in chunks) == "Mars rules."
        final = chunks[-1]
        assert final.usage is not None
        assert final.usage.input_tokens == 100
        assert final.usage.output_tokens == 40

    async def test_the_finish_reason_arrives_on_the_final_chunk(self) -> None:
        provider = build(
            sse([response(text="x", finish=None), response(text="y", finish="SAFETY")])
        )

        chunks = [chunk async for chunk in provider.stream(a_request())]

        assert chunks[-1].finish_reason == "refusal"


# ─── error classification ────────────────────────────────────────────


class TestRetryClassification:
    @pytest.mark.parametrize("status", [429, 500, 503])
    async def test_transient_statuses_are_retryable(self, status: int) -> None:
        provider = build(serving({"error": {"code": status, "message": "x"}}, status=status))

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        assert caught.value.retryable is True

    @pytest.mark.parametrize("status", [400, 401, 403, 404])
    async def test_client_errors_are_not_retryable(self, status: int) -> None:
        provider = build(serving({"error": {"code": status, "message": "x"}}, status=status))

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        assert caught.value.retryable is False

    async def test_the_response_body_never_reaches_the_error_message(self) -> None:
        """`str(genai.errors.APIError)` interpolates the whole payload.

        On a 400 that payload echoes the request, and on an auth failure
        it can name the credential. `.claude/rules/security.md` is
        unambiguous: a key never reaches an error message. Building the
        message from the status code alone makes that true by
        construction rather than by review.
        """
        provider = build(
            serving(
                {"error": {"code": 400, "message": "API key AIzaSyLEAKED is invalid"}},
                status=400,
            )
        )

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        assert "AIzaSyLEAKED" not in str(caught.value)

    async def test_an_unreachable_backend_is_retryable(self) -> None:
        def refuse(_req: httpx.Request) -> httpx.Response:
            raise httpx.ConnectError("down")

        provider = build(httpx.MockTransport(refuse))

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        assert caught.value.retryable is True


# ─── embeddings ──────────────────────────────────────────────────────


def embeddings(handler: httpx.MockTransport, **kwargs: object) -> GoogleEmbeddingProvider:
    provider = GoogleEmbeddingProvider(api_key="k", **kwargs)  # type: ignore[arg-type]
    provider._client._api_client._async_httpx_client = httpx.AsyncClient(transport=handler)
    return provider


def vectors(widths: list[int]) -> httpx.MockTransport:
    body = {"embeddings": [{"values": [0.1] * width} for width in widths]}
    return httpx.MockTransport(lambda _req: httpx.Response(200, json=body))


class TestEmbeddings:
    async def test_a_batch_returns_one_vector_per_text(self) -> None:
        provider = embeddings(vectors([768, 768]))

        result = await provider.embed(["a", "b"])

        assert len(result) == 2
        assert all(len(v) == 768 for v in result)

    async def test_a_wrong_width_is_refused(self) -> None:
        """A pgvector column is fixed-width.

        A wrong-width vector either fails on insert or lands in a column
        that accepts it and makes every similarity score meaningless —
        and the second failure mode is silent, which is why this is an
        exception rather than a log line.
        """
        provider = embeddings(vectors([768, 1024]))

        with pytest.raises(ProviderError, match="1024-dim"):
            await provider.embed(["a", "b"])

    async def test_a_short_batch_is_refused(self) -> None:
        """Silent truncation misaligns every vector after the gap.

        The corpus then looks entirely plausible and answers with the
        wrong documents, which is far worse than an error.
        """
        provider = embeddings(vectors([768]))

        with pytest.raises(ProviderError, match="1 vectors for 2 texts"):
            await provider.embed(["a", "b"])

    async def test_an_empty_batch_makes_no_request(self) -> None:
        def explode(_req: httpx.Request) -> httpx.Response:
            raise AssertionError("an empty batch billed a round trip")

        assert await embeddings(httpx.MockTransport(explode)).embed([]) == []

    async def test_the_requested_width_is_sent(self) -> None:
        """Gemini's embedding model is Matryoshka — one model, several widths.

        Leaving it to the default means the width changes when the
        default does, and `EMBEDDING_DIM` stops matching the column.
        """
        seen: dict[str, Any] = {}

        def capture(request: httpx.Request) -> httpx.Response:
            seen.update(json.loads(request.content))
            return httpx.Response(200, json={"embeddings": [{"values": [0.1] * 512}]})

        provider = embeddings(httpx.MockTransport(capture), dimensions=512)
        await provider.embed(["a"])

        assert seen["requests"][0]["outputDimensionality"] == 512
