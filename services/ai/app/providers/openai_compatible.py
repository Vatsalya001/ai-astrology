"""One adapter, five backends.

Ollama, LM Studio, Groq, OpenRouter and Cerebras all serve the OpenAI
wire format, so the official `openai` SDK talks to every one of them and
the only difference is a base URL and a tier. The spec calls this the
highest-leverage code in the phase and it is right: five integrations
for the cost of one, and the dev/prod split becomes configuration.

── This is the file that may import a vendor SDK ──

It and its siblings in this package, and nothing else. `.importlinter`
enforces that; see `pyproject.toml`.
"""

from __future__ import annotations

import time
from collections.abc import AsyncIterator
from typing import Any

import httpx
import openai
from openai import AsyncOpenAI

from app.providers.base import (
    Capabilities,
    CompletionChunk,
    CompletionRequest,
    CompletionResponse,
    FinishReason,
    ModelMap,
    ProviderError,
    ProviderTier,
    Usage,
)
from app.providers.resilience import UnsafeToReplayError, is_retryable_status

# ─── error classification ────────────────────────────────────────────
#
# The single most consequential mapping in this file. `retryable` decides
# whether the registry walks to the next provider, and getting it wrong
# is expensive in both directions: a retryable error marked permanent
# turns a blip into an outage, and a permanent error marked retryable
# sends the same malformed request to every provider in the chain.
#
# The table itself lives in `resilience.py` so all the adapters share one
# — this file used to keep a private 5xx enumeration that omitted 501,
# 520, 522, 524 and 529, and marked every one of them permanent while
# other adapters called the same code retryable.


def _timeout_reached_the_provider(err: Exception) -> bool:
    """Did the request get far enough that it may have been billed?

    `httpx` names the phase in the exception type and the OpenAI SDK
    keeps the original on `__cause__`. A connect or pool timeout means no
    request ever left this process, so nothing could have been generated
    or charged. Anything else — a read timeout above all — means the
    bytes went out and only the answer was lost.

    Unknown causes fail CLOSED, i.e. "it may have been billed". The
    expensive mistake here is assuming a call did not happen.
    """
    return not isinstance(err.__cause__, httpx.ConnectTimeout | httpx.PoolTimeout)


def _classify(err: openai.APIError, provider_id: str) -> ProviderError:
    status = getattr(err, "status_code", None)

    # Before the broader APIConnectionError branch: APITimeoutError is a
    # SUBCLASS of it, so testing the parent first would swallow every
    # timeout and retry the billable ones.
    if isinstance(err, openai.APITimeoutError):
        if _timeout_reached_the_provider(err):
            return UnsafeToReplayError(
                f"{provider_id} timed out after the request was sent",
                provider_id=provider_id,
            )
        return ProviderError(
            f"{provider_id} unreachable: timed out before the request was sent",
            provider_id=provider_id,
            retryable=True,
        )

    if isinstance(err, openai.APIConnectionError):
        # The backend is not answering and never accepted the request.
        # Nothing about it is wrong, so another provider is very likely
        # to succeed — precisely the case fallback exists for. Ollama not
        # running on a developer's laptop arrives here.
        return ProviderError(
            f"{provider_id} unreachable: {type(err).__name__}",
            provider_id=provider_id,
            retryable=True,
        )

    if isinstance(status, int):
        # `{err}` is deliberately NOT interpolated. `str(APIStatusError)`
        # is built as "Error code: 401 - {body}" — the WHOLE response
        # body, which on an auth failure is where a provider echoes the
        # key it rejected. `.claude/rules/security.md`: a credential never
        # reaches an error message. Building from the status code alone
        # makes that true by construction rather than by review.
        return ProviderError(
            f"{provider_id} returned {status}",
            provider_id=provider_id,
            retryable=is_retryable_status(status),
            status_code=status,
        )

    # Unknown shape. Treated as retryable on the same reasoning the
    # registry uses: an unmapped error should degrade one provider rather
    # than fail the request outright. The type name, not the message —
    # the message is where a body ends up.
    return ProviderError(
        f"{provider_id} failed: {type(err).__name__}",
        provider_id=provider_id,
        retryable=True,
    )


def _finish_reason(raw: str | None) -> FinishReason:
    """Map the wire's reason onto ours.

    `content_filter` becomes `refusal` rather than `error` — it is the
    model declining, which is a product outcome with a written response,
    not something to retry. Collapsing it into `error` would make the
    chain retry a refusal at every provider and then surface a failure to
    a user who asked a question the product has a real answer for.
    """
    match raw:
        case "stop" | None:
            return "stop"
        case "length":
            return "length"
        case "content_filter":
            return "refusal"
        case _:
            return "stop"


class OpenAICompatibleProvider:
    """Any backend speaking the OpenAI wire format."""

    def __init__(
        self,
        *,
        base_url: str,
        api_key: str,
        tier: ProviderTier,
        models: ModelMap,
        provider_id: str = "openai-compatible",
        timeout_seconds: float = 60.0,
        capabilities: Capabilities | None = None,
    ) -> None:
        self._client = AsyncOpenAI(
            base_url=base_url,
            # Ollama and LM Studio need no key and reject an empty string
            # rather than ignoring it, so a placeholder is sent. The SDK
            # requires *something*; the local server never looks at it.
            api_key=api_key or "not-needed",
            timeout=timeout_seconds,
            # Retries are the registry's job. Leaving the SDK's own
            # retries on would multiply: three SDK attempts inside three
            # chain attempts is nine calls to a provider that is down,
            # and nine times the wait before the user sees anything.
            max_retries=0,
        )
        self._tier: ProviderTier = tier
        self._models = models
        self._id = provider_id
        self._capabilities = capabilities or Capabilities(
            streaming=True,
            json_mode=True,
            prompt_caching=False,
            max_context_tokens=32_768,
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

    # ─── request assembly ────────────────────────────────────────────

    def _messages(self, req: CompletionRequest) -> list[dict[str, str]]:
        """System blocks first, in order, then the conversation.

        The blocks are joined with a blank line rather than sent
        separately: this wire format has no per-block cache control, so
        the only thing that survives is ORDER, and order is what makes a
        byte-stable prefix. An adapter with real per-block cache
        control would keep them separate.
        """
        out: list[dict[str, str]] = []

        if req.system:
            out.append({"role": "system", "content": "\n\n".join(b.content for b in req.system)})

        out.extend({"role": m.role, "content": m.content} for m in req.messages)
        return out

    def _kwargs(self, req: CompletionRequest) -> dict[str, Any]:
        kwargs: dict[str, Any] = {
            "model": self._models.for_tier(req.tier),
            "messages": self._messages(req),
            "max_tokens": req.max_tokens,
            "temperature": req.temperature,
        }

        if req.stop_sequences:
            kwargs["stop"] = req.stop_sequences

        if req.json_schema is not None:
            if not self._capabilities.json_mode:
                # Refused at the edge rather than sent and hoped for. A
                # backend without JSON mode returns prose, which fails to
                # parse three layers away from the cause.
                raise ProviderError(
                    f"{self._id} cannot produce structured output, but a json_schema was requested",
                    provider_id=self._id,
                    retryable=False,
                )
            # The OpenAI `json_object` contract has a requirement that is
            # easy to miss and fails LOUDLY at the vendor rather than
            # here: the messages must themselves mention JSON. Groq
            # returns
            #
            #   400 'messages' must contain the word 'json' in some form,
            #       to use 'response_format' of type 'json_object'
            #
            # and OpenAI enforces the same rule. Ollama does not, so a
            # setup that works locally 400s the moment it points at a
            # hosted backend.
            #
            # Today every prompt satisfies this by luck —
            # `intent_classification.v4` opens its output section with "A
            # single JSON object with exactly these keys". Nothing
            # required that, and dropping the word while rewording a
            # prompt would break structured output at runtime with an
            # error naming neither the prompt nor the word.
            #
            # Refused here, in the same spirit as the capability check
            # above: a vendor 400 three layers away is the expensive way
            # to learn this. NOT fixed by injecting the word ourselves —
            # appending to the system prompt would change the cacheable
            # prefix, and a byte change early in the prefix invalidates
            # the whole cache downstream (§2).
            if not any("json" in m["content"].lower() for m in kwargs["messages"]):
                raise ProviderError(
                    f"{self._id} was asked for structured output, but no message "
                    f"mentions JSON. OpenAI-compatible backends reject "
                    f"response_format=json_object unless the word appears in the "
                    f"prompt. Add it to the prompt module rather than here — "
                    f"injecting it would change the cacheable prefix.",
                    provider_id=self._id,
                    retryable=False,
                )
            kwargs["response_format"] = {"type": "json_object"}

        return kwargs

    def _usage(self, raw: Any) -> Usage:
        """Whatever the backend reported, or zeros.

        Ollama omits usage entirely on some versions. Zeros are correct
        there — nothing was billed — and are safe to sum, which a `None`
        would not be.
        """
        if raw is None:
            return Usage()
        return Usage(
            input_tokens=getattr(raw, "prompt_tokens", 0) or 0,
            output_tokens=getattr(raw, "completion_tokens", 0) or 0,
            # Cost is not computed here. Price per model is a routing
            # concern that changes without any code change, and an
            # adapter hardcoding a rate is a bill that silently goes
            # wrong the day a price does.
            cost_micros=0,
        )

    # ─── the protocol ────────────────────────────────────────────────

    async def complete(self, req: CompletionRequest) -> CompletionResponse:
        started = time.monotonic()
        try:
            raw = await self._client.chat.completions.create(**self._kwargs(req))
        except openai.APIError as err:
            raise _classify(err, self._id) from err

        elapsed_ms = int((time.monotonic() - started) * 1000)

        choice = raw.choices[0] if raw.choices else None
        return CompletionResponse(
            text=(choice.message.content if choice and choice.message.content else ""),
            finish_reason=_finish_reason(choice.finish_reason if choice else None),
            usage=self._usage(raw.usage),
            # The model the BACKEND says it used, falling back to what we
            # asked for. They differ on OpenRouter, which silently routes
            # to whatever is cheapest — and a log that records the
            # requested model there is a log that cannot explain the
            # answer.
            model=getattr(raw, "model", None) or self._models.for_tier(req.tier),
            provider_id=self._id,
            latency_ms=elapsed_ms,
        )

    async def stream(self, req: CompletionRequest) -> AsyncIterator[CompletionChunk]:
        """One final chunk carrying the finish reason and the usage.

        Both of those were missing. This wire format sends no usage on a
        stream unless asked, so every streamed response was recorded as
        free — silently, because an absent cost looks exactly like a
        cheap one in a telemetry row, and this is the only adapter it
        happened on.
        """
        try:
            stream = await self._client.chat.completions.create(
                **self._kwargs(req),
                stream=True,
                # The opt-in that makes a streamed call billable at all.
                # Only legal alongside `stream=True` — sending it on a
                # non-streamed request is a 400 — which is why it is here
                # rather than in `_kwargs`.
                stream_options={"include_usage": True},
            )
        except openai.APIError as err:
            raise _classify(err, self._id) from err

        usage: Usage | None = None
        finish: FinishReason | None = None

        try:
            async for event in stream:
                # Read BEFORE the `choices` guard below. The usage-bearing
                # frame arrives with an empty `choices` list, so the
                # obvious `if not event.choices: continue` skips exactly
                # the chunk this exists to read.
                reported = getattr(event, "usage", None)
                if reported is not None:
                    usage = self._usage(reported)

                if not event.choices:
                    continue

                choice = event.choices[0]
                if choice.finish_reason:
                    # Held back rather than forwarded here, so the reason
                    # and the usage land on the SAME final chunk that
                    # every other adapter puts them on. A consumer that
                    # stops reading at the first finish_reason would
                    # otherwise never see the cost.
                    finish = _finish_reason(choice.finish_reason)

                text = choice.delta.content or "" if choice.delta else ""
                if text:
                    yield CompletionChunk(text=text)
        except openai.APIError as err:
            # A stream can fail after its first chunk. Classified the
            # same way so a caller sees one error type whether the
            # failure happened at connect time or halfway through.
            raise _classify(err, self._id) from err

        # Zeros rather than nothing when the backend ignored
        # `stream_options` — some Ollama and LM Studio builds do. A
        # consumer reading cost off the last chunk then gets a number it
        # can add up on every provider, instead of a `None` that only
        # this one produces.
        yield CompletionChunk(text="", finish_reason=finish or "stop", usage=usage or Usage())

    async def health_check(self) -> bool:
        """Never raises. A provider that is down is a `False`.

        Listing models rather than generating: it is the cheapest call
        every one of these backends supports, costs no tokens, and needs
        no model to be pulled — which matters on a fresh Ollama install
        where generating would fail for a reason unrelated to liveness.
        """
        try:
            await self._client.models.list()
        except Exception:
            return False
        return True
