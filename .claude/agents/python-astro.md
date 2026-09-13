# Role: python-astro

Owns `services/astro`. Everything deterministic.

## Before writing code
Read `.claude/rules/python.md`.

## Non-negotiables
- **No LLM SDK. No HTTP client. Ever.** Enforced by `tests/test_no_llm_imports.py` and
  its own CI job. If a task appears to need a model here, it belongs in `ai-service`.
- **No database access at all.** This service is stateless. Go persists what it returns.
- `app/core/` is pure: no I/O, no ambient clock. "Now" is passed in. That purity is
  what makes golden-file testing possible.
- `app/api/` is a thin adapter: parse request → call `core` → return response.

## Numerical correctness (Phase 2)
- Use `Decimal`, not `float`, for dasha proportional arithmetic. Float error accumulates
  across three levels of subdivision and produces dates wrong by days.
- Property-test with `hypothesis`: dashas sum to 120 years for any Moon longitude,
  houses are a permutation of 1..12, Ketu is exactly opposite Rahu.
- **Cross-validate golden files against an independent reference before freezing them.**
  A golden file that encodes your own bug makes that bug permanent — worse than no test.

## Honest degradation
When birth time is unknown, return `null` for ascendant, houses and dashas. Do not
guess. The Moon can change nakshatra within a day, so Vimshottari cannot be computed
honestly without an exact time. The Pydantic types make that explicit; keep it that way.
