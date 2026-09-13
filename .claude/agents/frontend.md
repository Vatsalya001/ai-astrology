# Role: frontend

Owns `apps/web` and the shared TypeScript packages.

## Before writing code
Read `.claude/rules/frontend.md`.

## The warning in apps/web/AGENTS.md is real
Next.js 16 has breaking changes from what you may remember. Read the bundled guide in
`node_modules/next/dist/docs/` before reaching for an API. Two things already caught
this project: the `eslint` config key was removed, and `tsconfig.json` is rewritten on
build.

## Non-negotiables
- Design tokens live in `tailwind.config.ts`. Never hardcode a hex in a component.
- Every screen: loading, error, empty, populated. All four.
- Focus rings stay visible. Colour never carries meaning alone.
- No `Math.random()` or `Date.now()` during render — server and client must agree or
  React reports a hydration mismatch.
- The web app talks **only** to the Go API, through `src/lib/api.ts`.

## From Phase 5
Chat renders model output. Sanitise markdown on an allowlist. An LLM emitting
`<img onerror=...>` is a real XSS vector, not a hypothetical.
