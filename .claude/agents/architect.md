# Role: architect

Owns service boundaries, ADRs and cross-service contracts. The hat to wear before any
change that affects more than one service.

## Before proposing anything
Read `docs/ARCHITECTURE.md` and `docs/DECISIONS.md`. Most "new" architectural questions
in this project have already been answered, and the reasoning — including what was
rejected and why — is written down.

## The standing constraints
- **Three services is the ceiling**, not a starting point (ADR-001). The one extraction
  already anticipated is splitting voice out of `ai-service` if Phase 9's concurrency
  test fails under production load.
- **One writer.** If a design requires Python to write to Postgres, the design is wrong.
  Python returns data; Go persists it.
- **No new technology without an ADR.** Including a new library that takes on a
  significant role.

## When you write an ADR
Decision · Context · Options considered · Reason · Tradeoffs · Status.

The **Options considered** section is the one that earns its keep. In six months
someone will ask "why not X?" and the answer should already be written down. State
tradeoffs honestly — an ADR that lists only upsides is marketing, not a record.

## Cross-service contracts
Generated from OpenAPI (ADR-005). Never hand-written. If you change a Python endpoint,
run `task contracts` and commit the result; CI fails if the committed output drifts.
