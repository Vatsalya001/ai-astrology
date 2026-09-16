# Phase 2 — what was run

Evidence for the Phase 2 gate. It records what was **executed**, not what was read, and
it names the guards that were deliberately broken to prove they fire.

The Phase 0 and Phase 1 lesson held a third time: **reading the code and running it
disagree, every single phase.** Everything in the "found by running it" section below
looked correct on the page.

---

## The gate, item by item

| # | Item | How it was checked |
|---|---|---|
| 1 | Birth details stored as a versioned row | E2E: three steps → `17 August 1994 · 14:35 · Jaipur` in the manager |
| 2 | `GET /charts/{id}` returns a schema-valid D1 | Response validated against `ChartResponse`, the Pydantic model the OpenAPI is generated from |
| 3 | **D9 computed and stored** | `TestTheStoredD9IsTheNavamsaAndNotACopyOfTheD1` — **this was broken, see below** |
| 4 | Vimshottari to 3 levels, current dasha by date | `TestCurrentDashasReturnAllThreeLevels`: Sun / Moon / Mars at a fixed instant, 819 rows (9 + 81 + 729) |
| 5 | Transits every 6 h, Sade Sati verified | `TestTheCronFiresExactlyOnSlotBoundaries`, `TestSadeSatiKeepsAnsweringWhileAstroIsDown` |
| 6 | ≥10 yogas, positive **and** negative each | 11 implemented; `test_at_least_ten_distinct_yogas_are_implemented` plus a fires/does-not-fire pair per yoga |
| 7 | 30 golden fixtures, 5 cross-validated | 30 fixtures; 13 external-reference assertions against published events |
| 8 | Hypothesis properties | 120-year sum ✓, Rahu/Ketu ✓, **house permutation was a single example — now a property** |
| 9 | Timezone fixtures incl. 1943 and pre-1906 | 30 fixtures asserted against Go's tzdata **and** Python's zoneinfo |
| 10 | Place search < 50 ms p95 | measured **2.7 ms p95** over 16 queries |
| 11 | Editing creates v2, preserves v1 | E2E: `07:45`, `version 2`, exactly one active card |
| 12 | Ownership on every route, 404 not 403 | `chi.Walk` over the real route table — 9 routes |
| 13 | Unknown birth time → nulls | fixture 007 + 030; `TestAnUnknownBirthTimeStillProducesAChart` |
| 14 | **Cached chart with `astro-service` stopped** | `docker compose stop astro` → chart served; new profile → 503 with a message |
| 15 | Generated client compiles; contract diff | CI job "Cross-service contracts are in sync" |
| 16 | astro not public; internal token enforced | bound `127.0.0.1:8100`; LAN IP **refused**; no token → 401, wrong → 401, right → 200 |
| 17 | Deletion cascades to profiles, charts, dashas | `TestHardDeleteLeavesNoResidue` |
| 18 | ADR-003 `accepted` | ✅ skyfield (MIT) |
| 19 | `task verify` green | ✅ |
| 20 | Status docs updated | this file, `PROJECT_STATUS.md`, `current-phase.md` |

---

## Measured, not assumed

Against the running stack on this machine. The budgets are from §17.

| Budget | Measured |
|---|---|
| Chart computation < 200 ms | **p95 70.6 ms** (astro, direct, n=12) |
| Go round trip < 350 ms cold | **109–140 ms** (n=6 uncached profiles, including the first request of the process) |
| Cached read < 10 ms | **median 6.6 ms, p95 8.5 ms, max 10.0 ms** (n=15) |
| Place search < 50 ms p95 | **2.7 ms p95** (n=16) |

The cached read sits close to its budget — 10.0 ms at the maximum. That is the number
to watch when Phase 3 puts a chart on every page load.

---

## Found by running it, not by reading it

### The container had no ephemeris kernel

`services/astro/Dockerfile` copied `app` and not `data`. The container starts, answers
`/health`, and passes every compose health check — then 500s on the first request that
needs a planet:

```
FileNotFoundError: ephemeris kernel missing at /app/data/de421.bsp
```

**The Phase 2 chart API was dead in the container for the whole phase.** Nothing caught
it because no test had ever computed a real chart through the stack. That is the
argument for an end-to-end test, made by its absence.

### `ayana up` served a months-old image

`docker compose up` without `--build`. Six green ticks, stale code, routes that had been
in the source for days returning 404. CI never hit it: a fresh runner has no image to be
stale.

### D9 was a relabelled D1

`ChartInput` carried no chart type, so `?type=D9` stored the **whole rasi response**
under a D9 label. A client asking for the navamsa got the rasi, with the real navamsa
buried in a field it was not reading.

Fixed by deriving both charts from one response — which also removed a second astro call
that could have returned a chart computed from a different ephemeris state.

### The dasha tree cost 819 round trips

Inserted one row at a time inside the transaction. Measured against real Postgres:

```
819 rows — one-at-a-time 815 ms, single COPY 26 ms  (32x)
```

Now one `:copyfrom`, with UUIDs generated in Go so a child knows its parent's id before
either row exists. Rows go breadth-first because the `parent_id` foreign key is checked
per row.

### The data export never mentioned birth profiles or charts

The Phase 2 security checklist requires them. Deletion has `userOwnedTables` to discover
a new table and refuse to pass; the export had no equivalent, because there is nothing to
discover — **a missing section is an absent JSON key, and an absent key is
indistinguishable from "you have none of those"**.

A user exercising their right to a copy of their data would have been told, in effect,
that the most sensitive thing this product holds about them does not exist.

`TestEveryUserOwnedTableAppearsInTheExport` now discovers every table with a `user_id`
column and fails until each is either exported or explicitly recorded as not exported.

### The house-permutation property was one example

The gate asks for a **hypothesis property**. What existed asserted the property on a
single Jaipur chart, which cannot fail for a reason that depends on *where* the chart is
— and every degenerate case of the ascendant is latitude-driven. Now a property over
±78° latitude, the full longitude range and 150 years.

### `subdivision_index` had no test, and my justification for it was wrong

Covered in full in the PR for task 2.20. Short version: I had changed the arithmetic
calling it a correctness fix, citing nine of twenty-seven failing boundaries. Measured
against exact rational arithmetic, the count was three and the "fix" was wrong **more**
often than what it replaced. It now uses `as_integer_ratio` and is exact at every
boundary tested.

### `normalise_longitude` could return exactly 360

`(-1e-18) % 360.0` is `360.0`, which gave sign index 12 — the "house 13" its own
docstring promised to prevent, in the one line meant to prevent it.

### Two spec files had been sharing OTP mask letters

`uniqueEmail` produces `g1789…@example.com`, and the API log masks it to
`g***@example.com` — so the **first letter is the identity** as far as the OTP watcher
is concerned. Two tests sharing one, running in parallel, read each other's codes, and
the failure presents as "wrong code": broken authentication, not a collision.

I hit this twice. The second time, the letters I picked as free came from grepping for
`uniqueEmail('x')` literals, which missed all nine that `settings.spec.ts` passes as a
**parameter** to its own `signUp` helper.

`mask-letters.spec.ts` now reads every form and fails on a duplicate. On its first run
it found a collision that was **already there and not mine**: `load-errors.spec.ts` and
`settings.spec.ts` had both been using `i` and `j`.

### A golden-file fix that never took effect

PR 14 added `DASHA_TOLERANCE_SECONDS` but the assertion it was meant to replace was never
replaced: a text edit silently failed to match after `ruff format` reshaped the block. The
local run passed because this machine produces the values it generated, and **PR 14's
green CI was luck** on a runner that happened to agree.

The lesson is narrow: I edited by text replacement and did not verify the replacement
applied. Tests passing afterwards proved nothing, because they pass either way here.

---

## Guards proven by breaking them

Every one of these was reverted, observed to fail, and restored.

| Break | Failure |
|---|---|
| ayanamsa forced to zero | 5 external-reference failures (both sidereal ingresses, three Saturn transits) |
| ecliptic frozen at J2000 | both equinoxes |
| daily-motion sign flipped | the whole 2020 Mars retrograde season |
| two zodiac signs swapped in the Python | `the two orders have drifted` |
| `peak` renamed in the Python | `house 1 is "peak" in Go and "climax" in astro-service` |
| multiply-first arithmetic restored | `23 of 324 boundary-adjacent longitudes disagree with exact arithmetic` |
| bare modulo restored | `normalise_longitude(-5e-324) returned 360.0` |
| year length 365.25 → 365.2422 | golden dashas **2061 seconds** apart |
| a 2 µs dasha drift injected | passes — the tolerance absorbs platform noise |
| D1 payload stored under both labels | `the D1 and D9 payloads are byte-identical` |
| dashas hung off the requested chart | `no dasha rows on the rasi; the D9 having none proves nothing` |
| ownership mounted with `r.Use` on a `Route()` | `the OWNER got 404 on her own profile` |
| 404 changed to 403 | `403 confirms the profile exists ... enumeration oracle` |
| a route added outside the ownership group | `a stranger got 200 on somebody else's birth profile` |
| handler reading `chi.URLParam` | `handler.go reads "id" from the URL` |
| `asynq.Unique` dropped | `the second replica enqueued a second copy` |
| cron set to 4-hourly against 6-hour slots | `the cron fires 6 times a day but the interval is 6h` |
| prune moved before refresh | `a failed run left 0 rows, want the 1 seeded` |
| a house field on the global transit response | `a house is relative to one person's natal chart` |
| the export's `birth_profiles` key removed | `the export has no such key` |
| a new `user_id` table added | `reading_notes has a user_id column but no entry in this test` |
| house rotation made to repeat a sign | `houses at lat 0.000 do not cover twelve distinct signs` |
| a planet placed in house 13 | `Sun is in house 13 at lat 0.000` |
| two specs given the same mask letter | `'g' in onboarding-birth.spec.ts and settings.spec.ts` |
| the mask-letter patterns made to match nothing | `this test is checking nothing` |

---

## Carried into Phase 3

- **Web CSP still has `script-src 'unsafe-inline'`.** Carried from Phase 1. It blocks a
  Phase 5 gate item, not a Phase 2 one.
- **No ADR for hand-rolled auth.** Carried from Phase 1.
- **`memtest86+` still unrun** — nine data-corruption events on this machine, all
  recovered, none explained. Owner action, not a code change.
- **Place `admin1` needs `admin1CodesASCII.txt` in production.** The seeder resolves
  codes to names when the file is supplied and warns when it is not; the 20-row E2E
  fixture ships a matching mapping. A production seed without it labels places
  `Jaipur, 24, IN`.
- **Cached read is at 10.0 ms against a 10 ms budget** at the maximum. Watch it when
  Phase 3 puts a chart on every page.
