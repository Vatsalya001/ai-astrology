"""The workhorse adapter, driven against a fake transport.

── Why httpx mocking rather than mocking the SDK ──

Patching `AsyncOpenAI.chat.completions.create` would test that the
adapter calls a method — which it obviously does — and would keep
passing if the SDK changed the shape it returns. Serving real HTTP
responses through the SDK's own transport exercises the SDK's parsing,
which is where the version-skew bugs actually live.
"""

from __future__ import annotations

import json
import struct
from typing import ClassVar

import httpx
import openai
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
from app.providers.openai_compatible import _classify
from app.providers.resilience import UnsafeToReplayError

MODELS = ModelMap(fast="llama3.2:3b", chat="qwen2.5:7b", deep="qwen2.5:7b")

# A key-shaped string for the fixtures that must not reach an error
# message. `str(openai.APIStatusError)` is "Error code: 401 - {body}" —
# the whole response body — and an auth failure is exactly where a
# provider echoes the credential it rejected.
LEAKED = "sk-ant-api03-" + "Z" * 24


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


def raising(error: Exception) -> httpx.MockTransport:
    def handler(_req: httpx.Request) -> httpx.Response:
        raise error

    return httpx.MockTransport(handler)


def chunk(**overrides: object) -> dict:
    body: dict = {
        "id": "1",
        "object": "chat.completion.chunk",
        "created": 1,
        "model": "qwen2.5:7b",
        "choices": [],
    }
    body.update(overrides)
    return body


def sse(events: list[dict], *, capture: dict | None = None) -> httpx.MockTransport:
    text = "".join(f"data: {json.dumps(e)}\n\n" for e in events) + "data: [DONE]\n\n"

    def handler(request: httpx.Request) -> httpx.Response:
        if capture is not None:
            capture.update(json.loads(request.content))
        return httpx.Response(200, text=text, headers={"content-type": "text/event-stream"})

    return httpx.MockTransport(handler)


def words(*pieces: str, billed: tuple[int, int] | None = None) -> list[dict]:
    """Content frames, a finish frame, and optionally the usage frame.

    `billed=None` models a backend that ignored `stream_options` — some
    Ollama and LM Studio builds do — which is the case the adapter has to
    turn into zeros rather than into nothing.

    The usage frame carries an EMPTY `choices` list, and that is the
    whole difficulty: an adapter that skips chunks without choices skips
    the only billing record a streamed call ever produces.
    """
    frames = [
        chunk(choices=[{"index": 0, "delta": {"content": p}, "finish_reason": None}])
        for p in pieces
    ]
    frames.append(chunk(choices=[{"index": 0, "delta": {}, "finish_reason": "stop"}]))

    if billed is not None:
        prompt, completion = billed
        frames.append(
            chunk(
                choices=[],
                usage={
                    "prompt_tokens": prompt,
                    "completion_tokens": completion,
                    "total_tokens": prompt + completion,
                },
            )
        )

    return frames


# ─── error classification ────────────────────────────────────────────


class TestRetryClassification:
    """The most consequential mapping in the adapter.

    `retryable` decides whether the registry walks to the next provider,
    and it is expensive in both directions: a retryable error marked
    permanent turns a blip into an outage, and a permanent one marked
    retryable sends the same malformed request to every provider in the
    chain.
    """

    @pytest.mark.parametrize(
        "status",
        # 501, 520, 522, 524 and 529 are the additions, and they are the
        # ones that were wrong. This adapter enumerated 5xx code by code
        # — {500, 502, 503, 504} — so a 529 from an overloaded Anthropic
        # behind a proxy, or a 520/522/524 from the Cloudflare edge in
        # front of an OpenRouter-style gateway, was marked PERMANENT
        # while Anthropic and Google called the identical code transient.
        # The chain then refused to fail over for an outage that had a
        # healthy provider one position away.
        [408, 409, 425, 429, 500, 501, 502, 503, 504, 520, 522, 524, 529],
    )
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

    @pytest.mark.parametrize("status", [400, 401, 403, 500])
    async def test_the_response_body_never_reaches_the_error_message(self, status: int) -> None:
        """`.claude/rules/security.md`: a key never reaches an error message.

        This SDK builds `str(APIStatusError)` as "Error code: 401 - "
        followed by the ENTIRE decoded body, and this adapter used to
        interpolate that straight into its own message. On an auth
        failure the body is where a gateway echoes the credential it
        rejected; on a 400 it echoes the request, which for this product
        is a user's birth details. Both end up in the registry's failure
        map and from there in a log line.
        """
        provider = build(responding(status, {"error": {"message": f"bad key {LEAKED}"}}))

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        assert LEAKED not in str(caught.value)
        assert "sk-ant-" not in str(caught.value)

    async def test_the_status_is_still_named_in_the_message(self) -> None:
        # The negative case for the redaction above: a message stripped
        # down to nothing is unusable for debugging, and the temptation
        # would be to put the body back.
        provider = build(responding(503))

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        assert "503" in str(caught.value)
        assert "ollama" in str(caught.value)


class TestTimeoutsAreNotAllTheSame:
    """A timeout is a claim about WHERE the request got to.

    `internal/platform/clients/ai_complete.go`: "replaying a completion
    spends money again and may produce a different answer". A read
    timeout may be a completion that ran to its last token and was
    billed in full, with only the response lost — so it must not be sent
    again. A connect timeout never left the process and costs nothing to
    repeat. Collapsing the two makes every lost response a double bill.
    """

    @pytest.mark.parametrize(
        "error",
        [httpx.ConnectTimeout("connect"), httpx.PoolTimeout("pool")],
        ids=["connect", "pool"],
    )
    async def test_a_timeout_before_the_request_was_sent_is_replayable(
        self, error: Exception
    ) -> None:
        provider = build(raising(error))

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        assert not isinstance(caught.value, UnsafeToReplayError), (
            "a connect or pool timeout never reached the provider, so nothing was "
            "generated and nothing was billed — refusing to retry it turns every "
            "cold Ollama into a user-visible failure"
        )
        assert caught.value.retryable is True

    @pytest.mark.parametrize(
        "error",
        [httpx.ReadTimeout("read"), httpx.WriteTimeout("write"), httpx.TimeoutException("?")],
        ids=["read", "write", "unknown"],
    )
    async def test_a_timeout_after_the_request_was_sent_is_not_replayable(
        self, error: Exception
    ) -> None:
        """Including the phase we cannot name — this fails closed.

        The expensive mistake is assuming a call that was billed did not
        happen, so anything that is not provably pre-send is treated as
        post-send.
        """
        provider = build(raising(error))

        with pytest.raises(UnsafeToReplayError) as caught:
            await provider.complete(a_request())

        # Still retryable: that flag is the REGISTRY's, and means "try a
        # different provider", which is one more bill rather than the
        # same one twice. Only the in-place retry is forbidden.
        assert caught.value.retryable is True

    async def test_a_refused_connection_is_still_replayable(self) -> None:
        # The everyday case, and the negative case for the two above: if
        # `UnsafeToReplayError` swallowed ordinary connection failures,
        # a laptop with Ollama switched off would stop retrying anything.
        provider = build(raising(httpx.ConnectError("refused")))

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        assert not isinstance(caught.value, UnsafeToReplayError)
        assert caught.value.retryable is True


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


# ─── streaming, and the cost it used to throw away ───────────────────


class TestStreamingUsage:
    """The only adapter that streamed for free.

    This wire format reports no usage on a stream unless the caller opts
    in, and the adapter did not — so every streamed response landed in
    `ai_request_logs` with nothing at all. Not zero: NOTHING, which is
    indistinguishable from a cheap call in a cost dashboard and so
    invisible until the invoice.
    """

    async def test_the_usage_opt_in_is_sent(self) -> None:
        """`stream_options` is the whole fix on the request side.

        Without it the backend sends no usage frame and there is nothing
        to parse, however careful the parsing is.
        """
        seen: dict = {}
        provider = build(sse(words("a ", "b", billed=(11, 7)), capture=seen))

        [c async for c in provider.stream(a_request())]

        assert seen["stream_options"] == {"include_usage": True}

    async def test_the_opt_in_is_absent_from_a_non_streamed_call(self) -> None:
        """The negative case, and it is a 400 if we get it wrong.

        The OpenAI API rejects `stream_options` on a request that is not
        a stream, so putting it in the shared `_kwargs` would break every
        `complete()` call on a strict backend.
        """
        seen: dict[str, object] = {}

        def capture(request: httpx.Request) -> httpx.Response:
            seen.update(json.loads(request.content))
            return httpx.Response(200, json=completion())

        await build(httpx.MockTransport(capture)).complete(a_request())

        assert "stream_options" not in seen

    async def test_usage_arrives_on_the_final_chunk(self) -> None:
        """Read off the frame with an EMPTY `choices` list.

        That frame is why the bug survived review: the loop opened with
        `if not event.choices: continue`, which reads as defensive and
        skips precisely the one chunk carrying the bill.
        """
        provider = build(sse(words("Saturn ", "waits.", billed=(41, 9))))

        chunks = [c async for c in provider.stream(a_request())]

        assert "".join(c.text for c in chunks) == "Saturn waits."
        assert [i for i, c in enumerate(chunks) if c.usage is not None] == [len(chunks) - 1]
        assert chunks[-1].usage is not None
        assert chunks[-1].usage.input_tokens == 41
        assert chunks[-1].usage.output_tokens == 9

    async def test_a_backend_that_ignores_the_opt_in_still_reports_zeros(self) -> None:
        """Zeros, not `None`. Some Ollama and LM Studio builds drop it.

        A consumer reading cost off the last chunk then gets an integer
        it can sum on every provider. A `None` that only this adapter
        produces is a branch every caller has to remember, and the one
        that forgets crashes on a laptop and not in CI.
        """
        provider = build(sse(words("Saturn ", "waits.")))

        chunks = [c async for c in provider.stream(a_request())]

        assert chunks[-1].usage is not None, "the shape must not depend on the backend"
        assert chunks[-1].usage.input_tokens == 0
        assert chunks[-1].usage.output_tokens == 0
        assert chunks[-1].usage.cost_micros == 0

    async def test_the_finish_reason_lands_with_the_usage(self) -> None:
        # Both on the last chunk, as on every other adapter, so a
        # consumer can close its UI state and record the cost in one
        # place rather than two that differ per provider.
        provider = build(sse(words("Saturn ", "waits.", billed=(11, 7))))

        chunks = [c async for c in provider.stream(a_request())]

        assert [i for i, c in enumerate(chunks) if c.finish_reason] == [len(chunks) - 1]
        assert chunks[-1].finish_reason == "stop"

    async def test_a_refusal_survives_to_the_final_chunk(self) -> None:
        # `content_filter` is a product outcome with a written response.
        # Losing it in the stream would show the user an empty bubble.
        provider = build(
            sse(
                [
                    chunk(choices=[{"index": 0, "delta": {"content": "no"}}]),
                    chunk(choices=[{"index": 0, "delta": {}, "finish_reason": "content_filter"}]),
                    chunk(
                        choices=[],
                        usage={"prompt_tokens": 11, "completion_tokens": 7, "total_tokens": 18},
                    ),
                ]
            )
        )

        chunks = [c async for c in provider.stream(a_request())]

        assert chunks[-1].finish_reason == "refusal"

    async def test_a_mid_stream_failure_is_classified_like_any_other(self) -> None:
        """A stream can die after its first chunk.

        The caller sees one exception type whether the failure happened
        at connect time or halfway through, or it needs two handlers for
        one condition.
        """

        def handler(_req: httpx.Request) -> httpx.Response:
            raise httpx.ReadTimeout("died mid-stream")

        provider = build(httpx.MockTransport(handler))

        with pytest.raises(ProviderError):
            [c async for c in provider.stream(a_request())]


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


class TestStructuredOutputNeedsTheWordJson:
    """A vendor requirement that fails at the vendor, not at us.

    OpenAI-compatible backends reject `response_format=json_object`
    unless the messages themselves mention JSON. Groq is explicit:

        400 'messages' must contain the word 'json' in some form, to
            use 'response_format' of type 'json_object'

    Ollama does not enforce it, so a setup that works locally breaks the
    moment it points at a hosted backend — exactly the dev/prod
    divergence §15 warns about.

    Found by `scripts/verify_provider.py openai-compatible` on its first
    real run against Groq. Every shipped prompt satisfies the rule today
    by luck: `intent_classification.v4` opens its output section with "A
    single JSON object with exactly these keys". Nothing required that,
    and a reword would have broken structured output at runtime with an
    error naming neither the prompt nor the word.
    """

    SCHEMA: ClassVar[dict] = {"type": "object", "properties": {"planet": {"type": "string"}}}

    async def test_a_prompt_without_the_word_is_refused_locally(self) -> None:
        provider = build(responding(200, completion()))
        request = CompletionRequest(
            messages=[Message(role="user", content="Reply with only a planet name.")],
            system=[SystemBlock(content="You label things.")],
            tier="chat",
            json_schema=self.SCHEMA,
            metadata=RequestMetadata(trace_id="t-1"),
        )

        with pytest.raises(ProviderError) as caught:
            await provider.complete(request)

        assert "mentions JSON" in str(caught.value)
        # Not retryable: repeating a deterministic 400 changes nothing
        # and burns the budget that a real outage would need.
        assert caught.value.retryable is False

    @pytest.mark.parametrize(
        ("label", "system", "user"),
        [
            ("in the system prompt", "Answer with a single JSON object.", "hello"),
            ("in the user message", "You label things.", "give me json please"),
            ("mixed case", "Return a Json object.", "hello"),
        ],
    )
    async def test_the_word_anywhere_is_enough(self, label: str, system: str, user: str) -> None:
        """The positive case, without which the test above is satisfied
        by an adapter that refuses every structured request."""
        provider = build(responding(200, completion(text='{"planet": "Mars"}')))
        request = CompletionRequest(
            messages=[Message(role="user", content=user)],
            system=[SystemBlock(content=system)],
            tier="chat",
            json_schema=self.SCHEMA,
            metadata=RequestMetadata(trace_id="t-1"),
        )

        response = await provider.complete(request)

        assert response.text == '{"planet": "Mars"}'

    async def test_a_request_without_a_schema_is_unaffected(self) -> None:
        # The rule only applies when response_format is set. An ordinary
        # chat turn must not start requiring the word "json".
        provider = build(responding(200, completion()))

        response = await provider.complete(a_request())

        assert response.text == "hi"


class TestEveryShippedPromptSatisfiesTheRule:
    """The guard that stops this being reintroduced by a reword.

    The adapter refusal turns a vendor 400 into a clear local error. It
    does not stop somebody shipping a prompt that triggers it — this
    does, at build time rather than on a user's request.
    """

    @pytest.mark.parametrize(
        ("module", "version_setting"),
        [
            ("intent_classification", "prompt_version_intent"),
            ("safety_classification", "prompt_version_safety"),
        ],
    )
    def test_a_structured_output_prompt_mentions_json(
        self, module: str, version_setting: str
    ) -> None:
        from app.prompts import load_module
        from app.settings import settings

        content = load_module(module, getattr(settings, version_setting)).content

        assert "json" in content.lower(), (
            f"{module} is sent with a json_schema, so an OpenAI-compatible backend "
            f"rejects the request unless the prompt mentions JSON. Add the word."
        )


class TestAConnectionLostMidRunIsNotReplayed:
    """The completion the dying provider received three times.

    `_classify` treated every non-timeout `APIConnectionError` as "the
    backend never accepted the request", and said so in a comment. That
    is true for a refused connection and false for a provider killed
    mid-run: the request is read in full, then the connection resets,
    and the bytes are gone whether or not tokens were generated.

    Measured against a real socket before the fix — a server that reads
    the whole request body and then sends RST, with a retry budget of 3:

        [server] got a completion request (670 bytes) -> RST
        [server] got a completion request (670 bytes) -> RST
        [server] got a completion request (543 bytes) -> RST
        COMPLETION REQUESTS THE DYING PROVIDER RECEIVED: 3

    Three bills for one question, where `TestATimeoutIsNotAFreeRetry`
    already establishes that one is too many.

    These use a real TCP server rather than MockTransport, because the
    thing under test is which httpx phase the failure lands in — and a
    mock transport is precisely the layer that decides that. The autouse
    socket guard in conftest.py allows loopback for this reason.
    """

    async def _server(self, *, read_the_request: bool) -> tuple[str, list[int]]:
        """A server that dies, either before or after reading the request.

        Returns its base URL and a list that accumulates the byte count
        of every request it managed to read.
        """
        import asyncio
        import socket as socket_module

        received: list[int] = []

        async def handle(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
            if read_the_request:
                # Read until the body is in hand, so the kill is
                # unambiguously AFTER the provider had the request.
                try:
                    data = await asyncio.wait_for(reader.read(65536), timeout=2)
                    received.append(len(data))
                except (TimeoutError, ConnectionError):
                    pass

            # RST rather than a clean FIN: SO_LINGER with a zero timeout
            # makes close() send a reset, which is what a killed process
            # does and what produces httpx.ReadError rather than a tidy
            # empty response.
            sock = writer.get_extra_info("socket")
            if sock is not None:
                sock.setsockopt(
                    socket_module.SOL_SOCKET,
                    socket_module.SO_LINGER,
                    struct.pack("ii", 1, 0),
                )
            writer.close()

        server = await asyncio.start_server(handle, "127.0.0.1", 0)
        port = server.sockets[0].getsockname()[1]
        self._servers.append(server)
        return f"http://127.0.0.1:{port}/v1", received

    @pytest.fixture(autouse=True)
    def _track_servers(self):  # type: ignore[no-untyped-def]
        self._servers: list = []
        yield
        for server in self._servers:
            server.close()

    def _real(self, base_url: str, **kwargs: object) -> OpenAICompatibleProvider:
        """The adapter with its own SDK client — no transport swapped in."""
        return OpenAICompatibleProvider(
            base_url=base_url,
            api_key="",
            tier="local",
            models=MODELS,
            provider_id="ollama",
            **kwargs,  # type: ignore[arg-type]
        )

    async def test_a_provider_that_dies_after_reading_is_unsafe_to_replay(self) -> None:
        base_url, received = await self._server(read_the_request=True)

        with pytest.raises(UnsafeToReplayError):
            await self._real(base_url).complete(a_request())

        # The setup has to have actually happened: if the server read
        # nothing, this is the never-sent case and proves the opposite.
        assert received, "the server never read the request, so this is not a mid-run kill"

    async def test_a_refused_connection_is_still_an_ordinary_retryable_failure(self) -> None:
        """The control, and the case that must not regress.

        A developer's laptop with Ollama stopped lands here, and it has
        to stay retryable — otherwise the fallback chain stops working
        for the single most common local failure.
        """
        import socket as socket_module

        with socket_module.socket() as probe:
            probe.bind(("127.0.0.1", 0))
            dead_port = probe.getsockname()[1]

        with pytest.raises(ProviderError) as caught:
            await self._real(f"http://127.0.0.1:{dead_port}/v1").complete(a_request())

        assert not isinstance(caught.value, UnsafeToReplayError), (
            "a refused connection was treated as possibly-billed; nothing was sent"
        )
        assert caught.value.retryable is True


class TestThePhaseCheckSeesTheSdksOwnExceptions:
    """Two httpx distributions are installed, and they are not the same.

    `httpx` 0.28.1 is what the adapter imports; `httpx2` is what the
    OpenAI SDK actually raises from. They are separate packages:

        httpx.ConnectError is httpx2.ConnectError   ->  False

    So `isinstance(err.__cause__, httpx.ConnectTimeout | httpx.PoolTimeout)`
    never matched a real failure, every timeout was classified "may have
    been billed", and the connect-timeout-is-retryable branch was dead
    code that a green test claimed to cover.

    The test claimed it because it built the cause by hand from OUR
    httpx — testing a code path that cannot run in production. Matching
    by class name instead makes the check indifferent to which
    distribution the SDK vendors next.
    """

    def _sdk_httpx(self):  # type: ignore[no-untyped-def]
        """The httpx module the installed SDK actually uses."""
        import openai._base_client as base

        for value in vars(base).values():
            if getattr(value, "__name__", "") in {"httpx", "httpx2"}:
                return value
        pytest.skip("could not determine which httpx the SDK uses")

    def test_the_two_distributions_really_are_distinct(self) -> None:
        """If this ever fails, the bug below is gone and so is its risk."""
        sdk_httpx = self._sdk_httpx()
        if sdk_httpx is httpx:
            pytest.skip("only one httpx installed; nothing to confuse")
        assert sdk_httpx.ConnectError is not httpx.ConnectError

    @pytest.mark.parametrize("phase", ["ConnectTimeout", "PoolTimeout"])
    def test_a_pre_send_timeout_from_the_sdks_httpx_is_retryable(self, phase: str) -> None:
        """The branch that was dead.

        Built from the SDK's own module, so it fails if the check ever
        goes back to comparing against the wrong package's classes.
        """
        sdk_httpx = self._sdk_httpx()
        err = openai.APITimeoutError(request=sdk_httpx.Request("POST", "http://x/v1"))
        err.__cause__ = getattr(sdk_httpx, phase)("boom")

        classified = _classify(err, "ollama")

        assert not isinstance(classified, UnsafeToReplayError), (
            f"a {phase} never sent a byte, so nothing could have been billed"
        )
        assert classified.retryable is True

    def test_a_read_timeout_from_the_sdks_httpx_is_unsafe_to_replay(self) -> None:
        """The control: the other half must still classify as billable."""
        sdk_httpx = self._sdk_httpx()
        err = openai.APITimeoutError(request=sdk_httpx.Request("POST", "http://x/v1"))
        err.__cause__ = sdk_httpx.ReadTimeout("boom")

        assert isinstance(_classify(err, "ollama"), UnsafeToReplayError)

    def test_an_unknown_cause_still_fails_closed(self) -> None:
        err = openai.APITimeoutError(request=self._sdk_httpx().Request("POST", "http://x/v1"))
        err.__cause__ = RuntimeError("something nobody mapped")

        assert isinstance(_classify(err, "ollama"), UnsafeToReplayError), (
            "an unrecognised cause must be assumed billable"
        )
