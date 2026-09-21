# ADR-011 — Remove the Anthropic adapter; production provider deferred to Phase 7

**Status:** accepted · 2026-09-21
**Supersedes in part:** [ADR-004](./004-llm-provider-abstraction.md), which named
Anthropic as the production provider.

## Decision

`AnthropicProvider` is **deleted**, along with its tests, its pricing rows and the
`anthropic` dependency. `LLM_PROVIDER` is now `openai-compatible | google | mock`.

The choice of production provider is **deferred to Phase 7**, the phase that actually
serves users. Invariant 3 is unchanged: `app/guards.py` still refuses to boot
production on any tier that is not `paid`.

## Context

The owner has no Anthropic API subscription, and **Anthropic sells no free tier** — a
claude.ai subscription does not grant API access. PHASE-04 §17 lists

> *"`AnthropicProvider` verified once against a real key, incl. prompt caching"*

as a gate line, which made the phase un-closeable without a purchase.

Two facts made deferral the better answer than buying access:

1. **The prompt-caching half was never satisfiable in Phase 4.** The stable prefix is
   ~770 tokens against the ~1024-token minimum below which `cache_control` is ignored
   entirely — no write, no read, no error. Even with a key there is no cache behaviour
   to observe until Phase 5's corpus grows the prefix.
2. **Phase 4 exposes nothing to users.** §1 says so. A production-provider commitment
   made here would be made at the point of least information.

## Reason

- The gate line's *purpose* — "offline tests only exercise a response shape we wrote
  down; verify against a real vendor once" — is provider-agnostic, and is now met by
  `scripts/verify_provider.py openai-compatible`, run against Groq.
- `OpenAICompatibleProvider` already covers Ollama, Groq, OpenRouter, Cerebras and
  LM Studio. Keeping a fourth adapter nothing exercised would leave 797 lines whose
  only proof of life was a mock transport.
- Deleting is cheap to reverse *in principle* (git history) and the decision to do so
  was taken explicitly by the owner after the tradeoff was put to them.

## Consequences

**`openai-compatible` with a declared `paid` tier is now the only production-legal
configuration.** That is coherent rather than accidental: it is the one adapter with no
vendor identity to infer a tier from, so the operator's declaration is the only signal
there is. `google` cannot be blessed this way — its adapter declares its own
`free-hosted` tier, because an API key string cannot be inspected for whether billing
is attached.

Several tests used `anthropic` as their stand-in for "a genuinely paid provider" and
now use `openai-compatible` + `paid`. The pricing arithmetic tests used
`claude-sonnet-5` as their worked example and now register a **synthetic** price row:
those tests are about integer arithmetic, not about any vendor's commercial decisions,
and coupling them to a real row is why fourteen of them failed when a price list
changed.

## Tradeoffs

- Re-adding Claude means rewriting the adapter, its streaming path, its cache
  breakpoints and its usage accounting — Anthropic reports the three input token
  classes disjointly, which no other adapter here does.
- The project loses its only adapter that supports explicit per-block prompt caching.
  `PromptBuilder`'s breakpoint machinery remains and is still correct; it simply has no
  backend that honours it until one is added.

## Revisit when

- Phase 7 chooses a production provider and needs a `paid`-tier vendor with a
  no-training commitment.
- Phase 5's RAG corpus pushes the cacheable prefix past ~1024 tokens and explicit
  cache breakpoints start paying for themselves —
  `test_prompt_registry.py::test_the_cacheable_prefix_is_still_below_the_threshold`
  fails on that day, by design.
