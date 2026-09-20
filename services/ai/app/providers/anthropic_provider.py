"""Production. The only adapter where a mistake costs real money.

Three things this one does that the generic adapter cannot, all from
PHASE-04 §2:

  1. **Prompt caching.** A breakpoint after the stable prefix. For this
     product that prefix is large — astrology corpus, safety rules,
     persona — and identical on every single request, so it is the
     largest cost lever in the service by a wide margin.
  2. **Effort per tier.** `low` for a 21-way label, `high` for a paid
     reading. Paying deep-reasoning prices to classify an intent is the
     easiest way to make a cheap product expensive.
  3. **`stop_reason == "refusal"`.** Checked before reading content, and
     surfaced as a product outcome rather than an error.

── Testing ──

Driven against `httpx.MockTransport` under the SDK's own transport, for
the same reason the OpenAI adapter is: patching `messages.create` would
assert that the adapter calls a method it obviously calls, and would keep
passing through a response-shape change. The parse is what breaks on a
version bump, so the parse is what the tests exercise.
"""

from __future__ import annotations

import time
from collections.abc import AsyncIterator
from typing import Any, Literal

import anthropic
from anthropic import AsyncAnthropic

from app.providers.base import (
    Capabilities,
    CompletionChunk,
    CompletionRequest,
    CompletionResponse,
    FinishReason,
    ModelMap,
    ModelTier,
    ProviderError,
    ProviderTier,
    SystemBlock,
    Usage,
)

Effort = Literal["low", "medium", "high"]

_EFFORT_BY_TIER: dict[ModelTier, Effort] = {
    # Classification and extraction. The answer is a label from a fixed
    # set; there is nothing to reason about and reasoning tokens are
    # billed at output rates.
    "fast": "low",
    # Conversation. Quality is visible in every sentence a user reads.
    "chat": "medium",
    # A paid interpretation, read once and carefully. The only place
    # where the price of thinking is defensible.
    "deep": "high",
}

# Two facts about Anthropic's cache that shape `_system` below, recorded
# here rather than as constants nothing reads:
#
#   - At most FOUR breakpoints per request. This adapter spends exactly
#     one, at the end of the stable prefix, because `PromptBuilder`
#     composes that prefix as a unit and no caller sends a partial one.
#   - Below roughly 1024 tokens the marker is ignored entirely and
#     nothing is cached. Not enforced here — the threshold is per-model
#     and undocumented — but it is why a short prompt showing a zero hit
#     rate is not necessarily a bug.


def _classify(err: Exception, provider_id: str) -> ProviderError:
    """Wire failure to a decision about whether to try again.

    The same mapping the OpenAI adapter makes, against a different
    exception hierarchy. `retryable` decides whether the registry walks
    to the next provider, and it is expensive in both directions: a blip
    marked permanent becomes a user-visible outage, and a malformed
    request marked retryable is sent to every provider in the chain.
    """
    if isinstance(err, anthropic.APIConnectionError | anthropic.APITimeoutError):
        # The exception TYPE, not the exception. A connection error's
        # `str()` can carry the request URL and, on some transports, the
        # headers that went with it — which is where the key lives. The
        # status branch below already built its message from the code
        # alone; these two did not, and the parity leak test only drove
        # the status branch, so they were unguarded.
        return ProviderError(
            f"{provider_id} unreachable: {type(err).__name__}",
            provider_id=provider_id,
            retryable=True,
        )

    if isinstance(err, anthropic.APIStatusError):
        status = err.status_code
        return ProviderError(
            f"{provider_id} returned {status}",
            provider_id=provider_id,
            # 429 and 5xx recover; a 400 will fail identically forever.
            # 401 in particular must NOT be retried: a bad key is not a
            # transient condition and retrying it three times across two
            # providers turns one misconfiguration into six calls and a
            # log that blames the wrong provider.
            retryable=status == 429 or status >= 500,
            status_code=status,
        )

    return ProviderError(
        f"{provider_id} failed: {type(err).__name__}", provider_id=provider_id, retryable=True
    )


def _finish_reason(stop_reason: str | None) -> FinishReason:
    """Anthropic's `stop_reason`, mapped onto ours.

    `refusal` survives as its own value rather than collapsing into
    `error`. The spec is explicit: surface it as "let's approach this
    differently", never as a failure. A refusal retried at every provider
    in the chain wastes three calls and then shows an error to a user
    whose question the product has a written answer for.

    `pause_turn` is Anthropic's signal for a long-running server tool
    turn. This service sends no tools, so seeing one means the request
    was not the one we built — treated as `stop` so the partial text is
    still returned rather than discarded.
    """
    match stop_reason:
        case "end_turn" | "stop_sequence" | None:
            return "stop"
        case "max_tokens" | "model_context_window_exceeded":
            return "length"
        case "refusal":
            return "refusal"
        case _:
            return "stop"


class AnthropicProvider:
    """Claude, with the cache breakpoint where it belongs."""

    def __init__(
        self,
        *,
        api_key: str,
        models: ModelMap,
        tier: ProviderTier = "paid",
        provider_id: str = "anthropic",
        timeout_seconds: float = 120.0,
        base_url: str | None = None,
        capabilities: Capabilities | None = None,
    ) -> None:
        if not api_key:
            # Constructed rather than discovered on the first request. A
            # provider with no key is a configuration error, and finding
            # it at startup costs a restart while finding it at 3am costs
            # a chain that silently fell back to a free tier.
            raise ProviderError(
                "AnthropicProvider needs an API key. It is the production provider; "
                "a missing key here means the chain quietly serves from a fallback "
                "and nobody notices until the quality does.",
                provider_id=provider_id,
                retryable=False,
            )

        self._client = AsyncAnthropic(
            api_key=api_key,
            # `or None` rather than a conditional splat: **kwargs is
            # opaque to mypy, so building the call dynamically would
            # silently give up type checking on every other argument to
            # a constructor where a wrong one is a production outage.
            base_url=base_url or None,
            timeout=timeout_seconds,
            # Retries belong to `ResilientProvider`, which owns the
            # budget and the breaker. Leaving the SDK's own retries on
            # multiplies them: two SDK attempts inside three chain
            # attempts is six calls to a provider that is down, and six
            # times the wait before anything reaches the user.
            max_retries=0,
        )
        self._models = models
        self._tier: ProviderTier = tier
        self._id = provider_id
        self._capabilities = capabilities or Capabilities(
            streaming=True,
            json_mode=True,
            prompt_caching=True,
            vision=True,
            max_context_tokens=200_000,
        )

    @property
    def id(self) -> str:
        return self._id

    @property
    def tier(self) -> ProviderTier:
        return self._tier

    @property
    def capabilities(self) -> Capabilities:
        return self._capabilities

    # ─── the cache breakpoint ────────────────────────────────────────

    def _system(self, blocks: list[SystemBlock]) -> list[dict[str, Any]]:
        """System blocks, with one `cache_control` after the last stable one.

        The rule that matters: a breakpoint caches everything BEFORE it,
        so it goes after the final cacheable block and nowhere else. Mark
        every block and the four-breakpoint budget is spent on boundaries
        no request ever splits on; mark the last block overall and the
        user's chart lands inside the cached prefix, which changes the
        prefix on every request and makes the hit rate exactly zero.

        Blocks are kept separate rather than joined — unlike the OpenAI
        adapter, which has nowhere to attach cache control and so has
        nothing to preserve but order.
        """
        out: list[dict[str, Any]] = []
        last_cacheable = max(
            (index for index, block in enumerate(blocks) if block.cacheable),
            default=-1,
        )

        for index, block in enumerate(blocks):
            entry: dict[str, Any] = {"type": "text", "text": block.content}
            if index == last_cacheable:
                entry["cache_control"] = {"type": "ephemeral"}
            out.append(entry)

        return out

    def _kwargs(self, req: CompletionRequest) -> dict[str, Any]:
        kwargs: dict[str, Any] = {
            "model": self._models.for_tier(req.tier),
            "max_tokens": req.max_tokens,
            "messages": [{"role": m.role, "content": m.content} for m in req.messages],
            "output_config": {"effort": _EFFORT_BY_TIER[req.tier]},
        }

        if req.system:
            kwargs["system"] = self._system(req.system)

        if req.stop_sequences:
            kwargs["stop_sequences"] = req.stop_sequences

        if req.json_schema is not None:
            # A real schema, not a "please reply in JSON" instruction.
            # The model is constrained by the sampler, so a malformed
            # object is not a thing the parser downstream has to handle.
            kwargs["output_config"]["format"] = {
                "type": "json_schema",
                "schema": req.json_schema,
            }

        # `req.temperature` is deliberately NOT forwarded: `messages.create`
        # has no such parameter in anthropic 1.7.0. Effort replaced it —
        # the overload list is the authority, and mypy --strict reports
        # the attempt, so this cannot silently rot back in.
        #
        # It IS a behavioural difference between adapters: the same
        # request is sampled differently here than on Ollama, where
        # temperature is honoured. Recorded rather than hidden, because a
        # parity test comparing free and paid output would otherwise
        # attribute the difference to the model.
        return kwargs

    def _usage(self, raw: Any) -> Usage:
        """The three input classes, kept disjoint.

        Anthropic reports fresh, cache-write and cache-read separately
        and they do not overlap. Folding them together would be the
        expensive kind of wrong: cache reads cost about a tenth of fresh
        input and cache writes about 1.25x, so a single `input_tokens`
        number cannot be priced at all.
        """
        if raw is None:
            return Usage()
        return Usage(
            input_tokens=getattr(raw, "input_tokens", 0) or 0,
            output_tokens=getattr(raw, "output_tokens", 0) or 0,
            cached_input_tokens=getattr(raw, "cache_read_input_tokens", 0) or 0,
            cache_write_input_tokens=getattr(raw, "cache_creation_input_tokens", 0) or 0,
            # Filled by app/pricing.py, which owns the rate table. An
            # adapter hardcoding a price is a bill that goes wrong on the
            # day a vendor updates a page and nothing in the repo changes.
            cost_micros=0,
        )

    def _text(self, content: Any) -> str:
        """Concatenate the text blocks, skipping thinking blocks.

        With `thinking` on, the first blocks are the model's reasoning.
        Reading `content[0].text` — the obvious line — returns the
        thinking to the user, which is both wrong and a disclosure.
        """
        parts: list[str] = []
        for block in content or []:
            if getattr(block, "type", None) == "text":
                parts.append(getattr(block, "text", "") or "")
        return "".join(parts)

    # ─── the protocol ────────────────────────────────────────────────

    async def complete(self, req: CompletionRequest) -> CompletionResponse:
        started = time.monotonic()
        try:
            raw = await self._client.messages.create(**self._kwargs(req))
        except anthropic.AnthropicError as err:
            raise _classify(err, self._id) from err

        finish = _finish_reason(getattr(raw, "stop_reason", None))

        return CompletionResponse(
            # Read unconditionally, including on a refusal: Anthropic
            # sends explanatory text with one, and discarding it would
            # leave the safety path with nothing to show but a code.
            text=self._text(getattr(raw, "content", None)),
            finish_reason=finish,
            usage=self._usage(getattr(raw, "usage", None)),
            model=getattr(raw, "model", None) or self._models.for_tier(req.tier),
            provider_id=self._id,
            latency_ms=int((time.monotonic() - started) * 1000),
        )

    async def stream(self, req: CompletionRequest) -> AsyncIterator[CompletionChunk]:
        """Text deltas only, with usage assembled across the events.

        Anthropic splits usage over two events: input counts arrive on
        `message_start` and output counts on `message_delta`. Reading
        only the last one loses every input and cache number, so cost for
        a streamed response would be recorded as output-only — which for
        this product, where the input prefix dwarfs the reply, is most of
        the bill.
        """
        try:
            stream = await self._client.messages.create(**self._kwargs(req), stream=True)
        except anthropic.AnthropicError as err:
            raise _classify(err, self._id) from err

        usage = Usage()
        try:
            async for event in stream:
                kind = getattr(event, "type", "")

                if kind == "message_start":
                    usage = self._usage(getattr(getattr(event, "message", None), "usage", None))

                elif kind == "content_block_delta":
                    delta = getattr(event, "delta", None)
                    # `thinking_delta` events arrive here too and must not
                    # be forwarded — see `_text`.
                    if getattr(delta, "type", None) == "text_delta":
                        yield CompletionChunk(text=getattr(delta, "text", "") or "")

                elif kind == "message_delta":
                    tail = self._usage(getattr(event, "usage", None))
                    usage = usage.model_copy(update={"output_tokens": tail.output_tokens})
                    yield CompletionChunk(
                        text="",
                        finish_reason=_finish_reason(
                            getattr(getattr(event, "delta", None), "stop_reason", None)
                        ),
                        usage=usage,
                    )
        except anthropic.AnthropicError as err:
            raise _classify(err, self._id) from err

    async def health_check(self) -> bool:
        """Never raises; a provider that is down is a `False`.

        A one-token generation rather than a model list: Anthropic's
        list endpoint answers from an edge that stays up when inference
        does not, so it reports healthy during exactly the outage this
        check exists to notice. One token costs a few micro-cents.
        """
        try:
            await self._client.messages.create(
                model=self._models.fast,
                max_tokens=1,
                messages=[{"role": "user", "content": "ping"}],
            )
        except Exception:
            return False
        return True
