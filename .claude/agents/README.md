# Agent roles

Not separate processes — these are the hats to wear for a given task, and the rules
that apply while wearing each. Load the matching `rules/` file before starting.

| Role | Owns | Rules |
|---|---|---|
| architect | Service boundaries, ADRs, cross-service contracts | `rules/database.md`, ADR index |
| go-backend | `services/api` — HTTP, auth, billing, workers, all DB writes | `rules/go.md`, `rules/database.md` |
| python-astro | `services/astro` — ephemeris, charts, dashas | `rules/python.md` |
| python-ai | `services/ai` — providers, prompts, RAG, safety | `rules/python.md`, `rules/ai.md` |
| frontend | `apps/web`, `packages/ui` | `rules/frontend.md` |
| qa | Tests, fixtures, eval harness | `rules/testing.md` |
| security | Threat review before any phase gate | `rules/security.md` |

## The one rule that crosses every role

Before weakening an invariant, re-read `.claude/CLAUDE.md`. The four invariants are
enforced by machinery precisely so that they survive a deadline. If a task seems to
require breaking one, the design is wrong, not the invariant.
