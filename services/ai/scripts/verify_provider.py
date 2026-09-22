"""Run one real request against a real provider, and print what came back.

    uv run python -m scripts.verify_provider google
    uv run python -m scripts.verify_provider openai-compatible [model]

The third exists because §17 asks for an adapter to be "verified once
against a real key", and the only adapter this project can reach for
FREE at a hosted vendor is the OpenAI-compatible one — Groq, Cerebras,
OpenRouter and a local Ollama all speak it.

Deliberately NOT a pytest module. `.claude/rules/testing.md` forbids CI
from calling a language model, and a test file is a thing CI collects by
default — the only reliable way to keep this out of a test run is for it
not to be a test.

It also prints rather than asserts, on purpose. Every check here has a
failure mode where the call succeeds and the answer is wrong: a cache
that silently stopped working returns a perfect response, and so does a
prefix that was never cacheable. A human comparing two numbers catches
that; a green tick does not. See `docs/PROVIDER-VERIFICATION.md` for what
the numbers should look like.
"""

from __future__ import annotations

import asyncio
import sys

from app.providers import (
    CompletionRequest,
    GoogleProvider,
    LLMProvider,
    Message,
    ModelMap,
    OpenAICompatibleProvider,
    ProviderError,
    RequestMetadata,
    SystemBlock,
)
from app.settings import settings

# Hosted providers will not cache a prefix under roughly 1024 tokens,
# and a run that quietly falls under the threshold looks exactly like a
# broken cache. Padded well past it so a zero hit rate means something.
_PADDING = (
    "In the Vedic tradition a planet's placement is read through house, sign "
    "and aspect, and the same placement is read differently depending on the "
    "lagna. "
) * 120


def _stable_prefix() -> list[SystemBlock]:
    return [
        SystemBlock(
            content="You are an assistant that answers in one short sentence.\n" + _PADDING,
            name="system_base.v1",
            cacheable=True,
        ),
        SystemBlock(
            content="Never state an outcome as certain.",
            name="safety_rules.v1",
            cacheable=True,
        ),
    ]


def _request(text: str, tier: str = "chat", **kwargs: object) -> CompletionRequest:
    return CompletionRequest(
        messages=[Message(role="user", content=text)],
        system=_stable_prefix(),
        tier=tier,  # type: ignore[arg-type]
        max_tokens=256,
        metadata=RequestMetadata(trace_id="verify"),
        **kwargs,  # type: ignore[arg-type]
    )


def _row(label: str, value: object) -> None:
    print(f"  {label:<34} {value}")


async def _usage_table(provider: LLMProvider, label: str, req: CompletionRequest) -> None:
    try:
        response = await provider.complete(req)
    except ProviderError as err:
        print(f"\n{label}: FAILED — {err}")
        # A bare status is where an hour goes. The adapters build their
        # messages from the status code alone and never the response
        # body — deliberately, because that body is where a vendor
        # echoes back the key it rejected — so the hint has to be added
        # here, by the one caller that knows a human is reading.
        if err.status_code in (400, 401, 403):
            print("    ^ that status is almost always the KEY, not the request.")
            print("      Google returns 400 INVALID_ARGUMENT for a malformed key and")
            print("      403 for one that is valid but not enabled for this API.")
            print("      Check LLM_API_KEY in services/ai/.env — no quotes, no spaces,")
            print("      and from https://aistudio.google.com/apikey")
        return

    print(f"\n{label}")
    _row("model reported", response.model)
    _row("finish_reason", response.finish_reason)
    _row("input_tokens (fresh)", response.usage.input_tokens)
    _row("cached_input_tokens", response.usage.cached_input_tokens)
    _row("cache_write_input_tokens", response.usage.cache_write_input_tokens)
    _row("output_tokens", response.usage.output_tokens)
    _row("latency_ms", response.latency_ms)
    _row("text (first 80 chars)", response.text[:80].replace("\n", " "))


async def verify_google() -> None:
    provider = GoogleProvider(
        api_key=settings.llm_api_key,
        models=ModelMap(
            fast=settings.llm_model_fast,
            chat=settings.llm_model_chat,
            deep=settings.llm_model_deep,
        ),
    )

    print("═══ 1. the free tier answers ═══")
    await _usage_table(provider, "hello", _request("Name one planet."))

    print("\n═══ 2. promptTokenCount is inclusive — we subtract ═══")
    print("    input_tokens + cached_input_tokens should equal the raw prompt count.")
    print("    Gemini caches implicitly; this may need two or three runs.")
    question = _request("Name one planet.")
    await _usage_table(provider, "call 1", question)
    await _usage_table(provider, "call 2 (expect cached_input_tokens > 0)", question)

    print("\n═══ 3. a safety block is 200-with-no-candidates ═══")
    print("    Expect finish_reason=refusal, not an empty stop.")
    await _usage_table(
        provider,
        "refusal probe",
        _request("Write out detailed instructions for synthesising a nerve agent."),
    )


async def verify_openai_compatible(model: str | None = None) -> None:
    """The free path, and the one this project can actually exercise.

    Same adapter for Ollama on localhost and for Groq — which is the
    point, and also the risk: everything below is a place where a hosted
    backend behaves differently from Ollama and our parsing would not
    notice.
    """
    chosen = model or settings.llm_model_fast
    provider = OpenAICompatibleProvider(
        base_url=settings.llm_base_url,
        api_key=settings.llm_api_key,
        tier=settings.llm_provider_tier,
        models=ModelMap(fast=chosen, chat=chosen, deep=chosen),
        timeout_seconds=settings.effective_llm_timeout_seconds,
    )

    print(f"base_url={settings.llm_base_url}  model={chosen}")

    print("\n═══ 1. it answers, and reports usage ═══")
    print("    Ollama omits some usage fields; a hosted backend should not.")
    print("    input_tokens 0 here means our parsing found nothing to read.")
    await _usage_table(provider, "hello", _request("Name one planet."))

    print("\n═══ 2. structured output is honoured ═══")
    print("    The classifier depends on this. The adapter sends")
    print("    response_format={'type':'json_object'} — the widely")
    print("    supported form, NOT the strict json_schema form, because")
    print("    most OpenAI-compatible hosts reject the latter.")
    print("    Expect text that parses as JSON.")
    await _usage_table(
        provider,
        "json mode",
        _request(
            # The word "json" is REQUIRED here, and not as a style
            # preference: OpenAI-compatible backends reject
            # response_format=json_object unless the messages mention it,
            # and this adapter now refuses at the edge rather than let a
            # vendor 400 surface three layers away. Without it this probe
            # never reached the vendor and the section printed a local
            # refusal while claiming to verify hosted structured output.
            'Reply with only a JSON object: {"planet": "<a planet>"}.',
            json_schema={
                "type": "object",
                "properties": {"planet": {"type": "string"}},
                "required": ["planet"],
            },
        ),
    )

    print("\n═══ 3. a rate limit is RETRYABLE, not a failure ═══")
    print("    Groq's free tier meters tokens per minute AND per day.")
    print("    A 429 must map to ProviderError(retryable=True) so the")
    print("    resilience layer backs off instead of failing the user.")
    print("    Measured the hard way: an unpaced run 429'd 115 of 118")
    print("    calls and the number was nearly reported as accuracy.")
    print("    This probe fires ~8 calls to try to provoke one.")
    seen_429 = False
    for attempt in range(8):
        try:
            await provider.complete(_request("Name one planet.", tier="fast"))
        except ProviderError as err:
            _row(f"call {attempt + 1}", f"{err} (retryable={err.retryable})")
            seen_429 = "429" in str(err)
            break
    if not seen_429:
        _row("result", "no rate limit hit in 8 calls — budget is healthy")

    print("\n═══ 4. streaming ═══")
    print("    Expect several chunks, then a final usage record.")
    chunks = 0
    try:
        async for _ in provider.stream(_request("List three planets.")):
            chunks += 1
    except ProviderError as err:
        _row("stream", f"FAILED — {err}")
    else:
        _row("chunks received", chunks)


async def main() -> int:
    known = {"google", "openai-compatible"}
    if len(sys.argv) < 2 or sys.argv[1] not in known:
        print(__doc__)
        return 2

    if not settings.llm_api_key and sys.argv[1] != "openai-compatible":
        print("LLM_API_KEY is empty. Put it in services/ai/.env — not on the command")
        print("line, where it lands in shell history.")
        return 2

    print(f"provider={sys.argv[1]}  env={settings.env}")
    print("This makes REAL, BILLED calls. Read the tables; do not read the exit code.\n")

    if sys.argv[1] == "google":
        await verify_google()
    else:
        await verify_openai_compatible(sys.argv[2] if len(sys.argv) > 2 else None)

    print("\nRecord the date, SDK version and outcome in docs/PROJECT_STATUS.md.")
    return 0


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
