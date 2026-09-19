# Current phase

```
Phase: 3 — Kundli UI
Gate:  🔶 OPEN  (15 of 16 gate items closed)
       One open, and it needs a decision rather than work.
```

Phase 2 closed with 20 of 20. Its record is `docs/TESTING-PHASE-2.md`, which lists what
was *run* rather than what was read, and names the twenty-three guards broken on purpose
to prove they fire. Phase 3 is being held to the same standard.

---

## Closed (15)

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
| 15 | `task verify` green | run at each PR |
| 16 | `PROJECT_STATUS.md` and this file updated | both current |

---

## Open (1)

### 14 — performance budgets met ⚖️ **needs a decision, not work**

Enforced regression budgets ship and fail the build (PR 15). The spec's **180 KB**
target does not pass: `/kundli/chart` is **201.0 KB** gzipped (measured 2026-09-19), and
the framework floor — React, react-dom and the Next client runtime, before any Ayana code
— is **159.5 KB**. That leaves ~20 KB for the whole product, and the i18n dictionaries
alone are 14.8 KB.

It is not closeable by optimisation. Stripping the glossary prose (8.9 KB) **and** the
entire Hindi dictionary (7.1 KB) — every removable byte — lands at 183.0 KB, measured.

Three options, and this is a decision rather than work:

- **A.** Ship one locale's dictionary. Saves ~15 KB, still misses, and costs Hindi.
- **B.** Revise the target to the enforced regression budgets that already ship and fail
  the build, recording the spec number as unmet. *Recommended: the 180 KB was written
  before the framework was chosen, and a budget no product-code change can meet is not a
  budget.*
- **C.** Treat it as a framework decision and defer it out of Phase 3 entirely.

**This is the only thing standing between Phase 3 and a closed gate.**

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
