# The two things a machine cannot do

Two Phase 3 items need a person. Everything else in the gate is automated and green.

This document is the script for both. It is written to be followed rather than read:
each step says what to do, what should happen, and what to write down if it does not.

---

## 1. Manual keyboard and screen-reader pass 🔑

**Gate item 13.** Why it cannot be automated: axe runs on five e2e specs and reports zero
violations on every Kundli route, which proves there is no *detectable* violation. It
cannot tell you whether the focus order makes sense, whether the chart's spoken output is
comprehensible, or whether a screen reader user could actually find what they came for.
Those are judgements.

Budget about 40 minutes.

### Before you start

```bash
cd ~/Desktop/astro
./scripts/ayana up          # the whole stack; wait for "worker consuming the queue"
./scripts/ayana status      # every line should be ✓
```

Then sign up at <http://localhost:3000> and add a birth profile, so there is a real chart
to navigate. Any details will do.

### 1a — Keyboard only

**Unplug or ignore your mouse.** That is not theatre: the failure mode is reaching for it
without noticing, and then reporting a pass for a flow you did not actually complete.

| Step | Do | Expect |
|---|---|---|
| 1 | Load `/kundli/chart`. Press `Tab` once. | A visible "skip to content" link appears. |
| 2 | Press `Enter` on it. | Focus lands on the main content, not back at the top. |
| 3 | Keep pressing `Tab` through the whole page. | The focus ring is **always visible**. Never lose track of where you are. |
| 4 | Note the order things receive focus. | It should follow the visual order down the page. Flag anything that jumps. |
| 5 | Reach the varga switcher (D1/D9/D10). Change it with arrow keys. | The chart reloads **and focus stays on the switcher** — it must not jump to the top. |
| 6 | Reach the style switcher. Change it the same way. | Same: the diagram redraws, focus stays. |
| 7 | `Tab` into the chart itself. | Each planet is reachable and focusable. |
| 8 | Press `Enter` on a planet. | A detail sheet opens **and focus moves into it**. |
| 9 | Press `Escape`. | The sheet closes **and focus returns to the planet you opened it from** — not to the top of the page. |
| 10 | Go to `/kundli/dashas`. `Tab` to a mahadasha bar, press `Enter`. | It drills down; focus is somewhere sensible. |
| 11 | Go to `/kundli/planets`. `Tab` through the table. | Every row is reachable; the sheet opens and closes as in 8–9. |
| 12 | Open the share sheet, `Tab` inside it. | Focus is **trapped** in the dialog — `Tab` from the last control returns to the first, not to the page behind. |
| 13 | Press `Escape` in the share sheet. | It closes and focus returns to the Share button. |

**Write down:** any step where focus vanished, jumped unexpectedly, or where you could not
tell what was focused.

### 1b — Screen reader

Use whichever you have. On Linux, Orca (`orca` — usually preinstalled on GNOME). On macOS,
VoiceOver (`Cmd+F5`). On Windows, NVDA (free).

Turn it on, close your eyes or turn off the monitor for the chart step. That is the test.

| Step | Do | Expect |
|---|---|---|
| 1 | Navigate to `/kundli/chart` and listen to the page. | The heading structure makes sense. You can tell what screen you are on. |
| 2 | Find the chart. | It is announced as a group with a summary sentence — not as "graphic" or silence. |
| 3 | Navigate into it. | Each planet announces its **name, sign, degree, house and nakshatra** as a sentence. |
| 4 | Find the visually-hidden table. | Use table navigation. The same data is there, captioned so the repetition reads as deliberate. |
| 5 | **The real question:** with your eyes closed, could you answer "which sign is my Moon in, and which house?" | Yes, without guessing. |
| 6 | Go to `/kundli/dashas`. | The current period is announced as current — not conveyed by colour alone. |
| 7 | Go to `/kundli/transits`. Find Sade Sati. | The phase is spoken as a word, and the dates are read out. |
| 8 | Trigger an error (stop the API: `./scripts/ayana stop api`, reload). | The error is **announced**, not silently rendered. |

**Write down:** anything you could not work out by listening, anything announced as
"graphic" or "button" with no label, anything where colour was the only signal.

### Recording the result

Create `docs/TESTING-PHASE-3.md` with what you actually ran and what you found — the same
shape as `docs/TESTING-PHASE-2.md`. Then in `.claude/state/current-phase.md`, move item 13
from Open to Closed with a pointer to it.

If you find problems, paste them into a session and they can be fixed — they are ordinary
bugs.

---

## 2. Get CI running 💳

**Not a gate item, but it undermines all of them.** Every check in this repository has
only ever been run on your machine. CI has never executed a single job.

### What is wrong

Every workflow run is refused at dispatch:

> The job was not started because recent account payments have failed or your spending
> limit needs to be increased.

This is a billing state on the GitHub account, not anything in the repository. The
workflow file is valid — it has simply never been allowed to start.

### Steps

1. Go to <https://github.com/settings/billing> while signed in as **Vatsalya001**.
2. Under **Payment information**, check for a failed charge or an expired card. Update it
   if so.
3. Under **Spending limit** → **Actions**, confirm the limit is not `$0`. A private
   repository consumes Actions minutes; a zero limit blocks every run once the free
   allowance is used.
4. Go to <https://github.com/Vatsalya001/ai-astrology/actions> and press **Re-run all
   jobs** on the most recent run.

### What to expect on the first real run

It will probably fail, and that is fine — it has never run. The likely causes, in order:

- **Playwright baselines.** The visual suite's screenshots are committed as
  `*-chromium-linux.png`. CI is linux/chromium, so they should match. If they differ by a
  small ratio, the runner's font rendering differs; re-generate them **on CI**, not
  locally, and never on a Mac.
- **Timeouts.** The integration suites start Postgres and Redis containers. A cold runner
  is slower than this machine.
- **The `astro` service image build.** It vendors a 16 MB JPL kernel.

Paste whatever it says into a session and it can be worked through.

---

## 3. One more, whenever you have a spare evening 🧠

`memtest86+`, overnight. The repository records **nine data-corruption events**, and one
of them this month silently flipped two bits in a golden test fixture — 220 bytes apart,
which is the signature of bad memory rather than a bad edit. `scripts/check-integrity.sh`
exists because of it.

That check currently reports only files you have edited, which is expected. On a clean
tree it should report nothing:

```bash
git stash && bash scripts/check-integrity.sh ; git stash pop
```

If it ever names a file you did **not** touch, the bytes on disk are damaged and git
cannot see it. Restore with `rm <path> && git checkout -- <path>` — a plain `git checkout`
will not do it, because git believes the file is already correct.
