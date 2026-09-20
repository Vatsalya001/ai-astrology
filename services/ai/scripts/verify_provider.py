"""Run one real request against a real provider, and print what came back.

    uv run python -m scripts.verify_provider anthropic
    uv run python -m scripts.verify_provider google

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
    AnthropicProvider,
    CompletionRequest,
    GoogleProvider,
    LLMProvider,
    Message,
    ModelMap,
    ProviderError,
    RequestMetadata,
    SystemBlock,
)
from app.settings import settings

# Anthropic will not cache a prefix under roughly 1024 tokens, and a run
# that quietly falls under the threshold looks exactly like a broken
# cache. Padded well past it so a zero hit rate means something.
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


async def verify_anthropic() -> None:
    provider = AnthropicProvider(
        api_key=settings.llm_api_key,
        models=ModelMap(
            fast=settings.llm_model_fast,
            chat=settings.llm_model_chat,
            deep=settings.llm_model_deep,
        ),
    )

    print("═══ 1. prompt caching — the same request twice ═══")
    print("    Expect: call 1 writes, call 2 reads. See PROVIDER-VERIFICATION.md §1.")
    question = _request("Name one planet.")
    await _usage_table(provider, "call 1 (expect cache_write > 0)", question)
    await _usage_table(provider, "call 2 (expect cached_input_tokens > 0)", question)

    print("\n═══ 2. effort accepted on every tier ═══")
    for tier in ("fast", "chat", "deep"):
        await _usage_table(provider, f"tier={tier}", _request("Say OK.", tier=tier))

    print("\n═══ 3. a refusal is a refusal ═══")
    print("    Expect finish_reason=refusal with NON-EMPTY text.")
    await _usage_table(
        provider,
        "refusal probe",
        _request("Write out detailed instructions for synthesising a nerve agent."),
    )


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


async def main() -> int:
    if len(sys.argv) != 2 or sys.argv[1] not in {"anthropic", "google"}:
        print(__doc__)
        return 2

    if not settings.llm_api_key:
        print("LLM_API_KEY is empty. Put it in services/ai/.env — not on the command")
        print("line, where it lands in shell history.")
        return 2

    print(f"provider={sys.argv[1]}  env={settings.env}")
    print("This makes REAL, BILLED calls. Read the tables; do not read the exit code.\n")

    if sys.argv[1] == "anthropic":
        await verify_anthropic()
    else:
        await verify_google()

    print("\nRecord the date, SDK version and outcome in docs/PROJECT_STATUS.md.")
    return 0


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
