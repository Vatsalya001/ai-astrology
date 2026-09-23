# Current phase

```
Phase: 4 — AI Infrastructure
Gate:  CLOSED 2026-09-23. §17 19/19, §14 14/14, §16 6/7.
       21 tasks done. Nothing is exposed to users in this phase.

       Read the boxes in docs/specs/PHASE-04-AI-INFRASTRUCTURE.md, not
       this line. Each carries the command that proves it. This line
       once read "✅ 19 of 19 closed" while eighteen of those boxes were
       unticked and the one [x] was a descope, not a completion —
       nothing had ever been ticked, and the closure existed only here.
       A count in a state file is the thing that rots; the boxes are the
       artifact. That is why no per-item detail lives here now.

       Closing it took an execution audit that re-ran 91 claims and
       found 30 false, then eleven fixes. The audit and every fix are in
       docs/PROJECT_STATUS.md, newest sections last.

       ── THE ONE LINE NOT MET ──────────────────────────────────
       §16 "Every AI call logged by Go with model, prompt version,
       tokens, latency, integer cost". Phase 4 has exactly one Go AI
       call site — the admin playground — and it deliberately writes no
       row, because mixing operator experiments into the usage table
       would corrupt the cost-per-request figure that table exists to
       produce. The machinery is built and tested; there is nothing to
       record until Phase 5 ships chat. The line belongs to Phase 5 and
       should move there rather than be ticked here.

       ── WHAT A READER SHOULD STILL DISTRUST ───────────────────
       The containerised `ai` service cannot reach Ollama on this
       machine (bound to 127.0.0.1). Use `task dev:ai`.

       The crisis list was reviewed on 2026-09-23 and grew from 50
       phrases to 100 across three scripts — but whether those are the
       phrasings real users write is a question only production data
       answers, and NOBODY HAS DIALLED THE HELPLINE the static response
       points at. That is the one open item on the safety path.

       memtest86+ is still unrun.

       Accuracy CLOSED at 180/200 = 90.0%, reproduced at 183/200 =
       91.5% (qwen/qwen3.8-27b via Groq, prompt v4). Hosted, not local:
       no free local model reaches the 85% bar.

       The Anthropic line is SUPERSEDED by ADR-011 — adapter removed,
       production provider deferred to Phase 7.

       ── THE CAVEAT THAT OUTLIVES EVERYTHING ──────────────────────
       🔴 THIS MACHINE HAS CONFIRMED FAILING RAM.

       `go run scripts/memcheck.go 6 45` found SEVEN single-bit flips
       on 2026-09-22 — every XOR a power of two, five inside a 4.3 KB
       span, both directions. Userspace memory does not change on its
       own.

       Fifteen-plus corruption events across this project have one
       explanation: the ephemeris kernel, a golden dasha fixture, seven
       Go linker panics, a mypy cache, a Turbopack checksum.

       EVERY NUMBER IN PHASE 4 WAS COMPUTED ON IT — the 90.0%
       classifier accuracy, the golden charts, the cost arithmetic.
       None is known wrong. None is known right.

       Re-run the gate measurements once the hardware is replaced, and
       treat THAT as the real close of Phase 4.
```

Phase 3 closed 16 of 16 on 2026-09-19. Its record is `docs/TESTING-PHASE-3.md` and
`docs/PROJECT_STATUS.md` PRs 12–30; the gate closure and its caveats are in git history
at `58fe1f2`. Three §11 security items were carried forward — see below.

---

## Phase 4 progress

| # | Task | State |
|---|---|---|
| 4.1 | `LLMProvider` / `EmbeddingProvider` protocols + registry | ✅ import-linter contract enforced and break-tested |
| 4.2 | `OpenAICompatibleProvider` (complete + stream) | ✅ 24 tests against a real httpx transport; retry classification break-tested |
| 4.3 | `OllamaEmbeddingProvider` | ✅ 8 tests; width and batch-length guards break-tested |
| 4.6 | `MockProvider` + fixtures | ✅ 17 tests; refuses unknown requests rather than inventing |
| 4.7 | PII guard | ✅ pre-existing; now enforced at registration and break-tested |
| 4.8 | Model router + per-env overrides | ✅ all 10 job types mapped, derived from the enum |
| 4.9 | Retry, timeout, circuit breaker, fallback | ✅ 12 tests on a fake clock; both breaker decisions break-tested |
| 4.10 | Prompt registry, immutable versions, `PromptBuilder` | ✅ lockfile of 8 module digests; editing one fails, break-tested |
| 4.11 | Cache-breakpoint ordering + prefix stability | ✅ ordering enforced structurally, not by convention |
| 4.4 | ~~`AnthropicProvider`~~ | **REMOVED, [ADR-011](../../docs/decisions/011-remove-anthropic-adapter.md).** Built and tested during the phase, then deleted: no subscription, and Anthropic sells no free tier |
| 4.5 | `GoogleProvider` + free embeddings | ✅ offline **and against a real key** — that run found 3 defects; see PROJECT_STATUS |
| — | `app/pricing.py` — tokens to integer micro-USD | ✅ unpriced model raises rather than costing 0 |
| 4.12 | Intent classifier + keyword pre-pass | ✅ pre-pass **100% precision at 41% coverage**, asserted in CI |
| 4.13 | Safety input classifier + crisis short-circuit | ✅ static response, never generated; startup guard |
| 4.14 | Output validator incl. `fabricated_chart_fact` | ✅ personal claims checked against the fact index |
| 4.20 | 200-message labelled intent dataset | ✅ synthetic, all 21 intents, 18 marked ambiguous |
| 4.15 | Orchestrator, stubbed context builders, telemetry envelope | ✅ crisis bypass asserted on the *provider*, not the text |
| 4.16 | OpenAPI export → generated Go client | ✅ `task contracts` regenerates; `go build` clean |
| 4.17 | `ai_request_logs` migration + persistence from the envelope | ✅ `ON DELETE SET NULL`, BIGINT cost, no content column |
| 4.18 | Typed AI client with timeout and rate limiting | ✅ concurrency-bounded; a completion is never replayed |
| 4.19 | Admin config, usage, incidents, playground | ✅ SUPER_ADMIN only, break-tested four ways |
| 4.21 | Provider parity suite | ✅ one suite, four adapters, each through its own SDK |

**1128 Python tests** in `services/ai`, `mypy --strict` clean, both import contracts kept.
Go: `go build`/`go vet` clean, unit + integration suites green.

**Intent-classifier accuracy — met on a hosted model, not on a local one.**
180/200 = 90.0% and independently reproduced at 183/200 = 91.5%, both on
`qwen/qwen3.8-27b` via Groq with prompt v4. Two runs agreeing within noise is what
makes the number worth anything on this machine.

The paragraph that used to sit here said the opposite — "one item NOT met" — and was
simply older than the measurement above it, on the same page. It is the reason the
header block now carries no per-item status at all: two places stating the same fact
is two places for it to rot, and this one rotted in under a day.

What has NOT changed: a free LOCAL model does not reach the bar. 40% on
`llama3.2:3b`, 60.5% on `qwen2.5:7b` against the spec's ≥85%. §15 predicted exactly
this, and the mitigation it named — "Groq free tier via the same adapter, one env
var" — is what produced the passing number. So the gate line is met and the §16 line
"whole AI stack runs on local free models at zero cost" is not; they are different
claims and only one of them holds.

Partial evidence already says most of the loss is **ours**: on 30 deferred messages
`llama3.2:1b` answered correctly 12 times and the product delivered 2 — ten correct
answers discarded by `MIN_CONFIDENCE = 0.6`, which is applied to a self-reported
number that small models do not calibrate.

A nine-dimension adversarial review found **39 confirmed defects in this phase's own
code**, all fixed and break-tested — including a crisis message that could reach
astrology generation, true general statements blocked as fabrications, and every
completion capped at 10s under a comment claiming 90.

Owed to the Phase 4 gate and **not closeable by the suite**:

- One real-key run per paid provider — `docs/PROVIDER-VERIFICATION.md`,
  `uv run python -m scripts.verify_provider google` — done 2026-09-21, and it
  found three defects. `verify_provider anthropic` no longer exists (ADR-011).
- The ≥85% intent-accuracy number — `uv run python -m scripts.measure_intent_accuracy`
  against local Ollama. CI asserts the keyword pre-pass only, which is the half that
  is pure code.
- **A human must dial each crisis helpline number** in
  `services/ai/app/safety/responses/`. No test can check a phone number is correct,
  and a wrong one costs someone the single attempt they were willing to make.

---

## Carried from Phase 3

- **§11.9 CSP `script-src 'unsafe-inline'`** — still unmet, and the SRI escape route was
  **tested and does not work**. `experimental.sri` was enabled and `'unsafe-inline'`
  removed: SRI emits integrity attributes for EXTERNAL scripts, but Next's RSC payload
  rides in two INLINE `<script>` tags that carry none, so `script-src 'self'` blocks them
  and the app does not hydrate — confirmed by `csp.spec.ts:52`, the repo's own test,
  which fails on a click timeout. Reverted. The remaining exit is a per-request nonce,
  which costs static rendering and CDN caching; PHASE-05 carries the blocking item.
- **§11.5 Chrome sandbox / network** — ✅ **network half closed 2026-09-22.**
  `resolverRules` gives the browser `MAP * ~NOTFOUND` with an EXCLUDE for only the host
  being printed, so a compromised print page cannot reach a collector. Fails CLOSED on a
  malformed URL. Break-tested three ways. `--no-sandbox` remains a deployment constraint
  and is documented as one.
- **§11.8 profile-label injection** — ✅ closed by PR 30.
- ~~**CI has never executed a job.**~~ **RESOLVED 2026-09-22.** It was an exhausted
  Actions quota, proven rather than guessed: jobs ran 07:31:53 → 07:31:56 with
  `"steps": []` — three seconds, zero steps, all eleven. The repo is public now, so
  Actions minutes are free, and **all 11 jobs pass**. Getting there took three real
  fixes, none of them cosmetic: the worker died because `.env.example` names a Chrome
  that does not exist on a runner; two tests searched a gazetteer for a city the CI
  fixture does not contain; and a visual snapshot was reporting a genuine cross-OS
  layout defect (the ℞ mark is in no bundled font, so its width — and the table column
  behind it — changed with the reader's operating system).

  This was described here as "the largest caveat on everything", and it was right: the
  first hour CI could run, it caught `ai_request_logs` missing from the data-subject
  export, which `task verify` structurally cannot see because it does not run the
  integration suite.
- `< 180 KB` first-load JS — unmet by decision, ADR-010.
- ~~No e2e drives PDF or share links; no share-management screen; no ADR for
  hand-rolled auth.~~ **All three closed 2026-09-22/23.**
  `tests/e2e/pdf-and-shares.spec.ts` (9 tests, four guards mutation-proven),
  `apps/web/src/app/settings/shares/page.tsx` (+ 11 unit tests), and
  `docs/decisions/012-hand-rolled-auth.md`.
  The share screen was not a cosmetic gap: `listShares` and `revokeShare` had zero
  call sites, so a user could mint a 30-day bearer link to a birth chart and had no
  way to see or revoke it — and revocation is the owner's only remedy.
- `memtest86+` unrun. Still true, and still the caveat below.

---

### Phase 3 detail, kept as the evidence trail

Phase 2 closed with 20 of 20. Its record is `docs/TESTING-PHASE-2.md`, which lists what
was *run* rather than what was read, and names the twenty-three guards broken on purpose
to prove they fire. Phase 3 is being held to the same standard.

---

## Closed (16)

| # | Item | Evidence |
|---|---|---|
| 1 | Complete correct chart for all 30 fixtures | 30 fixtures, 153 assertions, `ChartSVG.fixtures.test.tsx` |
| 2 | Both styles correct; switcher persists | unit + e2e; 108 e2e specs green against the real stack |
| 3 | D1, D9, D10 viewable | `VargaSwitcher`, varga tests |
| 4 | Geometry in a pure React-free package | `packages/astrology-geometry`, no React reference |
| 5 | Planet table + house view, responsive, detail sheets | unit + e2e at 360px |
| 6 | Dasha timeline, current period, drill-down | `DashaTimeline.test.tsx`, e2e |
| 7 | Yogas in en and hi | e2e asserts the corpus directly, not just the dictionary |
| 8 | Transits and Sade Sati with phase **and dates** | PR 17 — engine window exposed, stored, served |
| 9 | Glossary covers every term used | `astro-term-usage.test.ts` |
| 10 | PDF via the asynq worker, signed URL | PR 12 |
| 11 | Another user's PDF rejected | PR 12/13, break-tested |
| 12 | Visual regression suite green | PR 16 — 15 baselines, 3 viewports, stable over 3 runs |
| 13 | Manual keyboard and screen-reader pass | `docs/TESTING-PHASE-3.md` — closed 2026-09-19, six defects found and fixed first |
| 14 | Performance budgets met and CI-enforced | ADR-010 — per-route regression budgets enforced, exit 1 on breach (break-tested); the spec's 180 KB recorded as unmet |
| 15 | `task verify` green | run at each PR |
| 16 | `PROJECT_STATUS.md` and this file updated | both current |

---

## Phase 3 gate: closed

All sixteen items are closed. Two of them are worth reading rather than counting:

**13 — the manual a11y pass** was not a formality. Q1 came back *"No, I would not have
found it"* and Q2 came back *"correct but confusing"* in three places. Six defects, all
fixed before the item closed, and three of them were in the inspector rather than the
product — a tool that made correct markup look broken. `docs/TESTING-PHASE-3.md` records
what was judged by a person and what was only measured, because those are not the same
evidence.

**14 — the performance budget** closed by a decision, not by reaching 180 KB. The route
is 201.0 KB and the framework floor under it is 159.5 KB, so the target leaves ~20 KB for
the whole product; deleting every removable byte, including the entire Hindi dictionary,
lands at 183.0 KB. ADR-010 replaces it as the gate criterion with the per-route
regression budgets, which fail the build on breach — verified by setting one below the
measured size and watching `npm run budget` exit 1.

The spec's number is **not** deleted. `specTargetKB: 180` still prints on every run and
is still recorded below as unmet. A target quietly moved to whatever was achieved hides
the gap; an unmet one stated plainly keeps it visible.

### The other two checklists in the spec

§15 is the gate, and it is the thing CLAUDE.md blocks Phase 4 on. The spec also carries a
**§11 Security checklist** and a **§13 Definition of Done**, and those are not all ticked.
Audited 2026-09-19 by executing the checks rather than reading them:

**§11 — three items not fully met**

| Item | State |
|---|---|
| 11.9 CSP allows inline SVG **without** allowing inline script | ❌ **Unmet.** `script-src 'self' 'unsafe-inline'` is live on every response, verified with `curl -I`. Inline script is allowed. |
| 11.8 A user-supplied profile label cannot inject markup | ⚠️ **Untested.** React escapes by default so it is probably true, but no test asserts it — and "probably true" is what §11 exists to replace. |
| 11.5 `chromedp` sandboxed, no network beyond the print route | ⚠️ **By argument, not by mechanism.** `--no-sandbox` is explicitly set (documented: container without user namespaces) and no network restriction is implemented. The reasoning in `chrome.go` is sound — it only ever loads our own print page — but nothing enforces it. |

The other seven verified met: ownership 404s, signed short-lived PDF URLs with no PII in
the path, pre-render authorisation, single-use print token (`printtoken.go` — redeeming
deletes it), no birth data in analytics or query strings, server-side share resolution,
and PDF rate limiting (`RenderLimit`).

**§13 — two items with caveats, both already reasoned**

- *"3 viewports × 2 themes"* → 3 viewports × **1** theme. Deliberate, and the reasoning is
  written into `visual.spec.ts`: the second theme does not exist, the app is dark-only, and
  screenshotting a theme no user can select would lock in appearance nobody sees.
- *"Performance budgets met and CI-enforced"* → budgets exist and fail the build (exit 1,
  break-tested). **CI-enforced is true in configuration and false in practice**, because CI
  has never run.

None of these reopen the gate. All three §11 items should be closed before Phase 5, which
is where the CSP one starts actively blocking work.

### What "16 of 16" does not mean

**Every check in this repo is local.** CI has never executed a single job — the runner
refuses at dispatch over account billing — so "green" throughout this phase means *green
on one developer's machine*. Eight PRs were merged over red checks that never started.

That is not a gate item and it does not reopen one. It is the largest caveat on
everything above, and it should be fixed before Phase 4 makes the codebase bigger: a
budget that fails the build, a guard that fires, a suite that passes — none of them
constrain anybody until a machine other than this one runs them.

---

## Unmet spec items, carried forward

- **`< 180 KB` first-load JS on the kundli route** (PHASE-03 §7). Currently 201.0 KB.
  Not a product-code problem — see ADR-010. Revisit if the framework floor moves.

---

## Also outstanding (not gate items)

- ~~Task 3.17 — Storybook.~~ Done: 17 stories, rendered in the unit suite, built in CI.
- ~~Task 3.18 — loading/error/empty.~~ Done: `tests/e2e/kundli-states.spec.ts` drives all
  three on all five Kundli routes.
- **CI has never executed a job.** 👤 Every check in this repo is local. The runner
  refuses at dispatch: *"recent account payments have failed or your spending limit needs
  to be increased."* Steps in `docs/MANUAL-A11Y-PASS.md` §CI.
- **No end-to-end test drives PDF or share links through a browser.** Each seam is covered
  from both sides; the join is not.
- **Share-management screen.** Links can be created from the sheet and revoked through the
  API, but nothing lists them.

## Carried debt

### The Go linker panics on a stale build cache (seen twice, 2026-09-21)

`task verify` failed twice with the linker crashing rather than a test failing:

```
panic: bad alignment value
panic: runtime error: index out of range [33554444] with length 35
        cmd/link/internal/loader.(*Loader).resolve
```

Both times on the largest test binary (`internal/httpapi.test`), both times with 27 GB
free and no OOM — so not memory pressure. `go clean -cache` fixed it both times and the
same commit then passed.

`mypy` failed the same way in the same session (`ValueError: reading past the buffer
end` from `mypy/cache.py`), fixed by `rm -rf .mypy_cache`.

**If `task verify` fails with a panic inside the toolchain rather than an assertion,
clear the caches before believing it is your code.** ADR-007 already pins the Go
toolchain because this machine's system Go has a corrupted stdlib byte; this looks
related and is worth a proper diagnosis before it costs somebody an afternoon.


- Web CSP still has `script-src 'unsafe-inline'` (from Phase 1; blocks a **Phase 5** item).
- No ADR for hand-rolled auth.
- `memtest86+` unrun, against nine recorded data-corruption events. 👤
  *(Not ten. See the correction below — I briefly logged a tenth and was wrong.)*

## `ayana test` rebuilt the app underneath its own server (2026-09-19)

`./scripts/ayana test` failed with ~40–60 red tests while `npx playwright test` passed
**148/148 on the same commit**. The difference is that `ayana test` runs `task verify`
first, and `task verify` builds the web app — underneath the `next start` that Playwright
is about to drive.

`next start` holds its build manifest in memory. After the rebuild it keeps serving HTML
that references the **old** chunk hashes, and those files are gone. So the server is
healthy by every obvious measure — `/auth` returns 200 for the whole run, same pid
throughout — and no JavaScript loads. The failures land far downstream of the cause: an
OTP request that never reaches the API, a dynamic import that never mounts, a URL that
never changes.

Fixed by calling `cmd_restart_web` after `task verify`, before Playwright.

### Correction

I diagnosed this three times before getting it right, and the middle two were wrong:

1. **Rebuild underneath the server** — correct, but I discarded it after a bad experiment.
2. **Memory corruption.** I read a `Segmentation fault` in `web.log` plus same-afternoon
   faults in Chrome and `pysemgrep`, and logged it here as corruption event #10. That
   entry has been removed. The failure reproduces deterministically, which corruption does
   not, and the Chrome `trap invalid opcode` I cited is especially weak evidence — Chrome
   compiles `CHECK()` failures to a deliberate `ud2`.
3. Back to (1), confirmed by `web_build_check` reporting `STALE BUILD — …/3c9xdf586ehwi.js
   returned 500`.

The experiment that sent me down the wrong path: I watched port 3000 during a rebuild and
saw `/auth` return 200 for sixty seconds straight, and concluded the server was unaffected.
It was — the server was never the problem. I had checked the HTML and not the chunks, which
is the one thing this specific failure does not touch. `web_build_check` gets this right
because it extracts **every** chunk with `sort -u`; sampling one gives the framework bundle,
whose hash does not move between builds.

The original `Segmentation fault` remains unexplained and is plausibly the same cause — a
process reading `.next` files replaced under it — but that is a guess, not a finding, and
nothing here depends on it.
