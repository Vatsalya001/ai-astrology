"""The production adapter, driven against a fake transport.

── Why `httpx2` here and `httpx` in the Google tests ──

Not a typo. The Anthropic SDK moved to httpx2 and google-genai has not,
so the two live side by side in the lockfile and a `MockTransport` from
the wrong one raises a type error deep inside `build_request` rather than
failing on the assertion. Worth the line of explanation: the error it
produces names neither library clearly.
"""

from __future__ import annotations

import json
from typing import Any

import httpx2
import pytest

from app.providers import (
    AnthropicProvider,
    CompletionRequest,
    Message,
    ModelMap,
    ProviderError,
    RequestMetadata,
    SystemBlock,
)

MODELS = ModelMap(fast="claude-haiku-4-5", chat="claude-sonnet-5", deep="claude-opus-5")


def build(handler: httpx2.MockTransport, **kwargs: object) -> AnthropicProvider:
    provider = AnthropicProvider(api_key="test-key", models=MODELS, **kwargs)  # type: ignore[arg-type]
    # Swapped under the SDK's own client, so auth headers, serialisation
    # and parsing are all the real thing and only the socket is fake.
    provider._client._client = httpx2.AsyncClient(transport=handler)
    return provider


def a_request(**kwargs: object) -> CompletionRequest:
    return CompletionRequest(
        messages=[Message(role="user", content="hello")],
        tier="chat",
        metadata=RequestMetadata(trace_id="t-1"),
        **kwargs,  # type: ignore[arg-type]
    )


def message(
    *,
    content: list[dict[str, Any]] | None = None,
    stop_reason: str = "end_turn",
    usage: dict[str, int] | None = None,
    model: str = "claude-sonnet-5",
) -> dict[str, Any]:
    return {
        "id": "msg_1",
        "type": "message",
        "role": "assistant",
        "model": model,
        "content": content if content is not None else [{"type": "text", "text": "Saturn."}],
        "stop_reason": stop_reason,
        "stop_sequence": None,
        "usage": usage or {"input_tokens": 10, "output_tokens": 3},
    }


def serving(body: dict[str, Any], status: int = 200) -> httpx2.MockTransport:
    return httpx2.MockTransport(lambda _req: httpx2.Response(status, json=body))


def capturing(seen: dict[str, Any], body: dict[str, Any] | None = None) -> httpx2.MockTransport:
    def handler(request: httpx2.Request) -> httpx2.Response:
        seen.update(json.loads(request.content))
        return httpx2.Response(200, json=body or message())

    return httpx2.MockTransport(handler)


# ─── the cache breakpoint ────────────────────────────────────────────


class TestCacheBreakpoint:
    """The largest cost lever in the service, so the most tested thing here.

    Every assertion below is about money rather than correctness: get any
    of them wrong and responses stay perfect while the bill multiplies,
    which is precisely the kind of regression no user reports.
    """

    async def test_the_breakpoint_lands_after_the_last_cacheable_block(self) -> None:
        seen: dict[str, Any] = {}
        provider = build(capturing(seen))

        await provider.complete(
            a_request(
                system=[
                    SystemBlock(content="rules", name="astrology", cacheable=True),
                    SystemBlock(content="persona", name="voice", cacheable=True),
                    SystemBlock(content="chart", name="chart_context"),
                ]
            )
        )

        blocks = seen["system"]
        assert "cache_control" not in blocks[0], (
            "an interior breakpoint spends one of the four available on a boundary "
            "no request ever splits at"
        )
        assert blocks[1]["cache_control"] == {"type": "ephemeral"}
        assert "cache_control" not in blocks[2], (
            "the breakpoint landed after the volatile chart block, which puts the "
            "user's own chart inside the cached prefix — the prefix then changes on "
            "every request and the hit rate is exactly zero"
        )

    async def test_exactly_one_breakpoint_is_spent(self) -> None:
        # Four is the hard limit. A marker per block exhausts it at five
        # modules and the request is rejected outright.
        seen: dict[str, Any] = {}
        provider = build(capturing(seen))

        await provider.complete(
            a_request(
                system=[SystemBlock(content=f"m{i}", cacheable=True) for i in range(6)]
                + [SystemBlock(content="volatile")]
            )
        )

        marked = [b for b in seen["system"] if "cache_control" in b]
        assert len(marked) == 1
        assert marked[0]["text"] == "m5"

    async def test_no_cacheable_block_means_no_breakpoint(self) -> None:
        """Rather than defaulting to the last block.

        A caller that marked nothing cacheable is saying the whole prompt
        is volatile. Marking the end anyway would pay the 1.25x cache
        WRITE premium on every request and read back nothing, which is
        strictly worse than not caching at all.
        """
        seen: dict[str, Any] = {}
        provider = build(capturing(seen))

        await provider.complete(a_request(system=[SystemBlock(content="all volatile")]))

        assert all("cache_control" not in block for block in seen["system"])

    async def test_blocks_are_sent_separately_not_joined(self) -> None:
        # The OpenAI adapter joins them because that format has nowhere to
        # attach cache control. Joining here would destroy the only thing
        # this adapter exists to do.
        seen: dict[str, Any] = {}
        provider = build(capturing(seen))

        await provider.complete(
            a_request(system=[SystemBlock(content="a", cacheable=True), SystemBlock(content="b")])
        )

        assert len(seen["system"]) == 2


# ─── usage, which is what the cache is measured by ───────────────────


class TestUsage:
    async def test_the_three_input_classes_stay_disjoint(self) -> None:
        """Fresh, written and read are priced about twelve-fold apart.

        Anthropic reports them as non-overlapping counts. Collapsing them
        into one `input_tokens` makes the call unpriceable — and the
        natural collapse (treat everything as fresh) overcharges a cached
        request by an order of magnitude on the largest part of the
        prompt.
        """
        provider = build(
            serving(
                message(
                    usage={
                        "input_tokens": 40,
                        "output_tokens": 300,
                        "cache_read_input_tokens": 8000,
                        "cache_creation_input_tokens": 120,
                    }
                )
            )
        )

        usage = (await provider.complete(a_request())).usage

        assert usage.input_tokens == 40
        assert usage.cached_input_tokens == 8000
        assert usage.cache_write_input_tokens == 120
        assert usage.output_tokens == 300

    async def test_missing_cache_counters_are_zero_not_an_error(self) -> None:
        # An uncached request omits them entirely.
        provider = build(serving(message()))

        usage = (await provider.complete(a_request())).usage

        assert usage.cached_input_tokens == 0
        assert usage.cache_write_input_tokens == 0

    async def test_cost_is_left_at_zero_for_the_pricing_module(self) -> None:
        """The adapter reports tokens; `app/pricing.py` owns rates.

        Asserted rather than assumed, because an adapter that quietly
        started computing cost would double-count once the orchestrator
        applies pricing on top.
        """
        provider = build(serving(message()))

        assert (await provider.complete(a_request())).usage.cost_micros == 0


# ─── refusal, effort, and reading the right blocks ───────────────────


class TestResponseHandling:
    async def test_a_refusal_is_a_refusal_and_not_an_error(self) -> None:
        """The spec: surface it as "let's approach this differently".

        Mapped to `error` it would be retried at every provider in the
        chain and then shown as a failure to a user whose question the
        product has a written answer for.
        """
        provider = build(serving(message(stop_reason="refusal")))

        assert (await provider.complete(a_request())).finish_reason == "refusal"

    async def test_text_accompanying_a_refusal_is_still_returned(self) -> None:
        # Discarding it leaves the safety path with a code and nothing to
        # show.
        provider = build(
            serving(
                message(
                    content=[{"type": "text", "text": "I can't help with that."}],
                    stop_reason="refusal",
                )
            )
        )

        assert (await provider.complete(a_request())).text == "I can't help with that."

    async def test_thinking_blocks_are_not_returned_to_the_user(self) -> None:
        """`content[0].text` is the obvious line and it is wrong.

        With thinking on, the first block is the model's reasoning.
        Returning it is both a wrong answer and a disclosure of how the
        system prompt steered it.
        """
        provider = build(
            serving(
                message(
                    content=[
                        {"type": "thinking", "thinking": "the user seems upset", "signature": "s"},
                        {"type": "text", "text": "Saturn is traditionally read as..."},
                    ]
                )
            )
        )

        text = (await provider.complete(a_request())).text

        assert text == "Saturn is traditionally read as..."
        assert "upset" not in text

    async def test_max_tokens_becomes_length(self) -> None:
        provider = build(serving(message(stop_reason="max_tokens")))

        assert (await provider.complete(a_request())).finish_reason == "length"

    @pytest.mark.parametrize(
        ("tier", "effort"), [("fast", "low"), ("chat", "medium"), ("deep", "high")]
    )
    async def test_effort_follows_the_tier(self, tier: str, effort: str) -> None:
        """Paying deep-reasoning prices for a 21-way label is the
        easiest way to make a cheap product expensive."""
        seen: dict[str, Any] = {}
        provider = build(capturing(seen))

        await provider.complete(
            CompletionRequest(
                messages=[Message(role="user", content="x")],
                tier=tier,  # type: ignore[arg-type]
                metadata=RequestMetadata(trace_id="t"),
            )
        )

        assert seen["output_config"]["effort"] == effort

    async def test_the_tier_selects_the_model(self) -> None:
        seen: dict[str, Any] = {}
        provider = build(capturing(seen))

        await provider.complete(a_request())

        assert seen["model"] == "claude-sonnet-5"

    async def test_a_json_schema_becomes_a_real_constraint(self) -> None:
        """Not a "please reply in JSON" instruction in the prompt.

        Constrained by the sampler means a malformed object is not a
        thing the parser downstream has to handle at all.
        """
        seen: dict[str, Any] = {}
        provider = build(capturing(seen))
        schema = {"type": "object", "properties": {"intent": {"type": "string"}}}

        await provider.complete(a_request(json_schema=schema))

        assert seen["output_config"]["format"] == {"type": "json_schema", "schema": schema}

    async def test_temperature_is_not_sent(self) -> None:
        """`messages.create` has no temperature parameter; effort replaced it.

        Asserted so the omission reads as a decision rather than a
        forgotten field — and so that a future SDK reintroducing the
        parameter surfaces as a failing test rather than as a silent
        difference in how the paid and free tiers sample.
        """
        seen: dict[str, Any] = {}
        provider = build(capturing(seen))

        await provider.complete(a_request(temperature=0.1))

        assert "temperature" not in seen


# ─── error classification ────────────────────────────────────────────


class TestRetryClassification:
    @pytest.mark.parametrize("status", [429, 500, 502, 503, 529])
    async def test_transient_statuses_are_retryable(self, status: int) -> None:
        provider = build(serving({"type": "error", "error": {"message": "x"}}, status=status))

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        assert caught.value.retryable is True

    @pytest.mark.parametrize("status", [400, 401, 403, 404, 413, 422])
    async def test_client_errors_are_not_retryable(self, status: int) -> None:
        """401 is the one that matters most.

        A bad key is not transient. Retrying it walks the same broken
        credential through every provider in the chain and leaves a log
        that blames the last one for the first one's problem.
        """
        provider = build(serving({"type": "error", "error": {"message": "x"}}, status=status))

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        assert caught.value.retryable is False
        assert caught.value.status_code == status

    async def test_an_unreachable_backend_is_retryable(self) -> None:
        def refuse(_req: httpx2.Request) -> httpx2.Response:
            raise httpx2.ConnectError("connection refused")

        provider = build(httpx2.MockTransport(refuse))

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        assert caught.value.retryable is True

    async def test_the_error_message_carries_no_response_body(self) -> None:
        """An API error echoes the request on some paths.

        `.claude/rules/security.md`: a key never reaches an error
        message. A message built from the status code alone cannot leak
        one by accident, however the vendor changes its error shape.
        """
        provider = build(
            serving(
                {"type": "error", "error": {"message": "invalid x-api-key sk-ant-SECRET"}},
                status=401,
            )
        )

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        assert "SECRET" not in str(caught.value)


async def test_a_missing_key_fails_at_construction() -> None:
    """Rather than on the first request.

    This is the production provider. A missing key discovered at startup
    costs a restart; discovered at 3am it means the chain has been
    quietly serving from a free-tier fallback and nobody noticed until
    the quality did.
    """
    with pytest.raises(ProviderError, match="API key"):
        AnthropicProvider(api_key="", models=MODELS)


async def test_the_tier_defaults_to_paid() -> None:
    """Which is what the PII guard reads.

    Defaulting to anything else would let the production guard pass with
    a provider the guard exists to check.
    """
    assert build(serving(message())).tier == "paid"
