# Phase 3 — what was run

Evidence for the Phase 3 gate. Like `TESTING-PHASE-2.md`, it records what was
**executed**, not what was read.

---

## Automated (already done)

| Suite | Count | Command |
|---|---|---|
| Web unit | 577 | `npm run test --workspace=web` |
| Go unit + integration | 18 packages | `go test -tags integration -race ./...` |
| Python | 308 | `uv run pytest` (in `services/astro`) |
| End-to-end, real browser | 145 | `npx playwright test` |
| Visual regression | 15 baselines | part of the e2e suite |
| Keyboard + screen-reader facts | 11 | `tests/e2e/kundli-keyboard.spec.ts` |

Two accessibility bugs were found by writing that last suite, both invisible to axe:
the skip link did not move focus (`<main>` was not focusable), and no dialog returned
focus to the control that opened it. Both fixed in PR 21.

---

## How to run the human pass

**No screen reader needed.** The page has an inspector built in — add `?a11y=1` to any
URL and a panel appears showing what a screen reader *would* announce, the focus order
numbered, and every live-region announcement as it fires.

```
http://localhost:3000/kundli/chart?a11y=1
```

Use the **jump buttons** in the panel (*Start of page*, *Main content*, *The chart*,
*The data table*) rather than clicking into the page, then press `Tab`.

---

## The human pass — FILL THIS IN

Everything above proves the facts are *present*. It cannot prove they are *followable*.
That is the judgement below, and it is the last thing standing between Phase 3 and a
closed gate.

**Date run:**
**Method:** in-page inspector (`?a11y=1`) / Orca / other:
**Browser:**

**Tab presses to reach the Moon:** ____ (the panel numbers every stop)

### Q1 — Can you answer "which sign is my Moon in, and which house?" from the panel alone

Open `http://localhost:3000/kundli/chart?a11y=1`, click **The data table**, then press
`Tab` and read the panel — not the page. The true answer is **Sagittarius, 2nd house**.

- [ ] Yes, without guessing
- [ ] Yes, but it took a long time — say where you got stuck:
- [ ] No — say what was missing:

**Notes:**

### Q2 — Does the reading order make sense?

Listen to the whole page top to bottom. The facts are all there; the question is whether
the *order* tells a story or jumps around.

- [ ] It follows the visual order and reads naturally
- [ ] It is correct but confusing — say where:

**Notes:**

### Q3 — Anything announced as "graphic", "button" or "clickable" with no useful label?

**Notes:**

### Q4 — Anything where colour was the only signal?

Examples to check: the current dasha period, a yoga's strength, the Sade Sati phase.

**Notes:**

---

## Outcome

- [ ] **Item 13 CLOSED** — the chart is comprehensible by ear
- [ ] **Issues found** — listed above; they are ordinary bugs and can be fixed

Once ticked, update `.claude/state/current-phase.md` — move item 13 from Open to Closed
and point it at this file.
