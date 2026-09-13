# Python rules — services/astro and services/ai

## Both services
- `uv` for dependency management. `ruff` for lint and format. `mypy --strict`.
- Pydantic v2 models double as the OpenAPI schema; they are the cross-service contract.
- `extra="forbid"` on settings so a typo'd env var is an error, not a silent default.
- Structured JSON logging to stdout, matching the Go service's field names.

## astro-service — additional, non-negotiable
- **No LLM SDK. No HTTP client. Ever.** Enforced by `tests/test_no_llm_imports.py`.
- `app/core/` is pure: no I/O, no database, no ambient clock. "Now" is passed in.
  That purity is what makes golden-file testing possible.
- `app/api/` is a thin adapter: parse request → call `core` → return response.
- Use `Decimal` for dasha proportional arithmetic. Float error accumulates across three
  levels of subdivision and produces dates that are wrong by days.
- `hypothesis` for property tests: dashas sum to 120 years, houses are a permutation of
  1..12, Ketu is exactly opposite Rahu.

## ai-service — additional
- **Read-only database role.** Never write. Return data for Go to persist.
- No vendor SDK outside `app/providers/`.
- Startup guards run before anything binds (`app/guards.py`).
- Never interpolate user input into the system prompt section.
