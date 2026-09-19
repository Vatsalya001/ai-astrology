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
**Method:** in-page inspector (`?a11y=1`)
**Browser:**

**Tab presses to reach the Moon:**

- `/kundli/chart` — **not by Tab at all.** The page has 8 tab stops and the data table is
  not one of them (`tabindex` is absent, deliberately). A screen-reader user reaches it
  by table navigation, which is the correct affordance; a 10-row table as 60 tab stops
  would be worse. Verified in a browser.
- `/kundli/planets` — **6 presses** to the Moon's row trigger. Verified in a browser.

---

> ### How this document is filled in
>
> Q1–Q4 below each have two parts:
>
> - **Measured** — filled in already, from assertions run in a real browser. Every claim
>   names what was executed. Do not re-derive these; check them if you like.
> - **Your judgement** — the part no measurement answers. That is what is left.
>
> The gate item exists for the judgement. The measurements are here so you are judging
> comprehensibility rather than re-checking facts.

---

### Q1 — Can you answer "which sign is my Moon in, and which house?" from the panel alone

1. Open `http://localhost:3000/kundli/chart?a11y=1`
2. In the panel (bottom right), click **The data table**
3. Read the section **"The data table, row by row"**

**Measured.** The panel prints, for the 26 Feb 2003 Prayagraj profile:

```
Moon, Sign: Sagittarius, House: 4th, Degree: 21 degrees, Nakshatra: Purva Ashadha, pada 3
```

All ten rows are present, each value prefixed by its column header — which is how a
screen reader reads a table cell. Cross-checked against `/kundli/planets` and against an
independent recomputation from the stored birth instant.

> **Do not use a number written here as the expected answer.** An earlier version of this
> question asserted "Sagittarius, 2nd house", which was true of a synthetic fixture and
> false of the profile actually loaded — so following it literally marked a *correct*
> panel as failed. The answer depends on whose chart is open.

**Your judgement** — you clicked one button and read one line. Was the answer *findable*,
or did you have to know where to look?

- [ ] Yes, without guessing
- [ ] Yes, but it took a long time — say where you got stuck:
- [ ] No — say what was missing:

**Notes:**

### Q2 — Does the reading order make sense?

Listen to the whole page top to bottom. The facts are all there; the question is whether
the *order* tells a story or jumps around.

1. On any Kundli route with `?a11y=1`, click **Read the page in order**
2. Read the transcript. ▸ marks structure; 🔊 marks text that exists only in the audio

**Measured.** The visual position of every heading, control, table and landmark was
compared against its DOM position on all five Kundli routes: **zero mismatches**. The DOM
order and the visual order agree everywhere.

`/kundli/chart` linearises to 32 lines — h1, each switcher followed by its own
explanation, the chart group with its summary, the ten table rows, then the three
actions. `/kundli/transits` reads Sun → Moon → Mars → … in traditional graha order, with
retrograde spoken as a word and Sade Sati stated as a full sentence.

**Your judgement** — measurement proves the order is *consistent*. It cannot prove the
order is *sensible*. Read one transcript end to end and say whether it tells a story.

- [ ] It follows the visual order and reads naturally
- [ ] It is correct but confusing — say where:

**Notes:**

### Q3 — Anything announced as "graphic", "button" or "clickable" with no useful label?

1. In the **Read the page in order** transcript, scan for `▸ button —` or `▸ link —`
   with nothing useful after the dash, or a generic label where data should be

**Measured.** No unlabelled control exists on any of the five routes — every interactive
element has a non-empty accessible name, which is why axe's `button-name` and `image-alt`
are green. The chart SVG is entirely `aria-hidden` with a captioned data table as the
readable path.

One defect of the *opposite* kind was found and fixed (PR 27): every Nakshatra cell
announced "What “Nakshatra” means" instead of its value, because the glossary button's
`aria-label` overwrote the data it wrapped. Confirmed against three independent accname
implementations; the Moon's row now announces `… 20°44' 4th Purva Ashadha 3. What
“Nakshatra” means —`.

**Your judgement** — is any remaining label *unhelpful* even though it exists?

**Notes:**

### Q4 — Anything where colour was the only signal?

Examples to check: the current dasha period, a yoga's strength, the Sade Sati phase.

The decisive check, if you want to do it yourself: Chrome devtools → `Ctrl+Shift+P` →
"Emulate vision deficiencies" → **Achromatopsia**, then walk the five routes.

**Measured.** Eight colour-carrying elements across the five routes; **every one has a
non-colour cue**. Verified by removing colour entirely (`filter: grayscale(1)`) and
asking the three questions named above:

| | with colour removed |
|---|---|
| current dasha | `Moon mahadasha, Jan 2018 – Jan 2028, Current period` |
| yoga strength | the words **Strong** / **Moderate**, visible and in the label |
| Sade Sati | "Not currently running. Saturn is in Pisces, the 4th sign from your Moon." |
| retrograde | `℞` glyph, `aria-hidden`, plus visually-hidden "retrograde" |

**Your judgement** — with colour removed, was anything *harder to notice* even though it
was technically present?

**Notes:**

---

## Outcome

- [ ] **Item 13 CLOSED** — the chart is comprehensible by ear
- [ ] **Issues found** — listed above; they are ordinary bugs and can be fixed

Once ticked, update `.claude/state/current-phase.md` — move item 13 from Open to Closed
and point it at this file.
