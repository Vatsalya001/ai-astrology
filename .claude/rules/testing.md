# Testing rules

## The principle
**Test the negative case.** A guard that has never been observed to fire is a guard you
cannot trust. Every invariant in this project has a test that asserts the unsafe thing
is actually refused — not merely that the safe path works.

## Go
- `go test ./... -race -count=1`. `-race` is not optional.
- Integration tests use `testcontainers-go` against real Postgres, behind a
  `//go:build integration` tag so the unit suite runs without Docker.
- **Do not mock the database.** The bugs that matter — lock contention, isolation
  levels, grant enforcement — live in behaviour a mock cannot reproduce.

## Python
- `pytest`. `hypothesis` for the astrology property tests in Phase 2: dashas sum to 120
  years, houses are a permutation of 1..12, Ketu is exactly opposite Rahu.
- Golden-file tests for chart computation, with fixtures committed.
  **Cross-validate against an independent reference before freezing a golden file.** A
  golden file that encodes your own bug makes that bug permanent.

## CI must never call a language model
Tests depending on a real model are slow, flaky, non-deterministic and eventually
expensive. Phase 4 ships a `MockProvider` returning fixtures; CI uses it exclusively.
Real-model evals run on demand in Phase 6.

## Fixtures
`tests/fixtures/` is synthetic only. Every birth profile there is invented. This is what
makes "no real user data reaches a free provider" enforceable rather than aspirational.

## What deserves a test
Business logic, every guard, every invariant, and anything involving money or
concurrency. Not getters, not framework wiring.
