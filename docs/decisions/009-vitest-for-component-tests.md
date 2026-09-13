# ADR-009 — Vitest for component tests

**Status:** accepted · 2026-09-14

## Decision

The web app gets **Vitest** plus **@testing-library/react** for component-level
tests. Playwright stays the only tool that drives a browser against the running
stack; the two do not overlap.

## Context

Phase 0 shipped `app/error.tsx` and `app/status/loading.tsx` with no automated
test. Both were verified by hand — temporarily breaking `status/page.tsx`,
watching them render, reverting. That works once. It does not survive the next
person who edits `error.tsx`.

The specific thing needing a guard is a security property, not a visual one:
`error.tsx` must never render `error.message`. A server-render failure
routinely carries a connection string or an internal hostname, and the error
page is public. Today nothing fails if someone "improves" it by surfacing the
real error — which is exactly the change a developer makes while debugging and
forgets to revert.

Playwright cannot reach it. The boundary only fires when a Server Component
throws, and provoking that needs a route whose sole purpose is to throw. Two
ways to get one, both rejected:

- **Ship the route.** A permanently reachable endpoint that crashes the server
  on request, in production, so that a test can pass.
- **Build it only for CI.** Then the artifact under test is not the artifact
  that ships — precisely the sandbox/prod divergence this project's rules warn
  about.

Rendering the component directly sidesteps both. The invariant lives in the
component, so that is where it can be asserted.

## Reason

- Vitest uses the same Vite pipeline Next already depends on; no second
  transform config to keep in step with `tsconfig.json`
- `@testing-library/react` asserts on what a user perceives — roles, accessible
  names — so a test fails when the *experience* breaks, not when a class name
  is renamed
- Fast enough to sit in `task verify` without anyone wanting to skip it
- Covers the gap between `tsc` (types) and Playwright (the whole stack) that
  currently has nothing in it

## Tradeoffs

- A third test runner in the repo, after `go test` and `pytest`. Justified only
  because the alternative for the TypeScript tier is no coverage at all.
- jsdom is not a browser. Layout, real focus order and computed styles are not
  trustworthy here — those assertions stay in Playwright, which is why
  `smoke.spec.ts` keeps the computed-style check on the design tokens.
- **It does not prove Next mounts the boundary.** These tests assert the
  component's contract given an error; that App Router renders `error.tsx` on a
  server throw is framework behaviour, verified manually and recorded in
  `docs/TESTING-PHASE-0.md §7b`. If Next changed that, these tests would still
  pass. Accepted: the framework behaviour is stable and documented, the
  component contract is what we keep breaking.

## Revisit when

- A component test starts asserting on layout or geometry — that belongs in
  Playwright, and its presence here means the boundary has blurred
- Next ships a supported way to exercise an error boundary end to end, at which
  point the manual procedure in §7b can be deleted
