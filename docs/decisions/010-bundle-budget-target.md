# ADR-010 — The 180 KB bundle target is replaced by enforced regression budgets

**Status:** accepted · 2026-09-19

## Decision

`PHASE-03-KUNDLI-UI.md` §7 asks for **< 180 KB gzipped** of first-load JS on the
kundli route. That target is **not met and will not be met**, and it is hereby
replaced — as the gate criterion — by the per-route regression budgets in
`apps/web/bundle-budget.json`, which ship, run in CI and fail the build.

The spec's number stays in the file as `specTargetKB`. It is reported on every
run and it is not enforced. Phase 3's gate item 14 closes on the regression
budgets being in place and green, not on 180 KB being reached.

## Context

Measured on 2026-09-19, gzipping each first-load chunk separately:

| route | first-load JS |
|---|---|
| `/home` | 201.5 KB |
| `/kundli/chart` | **201.0 KB** |
| `/kundli/yogas` | 199.3 KB |
| `/kundli/planets` | 198.4 KB |

The floor beneath those — React, react-dom and the Next client runtime, before a
single line of this product — is **159.5 KB**. Two chunks account for 112 KB of
it and contain no product code at all.

So 180 KB leaves roughly **20 KB for the whole of Ayana**. The i18n dictionaries
alone are 14.8 KB.

### It is not reachable by trimming product code

This was measured rather than estimated. Removing *every* removable byte —

- the glossary prose (8.9 KB), needed only on tap
- the **entire** Hindi dictionary (7.1 KB)

— lands at **183.0 KB**. Still over, having deleted a whole locale and the
feature that makes the vocabulary approachable, which are two of the things the
phase exists to deliver.

### Why the target was wrong rather than the implementation

The 180 KB was written into the spec before the framework was chosen. It is a
number about a product, applied to a stack whose fixed cost is 89% of it. A
budget that no product-code change can satisfy does not constrain product code;
it just fails, permanently, and teaches everyone to ignore the budget line.

## Options considered

**A. Ship one locale's dictionary.** Saves ~15 KB and still misses at ~186 KB.
Costs Hindi — in a Vedic astrology product, for an Indian audience. Rejected: it
pays a real product price for a target it does not even reach.

**B. Replace the gate criterion with the enforced regression budgets.**
*Chosen.*

**C. Defer it out of Phase 3 as a framework decision.** Leaves the phase open on
a question nobody intends to answer this quarter, and leaves the regression
budgets — which already work — unrecognised. Rejected as bookkeeping rather than
a decision.

A fourth option, *change the framework*, is not a Phase 3 call and is not
foreclosed by this ADR.

## Consequences

**What is enforced.** Each route carries its measured size plus a small
allowance. An import that drags in a date library or a second copy of a corpus
fails the build **on the PR that adds it**. That is the thing a CI budget can
actually deliver, and it is delivered.

**Lowering a number in `bundle-budget.json` is always allowed. Raising one is a
decision that must appear in a diff with a reason beside it.** That asymmetry is
the whole mechanism — without it the budgets ratchet upward silently and mean
nothing.

**The spec number is not deleted.** `specTargetKB: 180` stays, every run prints
which routes exceed it, and `PROJECT_STATUS.md` records it as an unmet spec item.
A target quietly redefined to whatever was achieved is worse than an unmet one
stated plainly: the first hides a gap, the second keeps it visible for whoever
revisits the framework question.

**This does not make the bundle acceptable forever.** It makes the gate honest.
If the framework floor moves — React Server Components trimming the client
runtime, or a different framework — the right response is to lower these numbers
toward the spec, not to treat 205 KB as the new normal.
