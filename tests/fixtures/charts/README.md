# Synthetic birth-profile fixtures

**Every profile here is invented.** None describes a real person. This is the only
birth data that development, CI and evaluation are permitted to use.

That rule exists because of the free-tier PII constraint (ADR-004): birth date + birth
time + birth place is, in combination, close to a unique identifier, and free model
tiers may train on their inputs. Synthetic fixtures make "never send real user data to
a free provider" enforceable rather than aspirational.

## Structure

```
charts/
├── profiles.json       the inputs — 12 cases, chosen for edge coverage
└── expected/           golden outputs, generated in Phase 2
```

`expected/` is empty until Phase 2 implements the ephemeris. At that point each
fixture gains an `expected_d1.json` and `expected_dasha.json`, and those files become
the contract: the Phase 2 gate requires all of them to match exactly, with at least
five cross-validated against an independent reference.

**A golden file that encodes your own bug is worse than no test at all** — it makes the
bug permanent. Cross-validate before freezing.

## Why these twelve

Each exists to break a specific assumption.

| ID | Case | What it catches |
|---|---|---|
| 001 | Delhi, 1990, morning | Baseline. Nothing special. |
| 002 | Mumbai, 1985, 00:12 | Date boundary — midnight can land on the wrong Julian day |
| 003 | Kolkata, 1943 | **Wartime DST.** India ran UTC+06:30, not +05:30. Assuming IST gives the wrong ascendant. |
| 004 | London, 1995 | Non-Indian timezone, with its own DST rules |
| 005 | Anchorage, 2000 | Extreme latitude — house systems degenerate near the poles |
| 006 | Quito, 1988 | Equator — the opposite degenerate case |
| 007 | Pune, 1998, time unknown | **Must return null** for ascendant, houses and dashas. Not a guess. |
| 008 | Chennai, 2000-02-29 | Leap day |
| 009 | Kolkata, 1901 | **Pre-1906 local mean time.** IST did not exist yet. |
| 010 | Jaipur, 1994, cusp | Ascendant within 0.5° of a sign boundary — the case where a small time error changes everything |
| 011 | Sydney, 1979 | Southern hemisphere + DST |
| 012 | Village, no coordinates | Manual coordinate entry, the common real-world failure |

Fixture 003 and 009 are the ones most likely to be got wrong, and the reason the Phase 2
spec insists timezone resolution uses a real tzdata lookup rather than a fixed offset.
