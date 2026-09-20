# Phase 4 gate — what passes, what does not, and what nobody can prove yet

Every item in `docs/specs/PHASE-04-AI-INFRASTRUCTURE.md` §17, checked by
**executing it** rather than by reading the code. Where a check is a command, the
command is here so anyone can re-run it.

**One gate item is not met.** It is called out in full below rather than folded into
a summary, because a gate report that buries its one failure is worth nothing.

---

## ✅ Met, and verified by running something

| Gate item | How it was checked |
|---|---|
| `LLMProvider` implemented by all four adapters, all passing the parity suite | `tests/test_provider_parity.py` — 4 adapters × the full contract, each through its own SDK against a fake transport. A guard compares `ADAPTERS` against `app.providers.__all__`, so a fifth adapter cannot be added and quietly skipped |
| Ollama runs `fast`, `chat` and `deep` locally at **zero cost** | All three tiers answered from Ollama; `pricing.cost_micros` returned **0 micro-USD** for the lot |
| `MockProvider` powers all CI; **no CI job makes a network call** | The whole suite run under a pytest plugin that raises on `socket.connect` / `create_connection`. 836 passed, zero sockets. The blocker was itself proven to fire |
| **PII guard blocks `production` + non-paid provider** | Executed: `ENV=production` with `tier=local` and `tier=free-hosted` both refused to boot; `tier=paid` booted. The guard now reads the **constructed provider's** tier, not the declared setting — `LLM_PROVIDER=mock LLM_PROVIDER_TIER=paid` used to boot a production service answering with canned fixtures |
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

## ❌ NOT met — intent classifier accuracy

> §17: *"Intent classifier ≥85% on 200 labelled messages, keyword pre-pass working."*

The pre-pass half is met and then some. The model half is not.

| | result |
|---|---|
| Keyword pre-pass | **100% precision at 41% coverage** — asserted in CI on every run |
| `llama3.2:3b`, model only | **40.0%** (80/200) |
| `qwen2.5:7b`, model only | **60.5%** (121/200) |
| As shipped (pre-pass + model) | **not measured** — bounded at ≥ 60.5% |

The as-shipped run was killed twice: once by `/tmp` being cleared, once by Ollama
becoming unresponsive after several hours of concurrent test load. The bound above is
arithmetic, not an estimate: the pre-pass contributes 82 correct answers by
construction, so as-shipped ≥ the model-only figure. The true value sits close to the
lower bound, because the 82 messages the pre-pass takes are the keyword-obvious ones
the model would mostly get right anyway.

**A free local model does not reach 85% on a 21-way classification.** That is the
honest finding, and §15 predicted it: *"Free local models behave differently from
Claude, so dev quality misleads."*

Two things are **not** the explanation, and were ruled out:

- **Not a broken prompt.** `intent_classification.v1` never named its output fields,
  so llama3.2:3b answered `{"intent": "career"}` — correct, and discarded by
  validation. That was a real bug and it is fixed (`v2` names every field, with a
  worked example). v1 is frozen and still loadable, because that is what the
  immutability guarantee is for.
- **Not a broken parser.** Local models wrap JSON in fences, single backticks and
  prose. `app/structured.py` handles all of them.

### The loss is mostly OURS, not the model's

The first measurement reported post-policy accuracy as though it were model
accuracy. It is not. Three things discard a classification before it is scored — an
unparseable reply, a provider error, and the **confidence threshold** — and the third
silently turns a correct answer into a miss whenever the model is right but unsure.

Partial run, `llama3.2:1b`, first 30 messages that reach the model:

| | |
|---|---|
| Model answered **correctly** | 12 |
| Product delivered | 2 |
| **Discarded by the threshold** | **10 — 83% of the model's correct answers** |

The sample is small and the model is the weakest one installed, so the *ratio* will
not hold for a better model. But the mechanism is not in doubt, and it is arithmetic,
not inference: none of those 30 rows is labelled `general_astrology`, so an
unparseable reply cannot score "raw correct" by luck and every one of the 10 must be
a low-confidence discard.

`MIN_CONFIDENCE = 0.6` is applied to a **self-reported** number. Small models are
poorly calibrated and report low confidence on answers they got right.

**This has deliberately not been changed.** The threshold is §6's, and re-tuning it
against a 1B model is precisely the trap §15 describes. What has changed is that the
loss is now *visible*: `IntentResult.fallback_from` and `.fallback_confidence` carry
what policy overrode, so production reports it from the first request rather than
needing this investigation repeated.

It also marks the right seam for Phase 5. §6 asks for *"GENERAL_ASTROLOGY **with
broad context**"* — the safety property is the context BREADTH, not the discarded
label. Phase 4's context builders are stubs, so today discarding the label is the
entire effect. Phase 5's builder can read `fallback_from` and `confidence` and widen
retrieval while keeping the model's best guess for persona and tier, which satisfies
§6 without throwing the answer away.

### Why the full number is still missing

Not for lack of trying — four runs were started and none finished.

An Ollama `llama-server` process has been stuck in a runaway `qwen2.5:7b` generation
for hours, holding 350–970% CPU with the machine at load ~23. It is a root-owned snap
process, so it cannot be killed from this session, and Ollama's own
`{"keep_alive": 0}` unload returns `done_reason: unload` without releasing it. Under
that contention a single classification takes minutes, and 118 of them do not
complete.

This is a machine condition, not a property of the code or the model — and it is
worth writing down, because "the measurement kept failing" and "the model is bad"
would otherwise be indistinguishable in six months.

### What would close it

1. **Free the machine** (one command, needs root):

   ```bash
   sudo snap restart ollama
   ```

2. **Re-run.** Either is enough; the second is faster and answers the diagnostic
   question directly:

   ```bash
   cd services/ai
   uv run python -m scripts.measure_intent_accuracy          # full, both passes
   uv run python -m scripts.diagnose_intent_loss qwen2.5:7b  # deferred only, attributed
   ```

3. **Measure against the paid provider.** Production is Claude; §15's whole point is
   that dev quality misleads. Needs a key — see `docs/PROVIDER-VERIFICATION.md`.

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
| **A human must dial each crisis helpline number** in `services/ai/app/safety/responses/` | A test proves a number is *present*. A wrong one costs someone in crisis the single attempt they were willing to make |
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
