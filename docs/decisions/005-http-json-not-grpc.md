# ADR-005 — HTTP + JSON between services, not gRPC

**Status:** accepted · 2026-09-13

## Decision

Go ↔ Python communication uses HTTP with JSON bodies. Contracts are generated from
FastAPI's OpenAPI document into typed Go clients.

## Context

Three services in two languages need a wire format and a contract mechanism.

## Reason

- **protoc in a three-language monorepo is a real tax.** Code generation, plugin
  versions and build ordering across Go, Python and CI cost more than they return at
  this call volume.
- **Call volume is low.** Charts are computed once and cached permanently. AI calls are
  dominated by model latency measured in seconds, not by transport overhead measured in
  microseconds.
- **FastAPI already emits OpenAPI**, so the contract is a by-product rather than a
  separate artefact to maintain.
- **Debuggability.** `curl` works. A failing call can be reproduced by hand.

## Tradeoffs

- JSON is slower and larger than protobuf
- No streaming RPC (SSE covers the Phase 5 streaming case adequately)
- Weaker schema evolution guarantees

## Discipline

**Never hand-write a cross-service client.** `task contracts` regenerates from the
OpenAPI document, and CI fails if the committed output differs from what the source
produces. A hand-written client drifts silently from the contract it claims to
implement.

## Revisit when

Inter-service call volume becomes latency-critical — most plausibly the Phase 9 audio
path, where the Go proxy adds a hop. Budget there is 5ms for both hops; if measurement
shows the transport is the problem rather than the implementation, revisit.
