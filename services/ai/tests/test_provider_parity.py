"""One suite, every adapter. PHASE-04 §12 task 4.21, §13 "Provider parity".

    "One suite run against every adapter, asserting schema-valid output
    and consistent exception types. Mock and Ollama runs are free and run
    in CI; Anthropic and Google runs are manual, on demand."

── What parity means here, and what it does not ──

It does NOT mean the adapters produce the same TEXT. They wrap different
models; identical output would mean the abstraction had flattened the
thing it exists to let you choose between.

It means everything AROUND the text is identical: the response shape, the
exception type, the `retryable` decision, and what happens at each edge.
The registry's fallback loop reads `retryable` and nothing else, so an
adapter that disagrees about it breaks failover for every other adapter
in the chain.

── Why these run offline against a fake transport ──

Each adapter is driven through its own SDK against `MockTransport`, so
the SDK's parsing is real and only the socket is fake. A suite that
patched each client's method would assert that four adapters call four
methods, which is not parity — it is four tautologies.

`MockProvider` is included deliberately. It is the adapter CI actually
runs the pipeline on, so an inconsistency between it and the real ones
means every integration test in this repository is exercising a shape
production does not have.
"""

from __future__ import annotations

import json
from collections.abc import Callable
from pathlib import Path
from typing import Any

import httpx
import httpx2
import pytest

from app.providers import (
    AnthropicProvider,
    Capabilities,
    CompletionRequest,
    CompletionResponse,
    GoogleProvider,
    LLMProvider,
    Message,
    MockProvider,
    ModelMap,
    OpenAICompatibleProvider,
    ProviderError,
    RequestMetadata,
    SystemBlock,
    Usage,
)

# ─── the statuses every adapter must agree about ─────────────────────
#
# The registry's fallback loop reads `retryable` and nothing else, so a
# code one adapter calls transient and another calls permanent is a
# failover that works on one provider and dead-ends on the next.
#
# This list used to be `[429, 500, 503]`, which is exactly the set of
# codes nobody gets wrong. The ones that broke were the unglamorous
# 5xx: 529 is Anthropic's "overloaded", and 520/522/524 come from a
# Cloudflare edge in front of an OpenRouter-style proxy. The OpenAI
# adapter enumerated 5xx code by code and so marked every one of them
# permanent.

RETRYABLE_STATUSES = (429, 500, 501, 502, 503, 504, 520, 522, 524, 529)
"""501 is in here deliberately; see `resilience.is_retryable_status` for why."""

PERMANENT_STATUSES = (400, 401, 403, 404, 422)

# A credential-shaped string, planted in every error fixture below.
#
# Without one, `test_the_error_carries_no_response_body` was vacuous:
# the fixtures said `"message": "x"`, which contains none of the four
# strings that test greps for, so it passed however the adapters
# behaved — and the OpenAI adapter was in fact interpolating the whole
# response body ("Error code: 401 - {'error': {'message': ...}}") into
# its own message. A guard nothing can fail is not a guard.
LEAKED_ANTHROPIC_KEY = "sk-ant-api03-" + "A" * 24
LEAKED_GOOGLE_KEY = "AIzaSy" + "B" * 27
CREDENTIAL_SHAPED = (
    f"Incorrect API key provided: {LEAKED_ANTHROPIC_KEY}. "
    f"Sent as `Authorization: Bearer {LEAKED_ANTHROPIC_KEY}` "
    f"and `x-api-key: {LEAKED_GOOGLE_KEY}`."
)

CREDENTIAL_MARKERS = ("sk-ant-", "AIzaSy", "Bearer ", "x-api-key")
"""Every one of these appears in `CREDENTIAL_SHAPED`, and the assertion
below is only meaningful because of it. If you edit that string, check
this list still describes it."""


# ─── one builder per adapter ─────────────────────────────────────────
#
# Each returns a provider wired to a transport that answers however the
# test asks. The shapes differ because the wire formats differ — that is
# the whole point of having four adapters — and everything above the
# transport is the real SDK.


def _openai_body(text: str = "Saturn.", finish: str = "stop") -> dict[str, Any]:
    return {
        "id": "chatcmpl-1",
        "object": "chat.completion",
        "created": 1,
        "model": "qwen2.5:7b",
        "choices": [
            {"index": 0, "message": {"role": "assistant", "content": text}, "finish_reason": finish}
        ],
        "usage": {"prompt_tokens": 11, "completion_tokens": 7, "total_tokens": 18},
    }


def _anthropic_body(text: str = "Saturn.", stop: str = "end_turn") -> dict[str, Any]:
    return {
        "id": "msg_1",
        "type": "message",
        "role": "assistant",
        "model": "claude-sonnet-5",
        "content": [{"type": "text", "text": text}],
        "stop_reason": stop,
        "stop_sequence": None,
        "usage": {"input_tokens": 11, "output_tokens": 7},
    }


def _google_body(text: str = "Saturn.", finish: str = "STOP") -> dict[str, Any]:
    return {
        "candidates": [
            {"content": {"role": "model", "parts": [{"text": text}]}, "finishReason": finish}
        ],
        "usageMetadata": {"promptTokenCount": 11, "candidatesTokenCount": 7},
        "modelVersion": "gemini-2.5-flash",
    }


def _openai_sse(text: str) -> str:
    """Three chunks, because a one-chunk stream does not exercise the join.

    The bug a streaming test is for is in the assembly, and a consumer
    that only ever sees one fragment never runs the code that joins
    them.
    """
    words = text.split(" ")
    pieces = [w if i == len(words) - 1 else w + " " for i, w in enumerate(words)]
    events = [
        json.dumps(
            {
                "id": "1",
                "object": "chat.completion.chunk",
                "created": 1,
                "model": "qwen2.5:7b",
                "choices": [{"index": 0, "delta": {"content": p}, "finish_reason": None}],
            }
        )
        for p in pieces
    ]
    events.append(
        json.dumps(
            {
                "id": "1",
                "object": "chat.completion.chunk",
                "created": 1,
                "model": "qwen2.5:7b",
                "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}],
            }
        )
    )
    # The frame `stream_options={"include_usage": true}` buys, and the
    # reason the adapter cannot skip chunks with an empty `choices`
    # list: this is where the only cost figure a streamed call ever
    # reports lives.
    events.append(
        json.dumps(
            {
                "id": "1",
                "object": "chat.completion.chunk",
                "created": 1,
                "model": "qwen2.5:7b",
                "choices": [],
                "usage": {"prompt_tokens": 11, "completion_tokens": 7, "total_tokens": 18},
            }
        )
    )
    return "".join(f"data: {e}\n\n" for e in events) + "data: [DONE]\n\n"


def _anthropic_sse(text: str) -> str:
    words = text.split(" ")
    pieces = [w if i == len(words) - 1 else w + " " for i, w in enumerate(words)]

    lines = [
        (
            "message_start",
            {
                "type": "message_start",
                "message": {
                    "id": "msg_1",
                    "type": "message",
                    "role": "assistant",
                    "model": "claude-sonnet-5",
                    "content": [],
                    "stop_reason": None,
                    "stop_sequence": None,
                    "usage": {"input_tokens": 11, "output_tokens": 0},
                },
            },
        ),
        (
            "content_block_start",
            {
                "type": "content_block_start",
                "index": 0,
                "content_block": {"type": "text", "text": ""},
            },
        ),
    ]
    lines += [
        (
            "content_block_delta",
            {
                "type": "content_block_delta",
                "index": 0,
                "delta": {"type": "text_delta", "text": p},
            },
        )
        for p in pieces
    ]
    lines += [
        ("content_block_stop", {"type": "content_block_stop", "index": 0}),
        (
            "message_delta",
            {
                "type": "message_delta",
                "delta": {"stop_reason": "end_turn", "stop_sequence": None},
                "usage": {"output_tokens": 7},
            },
        ),
        ("message_stop", {"type": "message_stop"}),
    ]
    return "".join(f"event: {name}\ndata: {json.dumps(payload)}\n\n" for name, payload in lines)


def _wants_stream(content: bytes) -> bool:
    """Did the caller ask for a stream?

    Read from the request body rather than from a flag the builder was
    given, so one transport serves both shapes and the same provider
    object can be used for `complete` and `stream` — which is how a real
    backend behaves, and is what makes the two tests comparable.
    """
    try:
        return bool(json.loads(content or b"{}").get("stream"))
    except (json.JSONDecodeError, ValueError):
        return False


def build_openai(*, status: int = 200, text: str = "Saturn.") -> LLMProvider:
    body = (
        _openai_body(text)
        if status == 200
        else {"error": {"message": CREDENTIAL_SHAPED, "type": "invalid_request_error"}}
    )

    def handle(request: httpx.Request) -> httpx.Response:
        if status == 200 and _wants_stream(request.content):
            return httpx.Response(
                200, text=_openai_sse(text), headers={"content-type": "text/event-stream"}
            )
        return httpx.Response(status, json=body)

    provider = OpenAICompatibleProvider(
        base_url="http://localhost:11434/v1",
        api_key="",
        tier="local",
        models=ModelMap(fast="llama3.2:3b", chat="qwen2.5:7b", deep="qwen2.5:7b"),
        provider_id="ollama",
    )
    provider._client._client = httpx.AsyncClient(transport=httpx.MockTransport(handle))
    return provider


def build_anthropic(*, status: int = 200, text: str = "Saturn.") -> LLMProvider:
    body = (
        _anthropic_body(text)
        if status == 200
        else {
            "type": "error",
            "error": {"type": "authentication_error", "message": CREDENTIAL_SHAPED},
        }
    )

    def handle(request: httpx2.Request) -> httpx2.Response:
        if status == 200 and _wants_stream(request.content):
            return httpx2.Response(
                200, text=_anthropic_sse(text), headers={"content-type": "text/event-stream"}
            )
        return httpx2.Response(status, json=body)

    provider = AnthropicProvider(
        api_key="test-key",
        models=ModelMap(fast="claude-haiku-4-5", chat="claude-sonnet-5", deep="claude-opus-5"),
    )
    # httpx2, not httpx — the Anthropic SDK moved and google-genai has
    # not. A MockTransport from the wrong one fails inside the request
    # builder with an error that names neither library.
    provider._client._client = httpx2.AsyncClient(transport=httpx2.MockTransport(handle))
    return provider


def build_google(*, status: int = 200, text: str = "Saturn.") -> LLMProvider:
    body = (
        _google_body(text)
        if status == 200
        else {
            "error": {
                "code": status,
                "message": CREDENTIAL_SHAPED,
                "status": "UNAUTHENTICATED",
            }
        }
    )
    provider = GoogleProvider(
        api_key="test-key",
        models=ModelMap(fast="gemini-2.0-flash", chat="gemini-2.5-flash", deep="gemini-2.5-pro"),
    )
    provider._client._api_client._async_httpx_client = httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _r: httpx.Response(status, json=body))
    )
    return provider


def build_mock(*, status: int = 200, text: str = "Saturn.") -> LLMProvider:
    provider = MockProvider(
        Path("/tmp/parity-fixtures-unused"), allow_unknown=True, default_text=text
    )
    if status != 200:
        # Built from the status alone, exactly as every real adapter's
        # `_classify` is, and deliberately NOT from the shared
        # `is_retryable_status` — a builder that called the same helper
        # the adapters call would agree with them by construction and
        # make the mock a witness to nothing.
        #
        # `CREDENTIAL_SHAPED` is in scope here and is deliberately not
        # interpolated. MockProvider is the adapter CI runs the whole
        # pipeline on, so a leak into its error text would be a leak in
        # every integration log this repository produces.
        provider.fail_next = ProviderError(
            f"mock returned {status}",
            provider_id="mock",
            retryable=status == 429 or status >= 500,
            status_code=status,
        )
    return provider


ADAPTERS: dict[str, Callable[..., LLMProvider]] = {
    "openai-compatible": build_openai,
    "anthropic": build_anthropic,
    "google": build_google,
    "mock": build_mock,
}

# Parametrised by NAME so a failure says which adapter, not
# `<function build_google at 0x...>`.
every_adapter = pytest.mark.parametrize("adapter", list(ADAPTERS), ids=list(ADAPTERS))


def a_request(**kwargs: Any) -> CompletionRequest:
    return CompletionRequest(
        messages=[Message(role="user", content="what does my chart say")],
        system=[
            SystemBlock(content="You are a Vedic astrology companion.", cacheable=True),
            SystemBlock(content="Chart: Saturn in the 4th."),
        ],
        tier="chat",
        metadata=RequestMetadata(trace_id="parity"),
        **kwargs,
    )


# ─── the response contract ───────────────────────────────────────────


class TestTheResponseShapeIsIdentical:
    @every_adapter
    async def test_it_returns_a_completion_response(self, adapter: str) -> None:
        response = await ADAPTERS[adapter]().complete(a_request())

        assert isinstance(response, CompletionResponse)
        assert isinstance(response.usage, Usage)

    @every_adapter
    async def test_every_field_is_populated(self, adapter: str) -> None:
        """Not just present — populated.

        Pydantic guarantees the fields EXIST. A provider that left
        `model` or `provider_id` empty would satisfy the type and
        produce a telemetry row that cannot explain which model answered
        — which is the whole reason those two columns exist.
        """
        response = await ADAPTERS[adapter]().complete(a_request())

        assert response.text, f"{adapter} returned no text"
        assert response.model, f"{adapter} did not report a model"
        assert response.provider_id, f"{adapter} did not report a provider id"
        assert response.finish_reason in {"stop", "length", "refusal", "error"}

    @every_adapter
    async def test_token_counts_are_non_negative_integers(self, adapter: str) -> None:
        # The telemetry row sums these across millions of calls, and a
        # negative or float count would poison the total silently.
        usage = (await ADAPTERS[adapter]().complete(a_request())).usage

        for name in (
            "input_tokens",
            "output_tokens",
            "cached_input_tokens",
            "cache_write_input_tokens",
            "cost_micros",
        ):
            value = getattr(usage, name)
            assert isinstance(value, int), f"{adapter}.{name} is {type(value).__name__}"
            assert value >= 0, f"{adapter}.{name} is negative"

    @every_adapter
    async def test_cost_is_left_for_the_pricing_module(self, adapter: str) -> None:
        """Every adapter reports tokens; none prices them.

        An adapter that started computing cost would be double-counted
        the moment the orchestrator applies pricing on top — and the
        error would be a quiet overstatement rather than a failure.
        """
        assert (await ADAPTERS[adapter]().complete(a_request())).usage.cost_micros == 0

    @every_adapter
    async def test_the_three_capability_flags_are_declared(self, adapter: str) -> None:
        capabilities = ADAPTERS[adapter]().capabilities

        assert isinstance(capabilities, Capabilities)
        assert isinstance(capabilities.json_mode, bool)
        assert isinstance(capabilities.prompt_caching, bool)
        assert capabilities.max_context_tokens > 0

    @every_adapter
    async def test_the_tier_is_one_the_pii_guard_understands(self, adapter: str) -> None:
        """`app/guards.py` reads exactly this, and refuses to boot on
        anything but `paid` in production.

        An adapter reporting a tier outside the Literal would make the
        guard's comparison meaningless — it would simply never match
        `paid` and would therefore always refuse, or always allow,
        depending on which way the check was written.
        """
        assert ADAPTERS[adapter]().tier in {"local", "free-hosted", "paid"}


# ─── the exception contract, which is what fallback depends on ───────


async def _verdict(adapter: str, status: int) -> bool:
    """What one adapter decides about one status code."""
    with pytest.raises(ProviderError) as caught:
        await ADAPTERS[adapter](status=status).complete(a_request())
    return caught.value.retryable


class TestTheExceptionContract:
    @every_adapter
    @pytest.mark.parametrize("status", RETRYABLE_STATUSES)
    async def test_a_transient_failure_is_retryable(self, adapter: str, status: int) -> None:
        """The registry's fallback loop reads `retryable` and nothing else.

        An adapter that marks a 503 permanent does not merely fail
        itself — it stops the chain walking to the next provider, so one
        adapter's mistake becomes an outage for a configuration that had
        a working fallback.
        """
        assert await _verdict(adapter, status) is True, f"{adapter} marked {status} permanent"

    @every_adapter
    @pytest.mark.parametrize("status", PERMANENT_STATUSES)
    async def test_a_permanent_failure_is_not_retryable(self, adapter: str, status: int) -> None:
        """The opposite error, and it is the expensive one.

        A malformed request marked retryable is sent to every provider
        in the chain, which turns one bad request into six and leaves a
        log blaming the last provider for the first one's problem.
        """
        assert await _verdict(adapter, status) is False, f"{adapter} marked {status} retryable"

    @pytest.mark.parametrize("status", RETRYABLE_STATUSES + PERMANENT_STATUSES)
    async def test_the_four_adapters_agree_on_every_status(self, status: int) -> None:
        """Agreement, asserted across all four at once rather than four times.

        The two tests above would catch a disagreement, but they report
        it as "google failed" on one line and say nothing about the
        other three — and the defect being guarded is definitionally a
        RELATIONSHIP between adapters, not a property of one. Collecting
        the verdicts first means the failure names the dissenter and the
        majority it broke away from, which is the sentence someone
        debugging a half-working failover needs.
        """
        expected = status in RETRYABLE_STATUSES
        verdicts = {name: await _verdict(name, status) for name in ADAPTERS}

        assert verdicts == dict.fromkeys(ADAPTERS, expected), (
            f"the adapters disagree about HTTP {status}: {verdicts}. The registry "
            f"reads this flag and nothing else, so failover now works on some "
            f"providers and dead-ends on others for the same failure."
        )

    @every_adapter
    @pytest.mark.parametrize("status", [400, 401, 403])
    async def test_the_error_carries_no_response_body(self, adapter: str, status: int) -> None:
        """`.claude/rules/security.md`: a key never reaches an error message.

        Two of these SDKs interpolate the whole response body into
        `str(err)`, and an auth failure is exactly where a credential
        would appear. Built from the status code instead, uniformly.

        The error fixtures each serve `CREDENTIAL_SHAPED`, which
        contains all four markers below. They previously said
        `"message": "x"` and this test passed whatever the adapters did
        — while the OpenAI adapter interpolated the entire body.
        """
        provider = ADAPTERS[adapter](status=status)

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        message = str(caught.value)
        for secret in CREDENTIAL_MARKERS:
            assert secret not in message, f"{adapter} leaked {secret!r} into its error"

    @pytest.mark.parametrize("secret", CREDENTIAL_MARKERS)
    def test_the_credential_fixture_actually_contains_a_credential(self, secret: str) -> None:
        """The assertion that makes the one above non-vacuous.

        `test_the_error_carries_no_response_body` greps for four strings.
        If the fixture stops containing them — a refactor, a tidied-up
        message — that test goes green forever without anyone touching
        an adapter. This one fails instead.
        """
        assert secret in CREDENTIAL_SHAPED

    # The mock is excluded because it has no wire: its error is built in
    # the builder rather than decoded from a response, so there is no
    # body for a credential to be in and nothing to prove arrived. Every
    # adapter that parses a real one is covered.
    @pytest.mark.parametrize("adapter", [name for name in ADAPTERS if name != "mock"])
    async def test_the_credential_reaches_every_adapters_error_path(self, adapter: str) -> None:
        """The other half of the vacuity hole, and the half that bit.

        `test_the_credential_fixture_actually_contains_a_credential`
        guards the STRING. Nothing guarded the WIRING — and reverting a
        single builder's error body to `{"message": "x"}` leaves that
        one green while silently re-vacuuming the leak test for that
        adapter. Verified by doing it: with the OpenAI adapter still
        interpolating the entire decoded body into its message, every
        leak assertion passed.

        Asserted on `__cause__`, the SDK's own exception, because that
        is the only evidence the fixture was actually served, decoded,
        and handed to `_classify` with the credential still in it. A
        failure here means the leak test above cannot fail for this
        adapter, whatever the adapter does.
        """
        with pytest.raises(ProviderError) as caught:
            await ADAPTERS[adapter](status=401).complete(a_request())

        raw = str(caught.value.__cause__)
        for secret in CREDENTIAL_MARKERS:
            assert secret in raw, (
                f"{adapter} never saw {secret!r}: its error fixture no longer serves a "
                f"credential, so test_the_error_carries_no_response_body is vacuous for it"
            )

    @every_adapter
    async def test_the_provider_id_is_on_the_error(self, adapter: str) -> None:
        # The registry logs which provider failed before moving on. An
        # error with no id produces a chain failure nobody can attribute.
        provider = ADAPTERS[adapter](status=500)

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        assert caught.value.provider_id


# ─── streaming ───────────────────────────────────────────────────────


class TestStreamingParity:
    @every_adapter
    async def test_a_stream_reassembles_to_the_same_text(self, adapter: str) -> None:
        """The property a consumer actually depends on.

        Chunk boundaries differ per provider and always will. What must
        not differ is that joining them gives the answer — a consumer
        that works on one adapter and drops the last word on another is
        the bug this asserts against.
        """
        text = "Saturn is traditionally read as patience rather than punishment."
        provider = ADAPTERS[adapter](text=text)

        chunks = [chunk async for chunk in provider.stream(a_request())]

        assert "".join(chunk.text for chunk in chunks) == text

    @every_adapter
    async def test_usage_arrives_exactly_once_and_on_the_final_chunk(self, adapter: str) -> None:
        """Exactly one, and last. `<= 1` is satisfied by ZERO.

        That was the hole. This asserted `len(carrying) <= 1`, which the
        OpenAI adapter satisfied by never emitting usage at all — it
        sent no `stream_options`, so the backend reported nothing, and
        then it skipped every chunk with an empty `choices` list, which
        is precisely the frame usage arrives on. Streaming through that
        provider was free in the telemetry and nobody could see it,
        because an absent cost reads the same as a cheap one.

        "On the final chunk" matters as much as "once". Google repeats
        running totals on every frame and Anthropic splits them across
        two events; both normalise to one trailing chunk. A consumer
        cannot know when to read the number unless every adapter puts it
        in the same place, and a consumer that summed what it received
        would be right on one provider and wrong on another.

        The text is deliberately long. An earlier version used the
        default one-word answer, which produces a single chunk on every
        adapter — so the assertion held whatever the implementation did.
        """
        provider = ADAPTERS[adapter](
            text="Saturn is traditionally read as patience rather than punishment."
        )

        chunks = [chunk async for chunk in provider.stream(a_request())]
        assert len(chunks) > 1, (
            f"{adapter} produced one chunk for a twelve-word answer; this test cannot "
            f"see a per-chunk usage bug without several chunks"
        )

        carrying = [index for index, chunk in enumerate(chunks) if chunk.usage is not None]

        assert carrying == [len(chunks) - 1], (
            f"{adapter} put usage on chunks {carrying} of {len(chunks)}; it belongs on "
            f"the last one and only there. An empty list means this provider streams "
            f"for free as far as the telemetry is concerned."
        )

    @every_adapter
    async def test_the_finish_reason_arrives_on_the_final_chunk(self, adapter: str) -> None:
        """Same place on every adapter, for the same reason as usage.

        A consumer closing its UI state on `finish_reason` and reading
        cost off the same chunk works everywhere or nowhere. The OpenAI
        adapter used to emit the reason on the frame that carried it and
        stop, leaving the two on different chunks — on the providers
        where it emitted usage at all.
        """
        provider = ADAPTERS[adapter](
            text="Saturn is traditionally read as patience rather than punishment."
        )

        chunks = [chunk async for chunk in provider.stream(a_request())]

        carrying = [index for index, chunk in enumerate(chunks) if chunk.finish_reason is not None]

        assert carrying == [len(chunks) - 1], (
            f"{adapter} put finish_reason on chunks {carrying} of {len(chunks)}"
        )


# ─── health ──────────────────────────────────────────────────────────


class TestHealthParity:
    @every_adapter
    async def test_a_health_check_never_raises(self, adapter: str) -> None:
        """A probe that raises is a probe nobody calls.

        The registry reports health for every provider at once; one dead
        backend must not take the whole endpoint down with it.
        """
        provider = ADAPTERS[adapter](status=500)

        result = await provider.health_check()

        assert isinstance(result, bool)


# ─── the suite is actually running against everything ────────────────


def test_every_adapter_in_the_package_is_covered() -> None:
    """The guard against this file quietly testing three of four.

    A new adapter added to `app/providers/` and not to `ADAPTERS` would
    leave every test above passing while the new one was never
    exercised — and the whole point of a parity suite is that it covers
    the set.
    """
    import app.providers as providers

    exported = {
        name
        for name in providers.__all__
        if name.endswith("Provider") and name not in {"LLMProvider", "EmbeddingProvider"}
    }
    # Embedding providers are a different protocol with a different
    # shape, and belong in their own suite.
    exported -= {"OllamaEmbeddingProvider", "GoogleEmbeddingProvider", "ResilientProvider"}

    covered = {
        "OpenAICompatibleProvider",
        "AnthropicProvider",
        "GoogleProvider",
        "MockProvider",
    }

    assert exported == covered, (
        f"the parity suite covers {sorted(covered)} but app.providers exports "
        f"{sorted(exported)}. A new adapter must be added to ADAPTERS, or this "
        f"suite is passing while never touching it."
    )


def test_the_adapters_are_not_secretly_the_same_object() -> None:
    """A version of this suite where every builder returned a mock would
    pass every assertion above and prove nothing."""
    built = [ADAPTERS[name]() for name in ADAPTERS]

    assert len({type(p).__name__ for p in built}) == len(ADAPTERS)


@every_adapter
async def test_json_is_either_supported_or_refused_at_the_edge(adapter: str) -> None:
    """Never sent and hoped for.

    A backend without JSON mode returns prose, which fails to parse
    three layers away from the cause — in a parser that has no idea the
    provider was never capable. Every adapter either honours the schema
    or raises immediately, and both are acceptable; silently ignoring it
    is not.
    """
    provider = ADAPTERS[adapter]()
    request = a_request(json_schema={"type": "object"})

    if provider.capabilities.json_mode:
        response = await provider.complete(request)
        assert response.text
    else:
        with pytest.raises(ProviderError):
            await provider.complete(request)


@every_adapter
async def test_the_system_prompt_order_is_preserved(adapter: str) -> None:
    """Order is the only thing every wire format keeps.

    Two of these have no per-block cache control, so a stable prefix is
    purely a matter of the blocks arriving in the same sequence. An
    adapter that reordered or dropped one would take the hit rate to
    zero on that provider alone, with every response still correct.
    """
    seen: dict[str, Any] = {}

    def capture_httpx(request: httpx.Request) -> httpx.Response:
        seen["body"] = json.loads(request.content)
        return httpx.Response(200, json=_openai_body())

    def capture_httpx2(request: httpx2.Request) -> httpx2.Response:
        seen["body"] = json.loads(request.content)
        return httpx2.Response(200, json=_anthropic_body())

    def capture_google(request: httpx.Request) -> httpx.Response:
        seen["body"] = json.loads(request.content)
        return httpx.Response(200, json=_google_body())

    match adapter:
        case "openai-compatible":
            provider = build_openai()
            provider._client._client = httpx.AsyncClient(  # type: ignore[attr-defined]
                transport=httpx.MockTransport(capture_httpx)
            )
        case "anthropic":
            provider = build_anthropic()
            provider._client._client = httpx2.AsyncClient(  # type: ignore[attr-defined]
                transport=httpx2.MockTransport(capture_httpx2)
            )
        case "google":
            provider = build_google()
            provider._client._api_client._async_httpx_client = (  # type: ignore[attr-defined]
                httpx.AsyncClient(transport=httpx.MockTransport(capture_google))
            )
        case _:
            # The mock keeps the request object itself, which is a
            # stronger check than reading a wire body.
            provider = build_mock()
            await provider.complete(a_request())
            blocks = provider.requests[0].system  # type: ignore[attr-defined]
            assert [b.content for b in blocks] == [
                "You are a Vedic astrology companion.",
                "Chart: Saturn in the 4th.",
            ]
            return

    await provider.complete(a_request())

    serialised = json.dumps(seen["body"])
    persona_at = serialised.index("Vedic astrology companion")
    chart_at = serialised.index("Saturn in the 4th")

    assert persona_at < chart_at, (
        f"{adapter} sent the volatile chart block before the stable persona block; "
        f"the cacheable prefix is no longer a prefix"
    )


# ─── the sinks the status-code test could not reach ──────────────────


class TestEveryErrorPathSuppressesCredentials:
    """The leak test drives the STATUS branch. There are three others.

    `test_the_error_carries_no_response_body` serves an HTTP error and
    checks the message, which exercises `_classify`'s status path and
    nothing else. Anthropic's connection branch and its catch-all both
    interpolated the exception itself, and `ProviderRegistry`'s failure
    map — rendered into `NoProviderAvailableError` — did the same for
    any unmapped exception. All three were unguarded, and the registry
    one is the catch-all by which an SDK body could reach a log even
    with every adapter clean.
    """

    SECRET = "sk-ant-LEAKED0000 x-api-key AIzaSyLEAKED Bearer LEAKED"

    def test_the_anthropic_connection_branch_carries_only_a_type(self) -> None:
        import anthropic as anthropic_sdk

        from app.providers.anthropic_provider import _classify

        err = anthropic_sdk.APIConnectionError(
            message=f"failed to connect: {self.SECRET}",
            request=httpx2.Request("POST", "https://api.anthropic.com/v1/messages"),
        )

        message = str(_classify(err, "anthropic"))

        assert "LEAKED" not in message, message
        assert "APIConnectionError" in message, "the type is what a reader needs"

    def test_the_anthropic_catch_all_carries_only_a_type(self) -> None:
        from app.providers.anthropic_provider import _classify

        message = str(_classify(RuntimeError(self.SECRET), "anthropic"))

        assert "LEAKED" not in message, message

    def test_the_google_transport_branch_carries_only_a_type(self) -> None:
        from app.providers.google_provider import _classify

        message = str(_classify(httpx.ConnectError(self.SECRET), "google"))

        assert "LEAKED" not in message, message

    def test_the_embedding_provider_carries_only_a_type(self) -> None:
        from app.providers.google_provider import _classify

        assert "LEAKED" not in str(_classify(OSError(self.SECRET), "google-embeddings"))

    async def test_the_registry_failure_map_carries_only_a_type(self) -> None:
        """The catch-all path, and the one that reaches a log line.

        `NoProviderAvailableError` renders this map. An SDK exception
        interpolated here would carry a request echo into the error the
        orchestrator logs — after every adapter had carefully suppressed
        its own.
        """
        from app.providers import NoProviderAvailableError, ProviderRegistry

        class Exploding:
            id = "exploding"
            tier = "local"
            capabilities = Capabilities()

            async def complete(self, req: CompletionRequest) -> CompletionResponse:
                raise RuntimeError(TestEveryErrorPathSuppressesCredentials.SECRET)

            def stream(self, req: CompletionRequest) -> Any:  # pragma: no cover
                raise NotImplementedError

            async def health_check(self) -> bool:
                return False

        registry = ProviderRegistry(env="development")
        registry.register(Exploding())  # type: ignore[arg-type]

        with pytest.raises(NoProviderAvailableError) as caught:
            await registry.complete(a_request())

        assert "LEAKED" not in str(caught.value), str(caught.value)
        assert "RuntimeError" in str(caught.value)
