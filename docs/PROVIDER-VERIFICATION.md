# Provider verification — the parts a mock cannot prove

Some gate items cannot be closed by the test suite, by design.

> ⚠️ **`AnthropicProvider` is gone** — [ADR-011](decisions/011-remove-anthropic-adapter.md).
> There is no Anthropic subscription and no free tier, so that line was superseded
> rather than met, and `verify_provider anthropic` no longer exists.
>
> The modes that DO exist:
>
> ```bash
> uv run python -m scripts.verify_provider google
> uv run python -m scripts.verify_provider openai-compatible [model]
> ```
>
> The second is the one ADR-011 names as the gate evidence: it covers the adapter
> actually carrying traffic, and it is the only one reachable at a hosted vendor for
> free. Both were run on 2026-09-21; between them they found four defects that every
> offline test had passed.

**Settled without a key:** whether `temperature` may be sent alongside `effort`.
`messages.create` has no `temperature` parameter in anthropic 1.7.0 at all — the
SDK's overload list says so and `mypy --strict` reports any attempt to pass one,
so the adapter's omission is checked at build time rather than needing a run.

Everything else about these adapters is tested offline against a fake transport,
which exercises our parsing of a response shape **we wrote down**. That catches
every bug on our side of the wire and none on theirs. This document is the other
side: one run each, against the real API, checking the handful of facts that only
the vendor can confirm.

It is a **manual, on-demand** procedure. CI must never do this — `.claude/rules/testing.md`
is unambiguous that no CI job calls a language model, and these runs cost money.

---

## Before you start

You need one key for whichever provider you are verifying. Put it in
`services/ai/.env` — never on the command line, where it lands in shell history:

```bash
# services/ai/.env   (gitignored)
LLM_PROVIDER=google            # or openai-compatible + LLM_BASE_URL
LLM_PROVIDER_TIER=free-hosted
LLM_API_KEY=AIza...            # https://aistudio.google.com/apikey
```

Then, from `services/ai`:

```bash
uv run python -m scripts.verify_provider google
uv run python -m scripts.verify_provider openai-compatible qwen/qwen3.8-27b
```

The script prints a table. **Read the table — do not read the exit code.** Every
check below has a failure mode where the call succeeds and the answer is wrong,
which is precisely why these are not assertions.

---

## ~~Anthropic — the five facts~~ (adapter removed, ADR-011)

Kept as a record of what a real-key run is FOR, not as instructions: there is no
`AnthropicProvider` to run them against. If Claude is ever added back, these five
are the checks that mattered — every one of them is a way for the call to succeed
while the answer is wrong.

## Google — the three facts

### 1. The free tier answers at all

Rate limits on the free tier are per-minute and per-day. A 429 on the first
call of the day means the key is exhausted, not that the adapter is broken.

### 2. `promptTokenCount` is inclusive of cached tokens

The adapter **subtracts** — the opposite convention from Anthropic — and the
whole cost calculation for this provider depends on that being right. Confirm
against a cached request that:

```
usage.input_tokens + usage.cached_input_tokens == promptTokenCount (raw)
```

Gemini caches implicitly, so this needs a long, repeated prefix and may need two
or three runs before an implicit cache is populated.

### 3. A safety block returns HTTP 200 with no candidates

Send something that trips the safety filter. Confirm the transport reports
**200**, the body has an empty `candidates` list, and the adapter returns
`finish_reason == "refusal"` rather than an empty `stop`.

---

## Embedding width

Only if you intend to use `GoogleEmbeddingProvider` in place of Ollama:

```
len(vector) == EMBEDDING_DIM   for every vector, every batch size
```

A pgvector column is fixed-width. A wrong width either fails on insert or lands
in a column that accepts it and makes every similarity score meaningless, and
the second one is silent.

---

## Recording the result

Append the date, the SDK version and the outcome of each numbered check to
`docs/PROJECT_STATUS.md`. "Verified" with no date is worth nothing a year later,
because the thing being verified is a vendor's behaviour and vendors change it.
