# ADR-006 — sqlc, not an ORM

**Status:** accepted · 2026-09-13

## Decision

`sqlc` generates type-safe Go from hand-written SQL in `services/api/db/queries/`.
No GORM, no ent, no Bun.

## Context

Go has capable ORMs. The question is whether this application's hard parts are helped
or hidden by one.

## Reason

**This application's hard parts are SQL problems:**

- Phase 7: `SELECT … FOR UPDATE` inside a transaction for wallet balance mutations,
  and a double-entry ledger with a debits-equal-credits invariant
- Phase 5: hybrid retrieval combining `pgvector` cosine distance, `tsvector` ranking
  and a JSONB metadata filter in one query
- Phase 8: billing ticks under contention, where exactly what is locked and for how
  long is the whole correctness argument

An ORM obscures exactly the queries you most need to read carefully. When a money bug
appears at 3am, `SELECT ... FOR UPDATE` in a `.sql` file is readable; a chain of
builder methods that may or may not emit the same lock is not.

**sqlc is not an ORM.** You write SQL, it generates structs and methods. There is no
query builder, no lazy loading, no N+1 surprises, and no runtime reflection.

## Tradeoffs

- More boilerplate for simple CRUD
- No automatic migrations (we use `golang-migrate` explicitly — every migration has a
  real `down`, because "revert the commit" is not a rollback plan)
- Dynamic queries need care

All acceptable. The schema is well understood and the queries are deliberate.
