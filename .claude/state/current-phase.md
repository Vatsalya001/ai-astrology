# Current phase

```
Phase: 2 — Astrology Engine
Gate:  ✅ CLOSED  (20 of 20 gate items, 8 of 8 Definition-of-Done items)
       Nothing open.
```

Evidence for every item: `docs/TESTING-PHASE-2.md`. It records what was *run*, not
what was read, and it names the twenty-three guards that were deliberately broken to
prove they fire.

**The Phase 0 and Phase 1 lesson held a third time, and this was the worst of the
three.** Reading the code said the chart API worked. Running it found that the
containerised `astro-service` had **no ephemeris kernel at all** — the Dockerfile
copied `app` and not `data` — so every chart request 500'd while the container
reported healthy, for an entire phase. Nothing caught it because no test had ever
computed a real chart through the whole stack. The end-to-end suite that task 2.21
asks for is what found it, on its first run.

Also found only by running it: `ayana up` serving a months-old image, a D9 that was a
relabelled D1, 819 database round trips per chart, and a data export that never
mentioned birth profiles or charts.

## What Phase 3 inherits

**The chart pipeline is whole.** Birth details → versioned profile → UTC instant from
real historical tzdata → astro-service → D1 and D9 stored in one transaction with an
819-node dasha tree → served from storage, including while astro is stopped.

**Three rules the next phase must not break.**

1. **A chart is a pure function of its inputs.** That is why a stored one is served
   without an HTTP call and is not "stale". If Phase 3 adds anything time-dependent to
   a chart, that assumption dies and the caching design dies with it.

2. **The transit table has no `user_id`, and must not gain one.** A planet's sign at an
   instant is the same for everybody; only the house differs, and that is an integer
   subtraction done at read time. It is what makes Sade Sati answerable during an
   outage.

3. **Golden files are frozen, not regenerated.** `scripts/generate_golden.py` refuses to
   run unless `test_external_reference.py` passes. If a golden file changes and that
   suite still passes, the change is in chart assembly and a human has to say why.

## Numbers measured at the gate

| Budget | Measured |
|---|---|
| Chart computation < 200 ms | p95 70.6 ms |
| Go round trip < 350 ms cold | 109–140 ms |
| Cached read < 10 ms | p95 8.5 ms, max 10.0 ms |
| Place search < 50 ms p95 | 2.7 ms |

The cached read is the one to watch: it is at its budget at the maximum, and Phase 3
puts a chart on every page load.

## Carried debt

- Web CSP still has `script-src 'unsafe-inline'` (from Phase 1; blocks a **Phase 5**
  gate item).
- No ADR for hand-rolled auth (from Phase 1).
- `memtest86+` unrun — nine data-corruption events on this machine, all recovered,
  none explained. Owner action.
- Production place seeding needs GeoNames' `admin1CodesASCII.txt`, or subdivisions
  display as codes. The seeder warns; the E2E fixture ships a mapping.

## Next: Phase 3 — Kundli UI

Spec: `docs/specs/PHASE-03-KUNDLI-UI.md`

Phase 2 deliberately built **data entry only**. Phase 3 is the visualisation: the North
and South Indian chart SVGs, the dasha timeline, the PDF worker.

Two things that will decide whether it is any good:

1. **The chart SVG needs per-element `aria-label`s and a visually-hidden table
   duplicating the data.** An SVG is meaningless to a screen reader otherwise, and this
   is in the frontend rules already.
2. **The PDF is an `asynq` worker with `chromedp`, never in the request path.** The
   queue exists now and the transit refresh is the worked example.
