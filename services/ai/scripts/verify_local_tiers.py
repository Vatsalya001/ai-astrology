"""Prove §17's "Ollama runs `fast`, `chat` and `deep` tiers locally at zero cost".

    uv run python -m scripts.verify_local_tiers

Prints the model, token counts and cost for each tier, then exits
non-zero if any tier failed or if anything cost money.

── Why this is not a pytest module ──

`.claude/rules/testing.md` forbids CI from calling a language model, and
a test file is a thing CI collects by default. The only reliable way to
keep a real-model call out of a test run is for it not to be a test.
Same reasoning as `verify_provider.py`.

── Why it builds nothing itself ──

Every provider here comes from `provider_from_settings()` — the same
factory `app/api/complete.py` calls. This project has already been
bitten by the alternative: two measurement scripts hardcoded
`OpenAICompatibleProvider` at `localhost:11434` while the service built
from `LLM_PROVIDER`, so with the provider switched to Google the scripts
went on quietly measuring Ollama and printing a Gemini heading over the
numbers.

The resolved configuration is printed first, for the same reason: a run
whose heading does not match what you meant is a run to throw away.

── What "locally" requires ──

`LLM_BASE_URL` must point at an Ollama this process can reach. Run it on
the HOST, where `localhost:11434` is correct — see
`services/ai/.env.example`. From inside the container `localhost` is the
container, and the repo-root `.env` uses `host.docker.internal` for that
reason.
"""

from __future__ import annotations

import asyncio

from app.providers import (
    CompletionRequest,
    Message,
    ProviderError,
    RequestMetadata,
)
from app.providers.factory import provider_from_settings
from app.settings import settings

TIERS = ("fast", "chat", "deep")

# Short, and answerable by a 1B model. The question is whether the tier
# is wired and free, not whether the model is clever — a prompt that a
# 3B model fails would fail this check for the wrong reason.
PROMPT = "Reply with exactly one word: hello"


async def main() -> int:
    model_for = {
        "fast": settings.llm_model_fast,
        "chat": settings.llm_model_chat,
        "deep": settings.llm_model_deep,
    }

    print("resolved configuration")
    print(f"  LLM_PROVIDER      {settings.llm_provider}")
    print(f"  LLM_BASE_URL      {settings.llm_base_url}")
    print(f"  LLM_PROVIDER_TIER {settings.llm_provider_tier}")
    for tier in TIERS:
        print(f"  {tier:17} {model_for[tier] or '(provider default)'}")
    print()

    if settings.llm_provider != "openai-compatible":
        print(f"✗ LLM_PROVIDER is {settings.llm_provider!r}, not openai-compatible.")
        print("  This check is about Ollama. Point LLM_PROVIDER at it and re-run.")
        return 2

    failures: list[str] = []
    total_cost = 0

    for tier in TIERS:
        provider = provider_from_settings()
        request = CompletionRequest(
            messages=[Message(role="user", content=PROMPT)],
            tier=tier,  # type: ignore[arg-type]
            max_tokens=32,
            metadata=RequestMetadata(trace_id=f"verify-local-{tier}"),
        )

        try:
            response = await provider.complete(request)
        except ProviderError as err:
            # Named rather than swallowed: "every tier failed" and "the
            # deep tier is unreachable" are different problems, and the
            # commonest cause of the first is an Ollama the process
            # cannot reach at all.
            print(f"✗ {tier:5} FAILED  {err}")
            failures.append(tier)
            continue

        usage = response.usage
        total_cost += usage.cost_micros
        print(
            f"✓ {tier:5} {response.model:18} "
            f"in={usage.input_tokens:4} out={usage.output_tokens:4} "
            f"cost_micros={usage.cost_micros}  {response.latency_ms:5} ms"
        )

    print()

    if failures:
        print(f"✗ {len(failures)} of {len(TIERS)} tiers failed: {', '.join(failures)}")
        print()
        print("  If every tier failed with 'unreachable', this process cannot reach")
        print(f"  Ollama at {settings.llm_base_url}. Check:")
        print("    curl -s http://localhost:11434/api/tags | head")
        print("  and note that inside a container `localhost` is the container.")
        return 1

    # The second half of the gate line, and the one a passing call does
    # not establish on its own: a tier that answered from a HOSTED
    # provider would print a cost above zero here.
    if total_cost != 0:
        print(f"✗ tiers answered but cost {total_cost} micro-USD — that is not local.")
        return 1

    print(f"✓ all {len(TIERS)} tiers answered from {settings.llm_base_url} at zero cost.")
    return 0


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
