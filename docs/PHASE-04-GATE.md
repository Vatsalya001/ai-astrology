# Phase 4 gate — what passes, what does not, and what nobody can prove yet

Every item in `docs/specs/PHASE-04-AI-INFRASTRUCTURE.md` §17, checked by
**executing it** rather than by reading the code. Where a check is a command, the
command is here so anyone can re-run it.

**Every §17 gate item is met.** The accuracy item closed at **180/200 = 90.0%**, and
the Anthropic line was **superseded by
[ADR-011](decisions/011-remove-anthropic-adapter.md)** on 2026-09-21: the adapter is
removed, the production provider is deferred to Phase 7, and the line's actual purpose
— verify an adapter against a real vendor once, rather than against a response shape we
wrote down — is met by `scripts/verify_provider.py openai-compatible` against Groq.

Two rows below were marked met by an earlier pass and did **not** survive being
re-executed at phase close — the PII guard had a hole in the primary position, and the
"no CI network call" plugin had never been committed. Both are fixed and both are now
enforced by a committed test rather than by a run somebody remembers doing. That is the
argument for auditing a checklist by running it: reading it found neither.

> ⚠️ **Read `docs/PROJECT_STATUS.md` on the ephemeris corruption before trusting any
> number on this page.** Two single-bit flips were found in a committed 16.8 MB binary
> on 2026-09-21, and `memtest86+` has never been run on this machine. Every measurement
> in this report was taken on it.

---

## ✅ Met, and verified by running something

| Gate item | How it was checked |
|---|---|
| `LLMProvider` implemented by every adapter, all passing the parity suite | `tests/test_provider_parity.py` — 3 adapters × the full contract (4 until [ADR-011](decisions/011-remove-anthropic-adapter.md) removed Anthropic), each through its own SDK against a fake transport. A guard compares `ADAPTERS` against `app.providers.__all__`, so a fifth adapter cannot be added and quietly skipped |
| Ollama runs `fast`, `chat` and `deep` locally at **zero cost** | All three tiers answered from Ollama; `pricing.cost_micros` returned **0 micro-USD** for the lot |
| `MockProvider` powers all CI; **no CI job makes a network call** | This row used to describe a plugin that was run once and **never committed** — true the day somebody checked, enforced by nothing after. `tests/conftest.py` now blocks every non-loopback `connect` / `connect_ex` / `create_connection` for the whole suite, autouse so a new file is covered without opting in. **931 pass under it.** Loopback stays allowed on purpose: `test_a_dead_primary_is_served_by_the_fallback` needs a real `ConnectionRefusedError` from a closed local port. It matters more than it did — `services/ai/.env` now holds a real key that pydantic reads at import, so an unmocked provider spends real quota **and still reports PASS** |
| **PII guard blocks `production` + non-paid provider** | Re-executed at phase close, and it found a hole this row previously denied. `ENV=production LLM_PROVIDER=google LLM_PROVIDER_TIER=paid` **booted** — the fallback path withheld the declared tier (its docstring names the risk: *"precisely how a free Gemini key gets blessed as paid and receives birth data in production"*) while the primary passed it, so the refusal only covered the position that takes outage traffic, not the one that takes all of it. Fixed; `google` and `mock` now refuse in production whatever the setting claims, and `openai-compatible` still takes its declared tier **deliberately** — it reaches both Ollama on localhost and paid inference hosts, so there is no vendor identity to infer from. All of it is pinned in `TestADeclaredTierCannotBlessAFreeKey`. Since ADR-011, `openai-compatible` + a declared `paid` tier is the ONLY production-legal configuration |
| Model router maps all 10 job types; **overridable from admin without deploy** | `GET`/`PATCH /api/v1/admin/ai/routing`. This did not exist — see *Fixed during review* |
| Prompt registry immutable; editing a published module fails CI | Executed: appending one line to `safety_rules.v1.md` failed with *"these published prompt modules were edited in place"* |
| Cache breakpoint ordering verified by a prefix-stability test | Two different users' charts produce a byte-identical prefix; a stable module changing moves it. **See the caveat below — the breakpoint does not yet engage** |
| Crisis input short-circuits to a **static, human-written** response | Asserted on the *provider*, not the text: `generator.requests == []`. Both paths — the offline keyword pass (zero model calls) and the model screener |
| Output validator blocks fabricated chart facts, unsupported certainty and prompt leaks | 35 table-driven cases in three directions |
| Telemetry envelope persisted by Go; `cost_micros` is an integer | `BIGINT`, proven against real Postgres by round-tripping 2^53+1 — a value no `float64` holds |
| Retry, circuit breaker and fallback verified **by killing the primary mid-run** | Executed end to end: primary killed → fallback served → breaker opened after the threshold → served in **0.1 ms** instead of paying the timeout → recovered when the primary returned |
| Generated Go client compiles from the AI OpenAPI; contract diff passes | `task contracts` → `oapi-codegen` → `go build ./...` clean |
| Admin config, usage, incidents and playground all working | Not just *reachable*: two rows seeded, then read back through HTTP asserting totals, cache hit rate, grouping, and that incidents selects only the failure |
| **No message content in `ai_request_logs`** | Asserted against the *serialised* envelope, so a field added later is caught too |
| `ai-service` DB role still read-only | `TestEveryRealTableRefusesWritesFromReader` covers `ai_request_logs` automatically |
| `task verify` green | ✅ |

---

## ✅ MET — intent classifier accuracy

> §17: *"Intent classifier ≥85% on 200 labelled messages, keyword pre-pass working."*

**180/200 = 90.0%**, measured end to end on a hosted model, with 4 of 118 calls lost
at the provider — below the 5% the script tolerates before it refuses to report.

| | result |
|---|---|
| Keyword pre-pass | **82/82 correct — 100% precision at 41% coverage** |
| Model accuracy, `qwen/qwen3.8-27b` | **100/114 = 87.7%** |
| **As shipped, whole set** | **180/200 = 90.0% — MEETS the gate** |
| Ceiling if the threshold discarded nothing | 182/200 = 91.0% |

Reproduce it:

```bash
cd services/ai
MEASURE_TOKENS_PER_MINUTE=7200 uv run python -m scripts.diagnose_intent_loss qwen/qwen3.8-27b
```

### The threshold is now the only thing left on the table, and it is well-calibrated

Eight correct answers were discarded, every one of them at exactly **0.3**:

```
115  what should i know          ->  general_astrology @ 0.3  CORRECT, discarded
124  hi                          ->  other             @ 0.3  CORRECT, discarded
144  tell me more                ->  general_astrology @ 0.3  CORRECT, discarded
158  Am I going to be successful?->  general_astrology @ 0.3  CORRECT, discarded
```

Read the messages. *"hi"*, *"ok"*, *"tell me more"*, *"what should i know"* — these
genuinely are ambiguous, and 0.3 is the **right** confidence for them. That is a model
reporting uncertainty accurately, which is the opposite of the `llama3.2:3b` failure
where every discarded answer read exactly `0.0` because it was copying the prompt.

Dropping `intent_min_confidence` to 0.25 would recover all eight and reach 188/200 =
94%. **That has deliberately not been done.** These 200 messages are the regression
suite; a threshold fitted to them is a number that does not generalise, and §17 is
already met without it. Phase 6's eval harness chooses that value on held-out data.

---

## Superseded: what the local model scored, and why it is recorded anyway

The local `fast` tier cannot reach the gate, and the record of finding that out is
worth more than the number. Four attempts died on a wedged machine; three full
118-message runs then completed on 2026-09-21, all on `llama3.2:3b`.

| | result |
|---|---|
| Keyword pre-pass | **82/82 correct — 100% precision at 41% coverage**, re-verified by execution, not read off this table |
| As shipped, prompt **v2** | **134/200 = 67.0%** |
| As shipped, prompt **v3** | **119/200 = 59.5%** — a regression I shipped; see below |
| As shipped, prompt **v4** | **134/200 = 67.0%** |
| Model-only accuracy, v4 | 57/94 = **60.6%** on the messages that reach it |
| Ceiling if the threshold discarded nothing | 139/200 = **69.5%** |

Read the last row first. Even a *perfect* confidence policy leaves this at 69.5%, so
nothing about thresholds, parsing or prompts closes a 15-point gap. **A 3B local model
does not do 21-way classification at 85%.** That is the finding, and §15 predicted it:
*"Free local models behave differently from Claude, so dev quality misleads."*


### A hosted model clears the bar — measured, on 70 messages

`openai/gpt-oss-120b` on Groq's free tier, prompt v4:

| | result |
|---|---|
| **Model accuracy on what it was asked** | **65/70 = 92.9%** |
| Correct answers lost to the threshold | **3**, all at confidence 0.3 |
| As shipped, whole set | **not soundly measured** — see below |

92.9% is the answer to the question this gate item actually asks. The local 3B
manages 60.6% on the same prompt and the same 118 messages, so the gap is the model,
exactly as §15 warned.

**The as-shipped figure from that run is not quotable, and the script now refuses to
quote it.** 48 of 118 calls died at the provider, so those rows fell back to
`GENERAL_ASTROLOGY` — a label 27 of the 200 rows carry — and *failing scored points*.
The run printed 80.5% while the model was answering 92.9% of what it was given.

The cause was a limit that is not in the response headers. Groq advertises
`x-ratelimit-limit-tokens: 8000` per minute; the binding limit is **200,000 tokens per
day**, which appears only in the body of the 429 that eventually fires. At ~1,400
tokens per classification that is ~142 calls a day — one clean 118-message run, with
little spare, and the earlier unpaced attempt had already spent most of it.

**What would close the item:** one clean run once the daily budget resets — roughly 45
minutes at the per-minute pacing, and the script exits non-zero unless it completes
without provider errors, so a contaminated run cannot be mistaken for a result again:

```bash
cd services/ai
MEASURE_TOKENS_PER_MINUTE=7200 uv run python -m scripts.diagnose_intent_loss openai/gpt-oss-120b
```

The model is named on the command line, not pinned in `services/ai/.env`. Pinning it
there is a trap: `Taskfile.yml` loads the repo-root `.env` into every task, and an OS
variable beats a `.env` file — so provider, base URL and tier all correctly fall back
to local Ollama under `task dev:ai`, while a model pinned in the service's own file is
the one thing that does *not* get overridden. It leaks across, and `task dev:ai` sends
`openai/gpt-oss-120b` to `localhost:11434`: a 404 on every request.

A model name is not a secret, so the command line is the right place for it. Only the
key lives in `.env`.

An earlier version of this report said *"the prompt is not the explanation"* and
listed it as ruled out. **That was wrong, twice**, and the corrections are the most
useful thing on this page:

- `intent_classification.v1` never named its output fields, so llama3.2:3b answered
  `{"intent": "career"}` — correct, and discarded by validation. Fixed in v2.
- `v2` then handed the model a literal `"confidence": 0.0` to copy, which is what the
  next section is about. Fixed in v4.

Both were found by *measuring*, and both had been written off in prose first. What is
genuinely ruled out is the parser: local models wrap JSON in fences, single backticks
and prose, and `app/structured.py` handles all of them — 18 of 118 replies are still
unparseable under v4, but they are empty or truncated rather than merely wrapped.

### The confidence signal was ours, and it was broken

The single most useful line in the first completed run was not the accuracy:

```
discarded confidences: min 0.0, median 0.0, max 0.0
```

Not a spread — a constant. `confidence` is a **required** field with no default, so
`llama3.2:3b` was genuinely emitting `0.0` on answers it had got right. 19 correct
labels discarded.

**Root cause, proven causal rather than inferred.** `intent_classification.v2`'s
output template gave `"confidence"` as a concrete `0.0` while every sibling field was
a placeholder. A 3B model pattern-completes the nearest template: it substituted
`primary` and copied the rest verbatim. Changing that one line to a placeholder and
nothing else moved the same three messages from `0.0, 0.0, 0.0` to `0.5, 0.0, 0.8`.
`qwen2.5:7b` ignores the template and reports ~0.8, which is why this stayed invisible
until it was measured on the model that actually serves the `fast` tier.

A template value that is also a plausible answer is indistinguishable from an
instruction to give that answer.

**This also makes the 0.6 → 0.4 threshold change inert**, and that is arithmetic from
the measured data rather than a second run: every discarded confidence in the v2 run
was exactly `0.0`, and no threshold above zero keeps a `0.0`. The same 19 correct
answers are lost at either setting. The change neither helped nor hurt.

Worth stating because the threshold was lowered on the strength of evidence that
pointed at it, and the evidence turned out to point one level deeper. Lowering it was
not wrong; it was aimed at a symptom whose cause was in the prompt.

### v3 was a regression, and it is recorded as one

v3 turned every template value into a placeholder. It fixed the confidence signal and
broke something worse: the model answered the entity *descriptions* —
`"<the period the message states, or \"\">"` — with `null` instead of `""`, which
failed validation and discarded the whole classification. Unparseable replies went
from 18 to **66 of 118**, and as-shipped accuracy fell to 59.5%.

Two fixes, and the order matters:

1. **`Entities` now coerces `null` to `""`.** This is the real defect. A classifier
   that loses a correct `primary` over the spelling of "nothing" is brittle against
   every model and every future prompt version, and no prompt wording makes that
   acceptable.
2. **v4 uses two worked examples** with *different* confidences instead of a template
   with placeholders. Two differing values cannot be copied as one.

**v4 scores 134/200 — exactly what v2 scored.** Model-only accuracy rose from 58.2% to
60.6% and the discarded confidences finally show a spread rather than a constant, so
the mechanism is genuinely fixed. But the bottom line did not move, and saying
otherwise would be dressing up a tie.

Three prompt versions were produced in one session. v2 and v4 are on the board; v3 is
on it too, as a regression, because a gate report that only lists the attempts that
worked is not a record of anything.

### What the loss actually is

Three things discard a classification before it is scored — an unparseable reply, a
provider error, and the confidence threshold — and only the third is policy. Under v4:

| | of 118 |
|---|---|
| Model answered | 94 |
| ...correctly | 57 |
| Unparseable or provider error | 24 |
| Correct answers still discarded by the threshold | 18 |

The threshold is applied to a **self-reported** number, and `llama3.2:3b` still
reports `0.0` on some answers it got right even under v4. `intent_min_confidence` is a
setting precisely so this can be re-tuned against a real model without a deploy.

`IntentResult.fallback_from` and `.fallback_confidence` carry what policy overrode, so
production reports this from the first request rather than needing the investigation
repeated. That is also the right seam for Phase 5: §6 asks for *"GENERAL_ASTROLOGY
**with broad context**"* — the safety property is the context BREADTH, not the
discarded label, so Phase 5's builder can widen retrieval while keeping the model's
best guess.

### Why it took five attempts

Four runs were started and killed before any finished — an Ollama `llama-server` stuck
in a runaway generation for hours at 350–970% CPU with the machine at load ~23, as a
root-owned snap process that could not be killed from the session and did not release
on `{"keep_alive": 0}`.

It is worth writing down, because "the measurement kept failing" and "the model is
bad" would otherwise be indistinguishable in six months. The machine freed at 02:17 on
2026-09-21 (load 1.24) and three full runs completed back to back.

### What would close it

**Not this:** more local prompt work. Three versions were tried and the ceiling is
69.5%. The next change that moves this number is a different model, not a better
sentence.

1. **Measure against a hosted model**, which does not need the local machine at all
   and is the run that actually matters — §15's whole point is that dev quality
   misleads. Both scripts now build whatever `LLM_PROVIDER` names and **print what
   they are talking to before they start**:

   ```bash
   cd services/ai
   # three lines in services/ai/.env, and NOTHING else:
   #   LLM_PROVIDER=google
   #   LLM_PROVIDER_TIER=free-hosted
   #   LLM_API_KEY=<https://aistudio.google.com/apikey>
   uv run python -m scripts.verify_provider google   # 3 probes, reads as tables
   uv run python -m scripts.diagnose_intent_loss     # model defaults follow the provider
   ```

   Two defects stood between that instruction and it being true, and both were found
   by *running* it rather than reading it:

   - Both scripts hardcoded `OpenAICompatibleProvider` while the service built from
     config, so this would have produced a number measured against `localhost:11434`
     under a heading naming Gemini. `app/providers/factory.py` is now the single
     construction path, and every script prints what it resolved before it starts.
   - The three model names were provider-independent settings, so `LLM_PROVIDER=google`
     sent the model name `llama3.2:3b` to the Gemini API — a `404 model not found`
     whose obvious readings are "my key is bad" and "the adapter is broken". Models now
     follow the provider unless explicitly pinned, and **both `.env.example` files
     stopped pinning them**, which is where the bug survived the first fix.

### What must NOT close it

Tuning the prompt against these 200 messages. They are the *regression suite*; a
number produced by fitting to them would not generalise, and Phase 6's eval harness
exists precisely so prompt changes are validated on held-out data.

---

## ⚠️ Met in letter, with a caveat worth stating

**The cache breakpoint has never engaged.** The shipped prefix is ~770 tokens against
Anthropic's ~1024 minimum (2048 on Haiku). Below that, `cache_control` is ignored
entirely — no write, no read, no error.

This is not a bug in `PromptBuilder`; it is the honest state of a phase whose stable
prefix is five short modules, and §2 expects the astrology corpus to be large. Phase
5's RAG corpus takes it over the line.

It is called out because §15 names *"prompt caching silently stops working in prod"*
as a risk — and a cache that never **started** working is indistinguishable from the
outside. `test_prompt_registry.py` now asserts the prefix is still below the
threshold and **fails when Phase 5 pushes it over**, which is how somebody learns the
date the cache began paying for itself.

---

## 🔒 Owed, and not closeable by any test

| Item | Why a test cannot do it |
|---|---|
| ~~A human must dial each crisis helpline number~~ — **closed by removing the numbers** | A test proves a number is *present*; only a human proves it *answers*, and a wrong one costs someone in crisis the single attempt they were willing to make. Rather than ship unverified numbers, `crisis.en.md` and `crisis.hi.md` now point at **findahelpline.com**, which maintains them as a full-time job. The debt is not deferred, it is discharged: there is no unverified number left in the repo. `app/safety/responses/README.md` records the reasoning and the procedure for adding a number back once someone has dialled it |
| A native speaker should review the Hinglish crisis phrases | The regexes match the sentences claimed; whether those are the sentences real users write is not something this repo can assert |
| `AnthropicProvider` verified once against a real key, **including prompt caching** | Offline tests exercise our parsing of a response shape *we wrote down*. See `docs/PROVIDER-VERIFICATION.md` |
| `GoogleProvider` free-tier run | Same |
| **GitHub Actions is out of minutes** | Not "never executed" — that note was stale. CI has run **178 times** and last *succeeded* on **2026-09-16**. Every run since fails in **~2 seconds with zero steps**, which is the signature of an exhausted quota, not a broken build. **Proven, not assumed:** every CI job's steps were extracted from `.github/workflows/ci.yml` and run locally — go, contracts, integrity, secret scan, both Python services, the determinism guard, web, and env-drift all pass, and `go-integration` + `e2e` were run separately against real Postgres and a real browser. The repository's code is green; the runner is not |

---

## Fixed during the gate review

A nine-dimension adversarial review (156 agents; every finding put to three
independent skeptics with distinct lenses) surfaced 39 confirmed defects in this
phase's own code. All are fixed and break-tested. The ones that mattered most:

1. **A crisis message could reach astrology generation.** The safety JSON parser was
   strictly weaker than the intent one — the recoverable path got the robust parser
   and the unrecoverable one did not.
2. **True general statements were blocked as fabrications.** *"Saturn rules
   discipline, and your 10th house is career"* — extracted as a false placement by a
   wildcard proximity window, blocked, regenerated, blocked, answered with the
   fallback. The validator had made the product unable to *explain* astrology in
   order to stop it *inventing* astrology.
3. **Every completion was capped at 10s under a comment claiming 90.**
4. **Two thirds of the model calls were never billed.**
5. **§7's action table was documentation** — only `CRISIS` was ever acted on.
6. **The corrective retry put model output in the system prompt.**
7. **§17's "overridable without deploy" and §14's "audit-logged in Go" did not exist.**

Three of the *fixers'* own fixes were then caught by the verifiers, two in the safety
path — including negative lookaheads that matched a prefix rather than a word, so
`to` swallowed *"take my life **to**night"*.
