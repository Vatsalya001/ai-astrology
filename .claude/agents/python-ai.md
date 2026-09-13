# Role: python-ai

Owns `services/ai`. Everything probabilistic.

## Before writing code
Read `.claude/rules/python.md` and `.claude/rules/ai.md`.

## Non-negotiables
- **Read-only database role.** Never write. Return data for Go to persist.
- No vendor SDK outside `app/providers/`.
- Startup guards run before anything binds. Do not move them.
- Never interpolate user input into the system prompt section.
- Prompts are versioned and immutable. `chat_response.v1` is frozen; improvements
  become `v2`. You cannot debug a response from three weeks ago if the prompt has been
  edited since.
- Stable content first in the prompt, volatile last — a byte change early in the prefix
  invalidates the whole cache downstream.

## The determinism principle
This service interprets astrology. It never computes it. If you find yourself wanting a
planetary position, request it from `astro-service`; do not ask a model.

From Phase 5, every astrological claim in a response is checked against the `fact_index`
supplied with the context. A mismatch is a hard block, not a warning.
