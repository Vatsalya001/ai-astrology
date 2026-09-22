"""Gemini — the most generous free hosted tier, and free embeddings.

PHASE-04 §2 wants this one for two jobs: a fallback when the dev machine
is loaded and Ollama is taking twenty seconds, and CI smoke tests that
need a real hosted model without a bill.

── Why it is not the OpenAI-compatible adapter pointed elsewhere ──

Google does serve an OpenAI-compatible endpoint, and using it would have
been one config line. It loses the two things this provider is here for:
free embeddings (a different endpoint entirely) and the safety-block
signal, which the compatibility layer flattens into a generic empty
response. An empty response indistinguishable from a refusal is exactly
the failure the `refusal`/`error` split exists to prevent.

── The vocabulary differences that actually bite ──

  - The assistant role is called `model`. Sending `assistant` is a 400.
  - The system prompt is `system_instruction` on the config, not a
    message, so there is no place to put a per-block cache marker.
  - `finish_reason` carries the safety verdict, and a blocked response
    arrives as HTTP 200 with no candidates at all.
"""

from __future__ import annotations

import time
from collections.abc import AsyncIterator
from typing import Any

from google import genai
from google.genai import errors as genai_errors
from google.genai import types as genai_types

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
from app.providers.resilience import (
    BEFORE_ANY_BYTE_WAS_SENT as _BEFORE_ANY_BYTE_WAS_SENT,
)
from app.providers.resilience import (
    SENT_THEN_LOST as _SENT_THEN_LOST,
)
from app.providers.resilience import (
    TIMEOUT_PHASES as _TIMEOUT_PHASES,
)
from app.providers.resilience import (
    UnsafeToReplayError,
    is_retryable_status,
)

# Every reason Google stops for something other than finishing. Grouped
# by what the caller must DO, which is the only distinction that matters
# once the response is gone.
_REFUSAL_REASONS = frozenset(
    {
        "SAFETY",
        "PROHIBITED_CONTENT",
        "BLOCKLIST",
        "SPII",
        # Recitation — the model was reproducing training data verbatim.
        # A refusal rather than an error: retrying produces the same
        # recitation, and the product has a graceful answer for it.
        "RECITATION",
        "IMAGE_SAFETY",
        "IMAGE_PROHIBITED_CONTENT",
    }
)


def _classify(err: Exception, provider_id: str) -> ProviderError:
    """Wire failure to a retry decision, without echoing the response body.

    `str(genai.errors.APIError)` interpolates the whole response JSON,
    which is a request echo on some error paths. `.claude/rules/security.md`
    is unambiguous that a key must never reach an error message, and a
    message built from the status code alone cannot leak one by accident.
    """
    if isinstance(err, genai_errors.APIError):
        code = err.code or 0
        return ProviderError(
            f"{provider_id} returned {code}",
            provider_id=provider_id,
            # The shared table, not a local copy: the registry's fallback
            # loop reads this flag and nothing else, so an adapter with
            # its own opinion about 529 breaks failover for the whole
            # chain. See `resilience.is_retryable_status`.
            retryable=is_retryable_status(code),
            status_code=code or None,
        )

    # Connection failures surface as bare httpx/transport errors here —
    # the SDK does not wrap them.
    #
    # Matched by class NAME rather than by isinstance, for the reason
    # written out at length in openai_compatible.py: this environment has
    # two httpx distributions and `google.genai._api_client` imports BOTH
    # `httpx` and `httpx2`. An isinstance check binds to whichever one
    # this module happened to import, so a `httpx2.ReadTimeout` would
    # miss the branch entirely and fall through to the plain-retryable
    # case below — which is the expensive direction: a completion that
    # may already have been generated and billed, sent again.
    phase = type(err).__name__

    if phase in _TIMEOUT_PHASES:
        if phase in _BEFORE_ANY_BYTE_WAS_SENT:
            # No request left this process, so nothing was generated and
            # nothing was billed. Safe to send again.
            return ProviderError(
                f"{provider_id} unreachable: timed out before the request was sent",
                provider_id=provider_id,
                retryable=True,
            )
        # A read timeout means the bytes went out and only the answer was
        # lost. The completion may have run to its last token and been
        # charged in full. Fails closed for any timeout phase we cannot
        # name, because the expensive mistake is assuming a call that was
        # billed did not happen.
        return UnsafeToReplayError(
            f"{provider_id} timed out after the request was sent",
            provider_id=provider_id,
        )

    if phase in _SENT_THEN_LOST:
        # The same mid-run death the OpenAI adapter classifies: the
        # request was read in full and the connection then broke, so the
        # answer is lost but the tokens may not be.
        return UnsafeToReplayError(
            f"{provider_id} lost the connection after the request was sent",
            provider_id=provider_id,
        )

    # Everything else: connection refused, DNS, TLS. Retryable for the
    # same reason a dead Ollama is — nothing about the request is wrong,
    # and it never reached anything that could bill for it.
    return ProviderError(
        f"{provider_id} unreachable: {type(err).__name__}",
        provider_id=provider_id,
        retryable=True,
    )


def _finish_reason(raw: Any) -> FinishReason:
    if raw is None:
        return "stop"

    name = getattr(raw, "name", None) or str(raw)

    if name in _REFUSAL_REASONS:
        return "refusal"
    if name == "MAX_TOKENS":
        return "length"
    if name in {"STOP", "FINISH_REASON_UNSPECIFIED"}:
        return "stop"

    # MALFORMED_FUNCTION_CALL, UNEXPECTED_TOOL_CALL and friends. This
    # service sends no tools, so one of these means the request was not
    # the one we built — an error, because it is a bug rather than an
    # outcome, and it should not be quietly rendered to a user.
    return "error"


class GoogleProvider:
    """Gemini for completions."""

    def __init__(
        self,
        *,
        api_key: str,
        models: ModelMap,
        tier: ProviderTier = "free-hosted",
        provider_id: str = "google",
        timeout_seconds: float = 60.0,
        capabilities: Capabilities | None = None,
    ) -> None:
        self._client = genai.Client(
            api_key=api_key,
            http_options=genai_types.HttpOptions(
                # Milliseconds here, unlike every other SDK in this
                # package. Passing seconds gives a 60 ms timeout, which
                # fails on the first token and looks exactly like a
                # network problem.
                timeout=int(timeout_seconds * 1000),
            ),
        )
        self._models = models
        self._tier: ProviderTier = tier
        self._id = provider_id
        self._capabilities = capabilities or Capabilities(
            streaming=True,
            json_mode=True,
            # Gemini has implicit caching, which needs no breakpoint and
            # cannot be placed. Declared False because this flag means
            # "supports an explicit breakpoint" — the thing the prompt
            # builder can act on.
            prompt_caching=False,
            vision=True,
            max_context_tokens=1_000_000,
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

    def _contents(self, req: CompletionRequest) -> list[dict[str, Any]]:
        """Messages, with `assistant` renamed to `model`.

        Google's only two roles are `user` and `model`. A `system` role
        in the message list is not supported at all, which is why any
        system content in `messages` is folded onto the user turn rather
        than dropped — dropping it would silently discard an instruction.
        """
        out: list[dict[str, Any]] = []
        for message in req.messages:
            role = "model" if message.role == "assistant" else "user"
            out.append({"role": role, "parts": [{"text": message.content}]})
        return out

    def _config(self, req: CompletionRequest) -> genai_types.GenerateContentConfig:
        config = genai_types.GenerateContentConfig(
            max_output_tokens=req.max_tokens,
            temperature=req.temperature,
            stop_sequences=req.stop_sequences or None,
            # Joined, because there is one slot. Order is still what
            # makes the prefix stable for implicit caching, so the blocks
            # are concatenated in the sequence the builder produced them.
            system_instruction=(
                "\n\n".join(block.content for block in req.system) if req.system else None
            ),
            # This service sends no tools, and leaving AFC on makes the
            # SDK warn on every single call. A warning that fires on the
            # happy path is a warning nobody reads, which is how the one
            # that matters gets missed.
            automatic_function_calling=genai_types.AutomaticFunctionCallingConfig(disable=True),
        )

        if req.json_schema is not None:
            config.response_mime_type = "application/json"
            config.response_json_schema = req.json_schema

        return config

    def _usage(self, raw: Any) -> Usage:
        if raw is None:
            return Usage()

        prompt = getattr(raw, "prompt_token_count", 0) or 0
        cached = getattr(raw, "cached_content_token_count", 0) or 0

        return Usage(
            # Google reports `prompt_token_count` INCLUSIVE of cached
            # tokens, unlike vendors that report them disjointly.
            # Subtracting keeps the
            # fields disjoint as `Usage` documents them; without it, a
            # cached request is priced as though the prefix were fresh —
            # which is the most expensive direction to be wrong in.
            input_tokens=max(prompt - cached, 0),
            # `candidates_token_count` + `thoughts_token_count`.
            #
            # Gemini 3.x reasons before answering and reports the two
            # separately, but BILLS BOTH AS OUTPUT. Reading only
            # `candidates` understates the cost of a thinking model by
            # whatever ratio it happens to reason at — measured on
            # gemini-3.6-flash with a two-sentence question:
            #
            #   promptTokenCount      11
            #   candidatesTokenCount  11     <- what we used to record
            #   thoughtsTokenCount   463
            #   totalTokenCount      485
            #
            # A 40x understatement, on the field the whole cost dashboard
            # is built from. Nothing offline could catch it: our fixtures
            # describe a response shape we wrote down, and we did not
            # know this field existed until a real key returned one.
            output_tokens=(
                (getattr(raw, "candidates_token_count", 0) or 0)
                + (getattr(raw, "thoughts_token_count", 0) or 0)
            ),
            cached_input_tokens=cached,
            cost_micros=0,
        )

    def _text(self, response: Any) -> str:
        """Every text part joined, without going through `.text`.

        The SDK's `.text` property raises when a response has no
        candidates — which is precisely what a safety block looks like —
        so the convenient accessor fails on the one path that most needs
        to return something.
        """
        parts: list[str] = []
        for candidate in getattr(response, "candidates", None) or []:
            content = getattr(candidate, "content", None)
            for part in getattr(content, "parts", None) or []:
                # A part Gemini marks `thought` is the model REASONING,
                # not its answer. This adapter never asks for thoughts to
                # be included, so in principle none arrive — but one real
                # run returned text beginning "**Check against
                # constraints:** Option A Sentence 1: ..." as the answer,
                # which is reasoning, and the behaviour did not reproduce
                # on demand afterwards.
                #
                # So this is a DEFENCE, not a fix for a confirmed repro,
                # and it is worth having either way. Reasoning reaching
                # `text` is not merely an ugly answer here: §7's output
                # validator judges this string, and a draft the model was
                # arguing with itself about is exactly the kind of text
                # that contains a claim it had not yet rejected.
                if getattr(part, "thought", None):
                    continue
                text = getattr(part, "text", None)
                if text:
                    parts.append(text)
        return "".join(parts)

    def _finish(self, response: Any) -> FinishReason:
        """A prompt-level block first, then the candidate's own reason.

        A blocked PROMPT returns HTTP 200 with no candidates and the
        reason on `prompt_feedback`. Reading only the candidate gives
        `stop` with empty text, which is indistinguishable from a model
        that had nothing to say — and would send an empty bubble to a
        user instead of the safety response.
        """
        feedback = getattr(response, "prompt_feedback", None)
        if feedback is not None and getattr(feedback, "block_reason", None):
            return "refusal"

        candidates = getattr(response, "candidates", None) or []
        if not candidates:
            return "refusal"

        return _finish_reason(getattr(candidates[0], "finish_reason", None))

    # ─── the protocol ────────────────────────────────────────────────

    async def complete(self, req: CompletionRequest) -> CompletionResponse:
        started = time.monotonic()
        model = self._models.for_tier(req.tier)

        try:
            raw = await self._client.aio.models.generate_content(
                model=model,
                contents=self._contents(req),
                config=self._config(req),
            )
        except Exception as err:
            raise _classify(err, self._id) from err

        return CompletionResponse(
            text=self._text(raw),
            finish_reason=self._finish(raw),
            usage=self._usage(getattr(raw, "usage_metadata", None)),
            # Google does not echo the served model, so the requested one
            # is the only name available. Recorded rather than left blank
            # so telemetry has the same shape from every provider.
            model=getattr(raw, "model_version", None) or model,
            provider_id=self._id,
            latency_ms=int((time.monotonic() - started) * 1000),
        )

    async def stream(self, req: CompletionRequest) -> AsyncIterator[CompletionChunk]:
        """Usage arrives cumulatively, so the last chunk carries the total.

        Google repeats `usage_metadata` on every chunk with running
        totals rather than deltas. Summing them would multiply the bill
        by the number of chunks, so each one replaces the last and only
        the final value is emitted.
        """
        model = self._models.for_tier(req.tier)

        try:
            stream = await self._client.aio.models.generate_content_stream(
                model=model,
                contents=self._contents(req),
                config=self._config(req),
            )
        except Exception as err:
            raise _classify(err, self._id) from err

        usage = Usage()
        finish: FinishReason = "stop"

        try:
            async for event in stream:
                # Guarded on the METADATA, not on the parsed `Usage`.
                # `self._usage(None)` returns a zero-filled pydantic model
                # and a pydantic model is always truthy, so the obvious
                # `self._usage(...) or usage` never falls back — and a
                # final frame without `usage_metadata`, which Google omits
                # on some paths, silently wiped the running total to zero
                # and recorded a paid generation as free.
                metadata = getattr(event, "usage_metadata", None)
                if metadata is not None:
                    usage = self._usage(metadata)

                text = self._text(event)
                candidates = getattr(event, "candidates", None) or []
                if candidates and getattr(candidates[0], "finish_reason", None):
                    finish = self._finish(event)
                elif getattr(getattr(event, "prompt_feedback", None), "block_reason", None):
                    # A PROMPT-level block: HTTP 200, an EMPTY candidates
                    # list, and the reason in `promptFeedback`. The guard
                    # above is false, so `finish` stayed at its "stop"
                    # initialiser and the caller could not tell a refused
                    # prompt from a model with nothing to say — an empty
                    # bubble streamed to the user instead of the safety
                    # response.
                    #
                    # `complete()` never had this: it goes through
                    # `_finish`, which reads promptFeedback. Only the
                    # streaming path was missing it.
                    finish = "refusal"
                if text:
                    yield CompletionChunk(text=text)
        except Exception as err:
            raise _classify(err, self._id) from err

        yield CompletionChunk(text="", finish_reason=finish, usage=usage)

    async def health_check(self) -> bool:
        try:
            await self._client.aio.models.get(model=self._models.fast)
        except Exception:
            return False
        return True


class GoogleEmbeddingProvider:
    """Free embeddings, as an alternative to a local Ollama.

    Kept a separate class rather than more methods on `GoogleProvider`
    because `EmbeddingProvider` is a separate protocol with a
    `dimensions` property, and one object satisfying both would be
    registrable in the wrong registry — a mistake nothing would catch
    until a similarity search returned noise.
    """

    def __init__(
        self,
        *,
        api_key: str,
        model: str = "gemini-embedding-001",
        dimensions: int = 768,
        provider_id: str = "google-embeddings",
        tier: ProviderTier = "free-hosted",
        timeout_seconds: float = 30.0,
    ) -> None:
        self._client = genai.Client(
            api_key=api_key,
            http_options=genai_types.HttpOptions(timeout=int(timeout_seconds * 1000)),
        )
        self._model = model
        self._dimensions = dimensions
        self._id = provider_id
        self._tier: ProviderTier = tier

    @property
    def id(self) -> str:
        return self._id

    @property
    def tier(self) -> ProviderTier:
        return self._tier

    @property
    def dimensions(self) -> int:
        return self._dimensions

    async def embed(self, texts: list[str]) -> list[list[float]]:
        if not texts:
            # An empty batch is a caller bug, not a request to make. The
            # API bills a round trip for it and returns nothing useful.
            return []

        try:
            raw = await self._client.aio.models.embed_content(
                model=self._model,
                contents=texts,
                config=genai_types.EmbedContentConfig(
                    # Gemini's embedding model is Matryoshka: one model
                    # serves several widths, and the width is chosen per
                    # request. Sent explicitly so it matches
                    # EMBEDDING_DIM rather than whatever the default
                    # happens to be this month — a pgvector column is
                    # fixed-width, and a mismatch means re-embedding the
                    # whole corpus.
                    output_dimensionality=self._dimensions,
                ),
            )
        except Exception as err:
            raise _classify(err, self._id) from err

        vectors = [list(e.values or []) for e in (raw.embeddings or [])]

        if len(vectors) != len(texts):
            # Silent truncation would misalign every vector with its
            # text from that point on, and the corpus would look
            # plausible while answering with the wrong documents.
            raise ProviderError(
                f"{self._id} returned {len(vectors)} vectors for {len(texts)} texts",
                provider_id=self._id,
                retryable=False,
            )

        for index, vector in enumerate(vectors):
            if len(vector) != self._dimensions:
                raise ProviderError(
                    f"{self._id} returned a {len(vector)}-dim vector at position "
                    f"{index}, expected {self._dimensions}. A wrong-width vector "
                    f"either fails on insert or lands in a column that accepts it "
                    f"and makes every similarity score meaningless.",
                    provider_id=self._id,
                    retryable=False,
                )

        return vectors

    async def health_check(self) -> bool:
        try:
            await self.embed(["ping"])
        except Exception:
            return False
        return True
