# ADR-004 — One provider protocol; free models in development, paid in production

**Status:** accepted · 2026-09-13

## Decision

All LLM access in `ai-service` goes through an `LLMProvider` protocol. Nothing outside
`app/providers/` imports a vendor SDK. The active provider is chosen entirely by
configuration.

A **startup guard** refuses to run a non-paid provider when `ENV=production`.

## Context

Development should cost nothing. Production needs frontier-model quality. Those are
different providers, and the switch must not require touching application code.

## Reason

**One adapter, five backends.** Ollama serves an OpenAI-compatible endpoint at `/v1`,
so the official `openai` Python SDK also reaches LM Studio, Groq, OpenRouter and
Cerebras. Writing that one adapter covers every free option worth using.

**The PII guard is the load-bearing part.** Requests carry birth date, birth time and
birth place — close to a unique identifier in combination — plus conversation content
about health, marriage, money and family. Most free API tiers reserve the right to
train on inputs.

So the rule is: free models for development and CI, a paid provider with a no-training
commitment in production, enforced by the process refusing to boot. A wiki page is not
an enforcement mechanism.

Local Ollama is exempt from the privacy concern — nothing leaves the machine — but is
still blocked in production on reliability grounds. A single `tier != "paid"` check
covers both cases.

## Tradeoffs

- Dev output quality differs from production. Mitigated by Phase 6's eval harness,
  which scores retrieval and grounding (model-independent) separately from prose
  quality, and by a provider comparison mode.
- The abstraction is one more layer. Kept deliberately thin: four adapters, one
  protocol, no plugin system.

## Verification

`services/ai/tests/test_guards.py` — 16 tests, including every disallowed
provider/environment combination. Proven to fire in a live process, not just in unit
tests.
