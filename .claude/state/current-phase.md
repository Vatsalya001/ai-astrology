# Current phase

```
Phase: 3 — Kundli UI
Gate:  🔶 OPEN  (14 of 16 gate items closed)
       Two open. One needs a human; one needs a decision.
```

Phase 2 closed with 20 of 20. Its record is `docs/TESTING-PHASE-2.md`, which lists what
was *run* rather than what was read, and names the twenty-three guards broken on purpose
to prove they fire. Phase 3 is being held to the same standard.

---

## Closed (13)

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
| 15 | `task verify` green | run at each PR |
| 16 | `PROJECT_STATUS.md` and this file updated | both current |

---

## Open (2)

### 13 — manual keyboard and screen-reader pass 👤 **needs a human**

The automated half is done: axe runs across five e2e specs and reports zero violations
on every Kundli route. The gate asks for a manual pass as well, and it is right to — axe
cannot tell whether a focus order makes *sense*, or whether the chart's spoken output is
comprehensible rather than merely present.

Steps are in `docs/MANUAL-A11Y-PASS.md`.

### 14 — performance budgets met

Enforced regression budgets ship and fail the build (PR 15). The spec's **180 KB**
target does not pass: `/kundli/chart` is 199.0 KB gzipped, and the framework floor —
React, react-dom and the Next client runtime, before any Ayana code — is **159.5 KB**.
That leaves ~20 KB for the whole product, and the i18n dictionaries alone are 14.8 KB.

Closing it means shipping one locale's dictionary instead of both (~15 KB, still short),
or a decision about the framework. Neither is a Phase 3 call.

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
