# ADR-001 — Three services: Go API, Python astro, Python AI

**Status:** accepted · 2026-09-13

## Decision

Three backend deployables rather than one:

- `api-service` (Go) — public HTTP surface, sole database writer, workers
- `astro-service` (Python) — deterministic ephemeris, stateless, no DB, no LLM
- `ai-service` (Python) — LLM orchestration, RAG, read-only DB role

## Context

The product has three workloads with genuinely different shapes:

1. **High-concurrency I/O** — WebSocket fan-out for consultations, a billing tick every
   30 seconds per active session, connection handling. Thousands of long-lived
   connections.
2. **Numerical, offline computation** — Swiss Ephemeris, dasha arithmetic, divisional
   charts. CPU-bound, stateless, heavily cacheable.
3. **LLM orchestration** — provider SDKs, embeddings, retrieval, evaluation, audio.

## Options considered

**A. TypeScript monolith (NestJS).** One language, one deploy. Rejected: `pyswisseph`
is the mature ephemeris binding and every LLM/audio library is Python-first. Node
bindings for Swiss Ephemeris are cgo wrappers with a thinner track record, and the
eval tooling would be built from scratch.

**B. Go monolith with cgo ephemeris.** One binary. Rejected: puts a C toolchain in the
critical path of every API build and deploy, and still leaves the AI work in the wrong
ecosystem.

**C. Three services (chosen).** Each language does what it is best at.

**D. Microservices per domain.** Rejected as premature. Three is the floor for this
stack, not a starting point to expand from.

## Reason

The language boundaries fall on genuine workload boundaries rather than arbitrary
domain lines. The split also buys a structural property that is otherwise only a
convention: **`astro-service` cannot call a language model**, because it has no SDK, no
key and no HTTP client. The determinism principle stops being a rule people remember
and becomes a fact about the topology.

## Tradeoffs

Real and worth stating plainly:

- Three CI pipelines, three dependency trees, three toolchains
- Cross-service contracts to keep in sync
- Distributed tracing needed to follow one request
- Local development needs several processes running
- Roughly 2–3 extra days in Phase 0, and 2–5 in most later phases

Mitigations, all in place from Phase 0:

- **One database, one writer.** Only Go writes; `ai-service` has a read-only role;
  `astro-service` has no database access. This removes the worst class of
  distributed-systems bug.
- **One `docker compose up`** brings the whole stack up.
- **One `Taskfile.yml`** hides three toolchains behind four commands.
- **One trace ID** propagated across all three services.
- **Degraded, not down.** Non-critical dependencies failing degrade the system;
  verified by stopping `astro-service` and observing `/health` return 200/`degraded`.

## Revisit when

- A fourth service is proposed — re-read this ADR first
- `ai-service` shows GIL contention between voice and chat (Phase 9); splitting
  `voice-service` out is the one extraction already anticipated
