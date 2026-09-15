# ADR-003 — Astrology engine and its licence

**Status:** ✅ **ACCEPTED** · raised 2026-09-13 · decided 2026-09-16

## Decision

**`skyfield` (MIT), with the Vedic layer implemented in `services/astro/app/core/`.**

Not `pyswisseph`. The project carries no ephemeris licence obligation, now or at
Phase 7.

## Why

The three options were:

| Option | Obligation | |
|---|---|---|
| `pyswisseph` under AGPL-3.0 | Network use triggers source disclosure — serving users over HTTP means offering them the complete corresponding source of Ayana | Rejected |
| `pyswisseph` + Astrodienst commercial licence | A one-time fee, and a purchase someone has to actually make | Rejected |
| **`skyfield` (MIT)** | **None** | **Chosen** |

The decision turns on how much `pyswisseph` actually saves us, and the honest
answer is: less than it first appears.

Both libraries compute accurate geocentric positions — `skyfield` from JPL
ephemerides, and far beyond the precision astrology needs. What `pyswisseph`
additionally *bundles* is the ayanamsa tables, the house systems and the node
calculations. Everything downstream of a longitude — nakshatras, padas, vargas,
the Vimshottari tree, aspects, yogas, Sade Sati — is deterministic arithmetic we
were always going to write and test ourselves.

So the marginal work is the Lahiri ayanamsa polynomial and the house systems.
Lahiri is published. Whole sign — the Vedic default and the only house system
Phase 2 ships — is `ascendant_sign + n`. That is a small, well-specified,
testable surface in exchange for permanently removing a licence question from a
product that intends to take money.

### What we are giving up, stated plainly

**Agreement with Astrodienst, not accuracy.** Indian astrologers cross-check
against astro.com, and `pyswisseph` would match it by construction. We now have
to *earn* that agreement through cross-validation instead of inheriting it.

This is the strongest argument against this decision and it is the reason the
Phase 2 gate requires five golden fixtures validated against an **independent**
reference before any of them are frozen. A golden file that encodes our own
ayanamsa bug would make that bug permanent and invisible.

**Placidus and Sripati get harder.** Both are available in `pyswisseph` and
neither is in `skyfield`. Neither is the default, and Phase 2 ships whole sign
only. When bhava chalit is wanted, the implementation is ours to write — priced
in, not overlooked.

## Consequences

- `EPHEMERIS_PROVIDER` remains a real configuration value with a real interface
  behind it, so this is reversible if cross-validation exposes something we
  cannot reconcile. The abstraction is not theatre; it is the exit.
- `EPHEMERIS_FLAG`, `EPHEMERIS_PATH` — `pyswisseph` concepts — do not apply.
  `skyfield` fetches a JPL kernel (`de421.bsp`, ~17 MB, public domain) which is
  vendored rather than downloaded at runtime: `app/core` may not touch the
  network, and a service that phones home on boot is not deterministic.
- The ayanamsa implementation is ours, so it gets its own tests against
  published values at several epochs — not just the golden files, which would
  only prove we are self-consistent.
- No AGPL obligation and no purchase. The Phase 7 liability this ADR was raised
  to prevent no longer exists.

## Gate

The Phase 2 gate requires this ADR to read `accepted`. It does. The obligation
it transfers — cross-validating the ayanamsa and the five fixtures against an
independent source — is tracked in that same gate and is not optional.
