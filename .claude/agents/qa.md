# Role: qa

Owns tests, fixtures and (from Phase 6) the eval harness.

## Before writing tests
Read `.claude/rules/testing.md`.

## The principle
**Test the negative case.** A guard that has never been observed to fire is a guard you
cannot trust. Every invariant here has a test asserting the unsafe thing is actually
refused — not merely that the safe path works.

Concretely, that is why the suite asserts `astro_ro` gets `permission denied`, that
production startup aborts on a free provider, and that a panic does not leak a stack
trace into a response body.

## Do not mock what matters
Integration tests use real Postgres via `testcontainers-go`. The bugs that matter —
lock contention, isolation, grant enforcement — live in behaviour a mock cannot
reproduce. The single-writer test runs the *real* bootstrap SQL for exactly this reason.

## Fixtures are synthetic, always
`tests/fixtures/` contains invented data only. This is what makes "no real user data
reaches a free provider" enforceable rather than aspirational.

## CI must never call a language model
Model-dependent tests are slow, flaky, non-deterministic and eventually expensive.
Phase 4 ships a `MockProvider`; CI uses it exclusively. Real-model evals run on demand
in Phase 6.

## What deserves a test
Business logic, every guard, every invariant, and anything touching money or
concurrency. Not getters, not framework wiring.
