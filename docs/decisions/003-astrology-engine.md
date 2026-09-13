# ADR-003 — Astrology engine and its licence

**Status:** 🔴 **PROPOSED — MUST BE CLOSED BEFORE PHASE 7** · raised 2026-09-13

This is the only open architectural question in the project. It is recorded now, in
Phase 0, because the moment money changes hands "we'll sort the licence out later"
becomes a liability rather than a task.

## Decision

**Not yet made.** Phase 2 implements against Swiss Ephemeris via `pyswisseph`. The
licensing path must be chosen before Phase 7 (Monetization).

## Context

Swiss Ephemeris is the reference implementation for astrological calculation:
sub-arcsecond accuracy, every ayanamsa, every house system, correct lunar nodes,
correct retrogradation. `pyswisseph` is its mature Python binding.

It is **dual-licensed**: AGPL-3.0, or a paid commercial licence from Astrodienst.

## Options considered

| Option | Obligation | Fits if |
|---|---|---|
| **AGPL-3.0** | Network use triggers source disclosure — you must offer the complete corresponding source of the application to its users | Open-sourcing, or pre-revenue and comfortable with that |
| **Commercial licence** | One-time fee to Astrodienst | Shipping closed-source commercial SaaS — which this is |
| **`skyfield` (MIT)** | None | Willing to implement the Vedic layer directly |

The third option deserves serious consideration rather than dismissal. `skyfield` is
MIT-licensed, pure Python, and computes accurate geocentric positions from JPL
ephemerides. What it does not provide is the Vedic layer — ayanamsa, nakshatras,
dashas, vargas, yogas.

But **all of that is deterministic arithmetic on top of longitudes, and we are writing
it ourselves regardless**: Lahiri ayanamsa is a published polynomial, nakshatra is
`floor(longitude / 13°20′)`, and Vimshottari is a fixed 120-year proportional
sequence. The marginal work is smaller than it first appears.

## Current position

Development proceeds on `pyswisseph`. Both paths are free and legally clean for
non-distributed development, so this does not block Phase 2.

The abstraction in `services/astro/app/core/` is written so the ephemeris backend is
swappable: `EPHEMERIS_PROVIDER` is already a configuration value, not a hardcoded
import. That keeps option C genuinely available rather than theoretically available.

## Decision criteria

Answer these, in order:

1. Will this ship as closed-source SaaS? If yes, AGPL is not viable.
2. What does the Astrodienst commercial licence actually cost today?
3. How much work is the Vedic layer on top of `skyfield`, measured rather than
   estimated? (Phase 2 will tell you, because you will have written most of it.)

## Tradeoffs

- **AGPL** — free, but source disclosure is incompatible with a closed commercial product
- **Commercial** — a one-time cost, and the accuracy is unmatched
- **`skyfield`** — no licence constraint, more of the Vedic layer to own and test

## Gate

The Phase 2 Phase Gate contains a blocking checklist item: this ADR must read
`accepted`, not `proposed`, before Phase 2 is considered complete.
