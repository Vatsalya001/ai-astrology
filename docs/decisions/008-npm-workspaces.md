# ADR-008 — npm workspaces instead of pnpm

**Status:** accepted · 2026-09-13

## Decision

The TypeScript side uses **npm workspaces**. The specifications say pnpm; this is a
deliberate, recorded deviation.

## Context

`corepack enable` requires write access to `/usr/bin` and failed with `EACCES` on the
development machine. The alternative install path was piping a remote script to a
shell, which was declined — reasonably.

npm 12 ships with Node 22 and supports workspaces natively.

## Reason

- Zero additional installation, and no privilege escalation
- Workspace support is sufficient for four packages and one app
- The difference that matters for a repo this size is install speed and disk usage,
  neither of which is currently a bottleneck

## Tradeoffs

- Slower installs and a larger `node_modules` than pnpm's content-addressed store
- No strict dependency isolation, so a phantom dependency could go unnoticed

## Revisit when

Install time becomes a real irritation, or a phantom-dependency bug appears. Switching
later is a lockfile change and a CI line — low cost, so there is no need to force it
now.
