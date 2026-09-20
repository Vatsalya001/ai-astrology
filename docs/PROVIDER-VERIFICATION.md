# Provider verification — the parts a mock cannot prove

Two Phase 4 gate items cannot be closed by the test suite, by design:

> - `AnthropicProvider` verified once against a real key, **incl. prompt caching**
> - `GoogleProvider` — free-tier fallback works

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
LLM_API_KEY=sk-ant-...
```

Then, from `services/ai`:

```bash
uv run python -m scripts.verify_provider anthropic
uv run python -m scripts.verify_provider google
```

The script prints a table. **Read the table — do not read the exit code.** Every
check below has a failure mode where the call succeeds and the answer is wrong,
which is precisely why these are not assertions.

---

## Anthropic — the five facts

### 1. A second identical request reads from the cache

The one that matters most, because it is worth roughly a 10x reduction on the
input side of every request this product makes, and because it fails **silently**:
the responses stay perfect and the bill doubles.

The script sends the same request twice and prints usage for both.

| | first call | second call |
|---|---|---|
| `cache_write_input_tokens` | **> 0** | 0 |
| `cached_input_tokens` | 0 | **> 0, and large** |
| `input_tokens` | small | small |

**If `cached_input_tokens` is 0 on the second call**, in likely order:

- The prefix was under ~1024 tokens, so Anthropic declined to cache it at all.
  The script pads the prefix past that; a real short prompt genuinely will not
  cache and that is not a bug.
- The two prefixes were not byte-identical. `PromptBuilder` exists to prevent
  this, and `tests/test_prompt_registry.py::test_two_charts_share_a_prefix`
  covers it — but a volatile block that crept in **before** the breakpoint
  reproduces it exactly.
- The breakpoint landed in the wrong place. Covered offline by
  `TestCacheBreakpoint`, so this would be surprising.

### 2. `effort` is accepted on all three tiers

`fast`/`chat`/`deep` map to `low`/`medium`/`high`. A rejected value is a 400 on
every request at that tier, so a dev machine that only ever exercises `chat`
would ship a broken `deep` path.

### 3. A refusal arrives as `stop_reason: "refusal"`

Send something the model will decline. Confirm `finish_reason == "refusal"` and
**not** `error`, and that `text` is non-empty — the safety path renders that
text, and a refusal that arrives with nothing to show is a blank bubble.

### 4. Thinking blocks are present and excluded

With effort engaged, confirm the raw response contains a `thinking` block and
that `response.text` does not contain any of it. Offline this is tested against
a fixture we authored; here it is tested against a block Claude actually
produced.

---

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
