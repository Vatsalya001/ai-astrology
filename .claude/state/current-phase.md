# Current phase

```
Phase: 4 — AI Infrastructure
Gate:  🔶 OPEN  (0 of 19 gate items closed)
       21 tasks. Nothing is exposed to users in this phase.
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
| 4.4, 4.5, 4.12–4.21 | | not started |

**164 Python tests** in `services/ai`, `mypy --strict` clean, both import contracts kept.

---

## Carried from Phase 3

- **§11.9 CSP `script-src 'unsafe-inline'`** — unmet. Blocks a Phase 5 gate item, and
  PHASE-05 already carries a blocking item to replace it with a nonce or SRI before the
  first model response is rendered. Not a Phase 4 concern.
- **§11.5 Chrome sandbox / network** — `--no-sandbox` is a deployment constraint; the
  network-restriction half is implementable and not yet done.
- **§11.8 profile-label injection** — ✅ closed by PR 30.
- **CI has never executed a job.** Still the largest caveat on everything: every check in
  this repo is local. Ten PRs have now been merged over checks that never started.
- `< 180 KB` first-load JS — unmet by decision, ADR-010.
- No e2e drives PDF or share links; no share-management screen; no ADR for hand-rolled
  auth; `memtest86+` unrun.

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
