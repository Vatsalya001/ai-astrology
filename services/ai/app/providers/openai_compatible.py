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

# ─── error classification ────────────────────────────────────────────
#
# The single most consequential mapping in this file. `retryable` decides
# whether the registry walks to the next provider, and getting it wrong
# is expensive in both directions: a retryable error marked permanent
# turns a blip into an outage, and a permanent error marked retryable
# sends the same malformed request to every provider in the chain.

_RETRYABLE_STATUS = frozenset({408, 409, 425, 429, 500, 502, 503, 504})


def _classify(err: openai.APIError, provider_id: str) -> ProviderError:
    status = getattr(err, "status_code", None)

    if isinstance(err, openai.APIConnectionError | openai.APITimeoutError):
        # The backend is not answering. Nothing about the request is
        # wrong, so another provider is very likely to succeed — this is
        # precisely the case fallback exists for. Ollama not running on a
        # developer's laptop arrives here.
        return ProviderError(
            f"{provider_id} unreachable: {err}",
            provider_id=provider_id,
            retryable=True,
        )

    if isinstance(status, int):
        return ProviderError(
            f"{provider_id} returned {status}: {err}",
            provider_id=provider_id,
            retryable=status in _RETRYABLE_STATUS,
            status_code=status,
        )

    # Unknown shape. Treated as retryable on the same reasoning the
    # registry uses: an unmapped error should degrade one provider rather
    # than fail the request outright.
    return ProviderError(f"{provider_id} failed: {err}", provider_id=provider_id, retryable=True)


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
        byte-stable prefix. The Anthropic adapter, which does have
        breakpoints, keeps them separate.
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
        try:
            stream = await self._client.chat.completions.create(**self._kwargs(req), stream=True)
        except openai.APIError as err:
            raise _classify(err, self._id) from err

        try:
            async for event in stream:
                if not event.choices:
                    continue
                delta = event.choices[0].delta
                yield CompletionChunk(
                    text=delta.content or "",
                    finish_reason=(
                        _finish_reason(event.choices[0].finish_reason)
                        if event.choices[0].finish_reason
                        else None
                    ),
                )
        except openai.APIError as err:
            # A stream can fail after its first chunk. Classified the
            # same way so a caller sees one error type whether the
            # failure happened at connect time or halfway through.
            raise _classify(err, self._id) from err

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
