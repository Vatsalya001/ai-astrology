# AI rules

## The determinism principle
An LLM never computes astrology. Not positions, houses, nakshatras, dashas, degrees,
transits or yogas. It interprets structured data computed upstream.

Machine-enforced two ways:
1. `astro-service` has no model access at all (topology).
2. From Phase 5, every astrological claim in a response is checked against the
   `fact_index` supplied with the context. A mismatch is a hard block.

## Prompts
- Versioned and **immutable**. `chat_response.v1` is frozen; improvements become `v2`.
  You cannot debug a response from three weeks ago if the prompt has been edited since.
- Composed from modules, never one giant string.
- Stable content first, volatile content last — a byte change early in the prefix
  invalidates the whole cache downstream.
- Every response records `{model, provider_id, prompt_version, context_version}`.

## Safety
- Crisis input **bypasses astrology entirely** and returns a static, human-written
  response with a helpline. Never a model-generated one. Never a prediction.
- No guaranteed outcomes: medical, marriage, pregnancy, death, legal, financial.
- Traditional framing ("is traditionally read as"), never predictive ("you will").
- Compatibility never produces a "don't marry this person" verdict.

## Cost
- Route by job: `fast` for classification and extraction, `chat` for conversation,
  `deep` for paid interpretation.
- Prompt caching on the stable prefix is the single biggest lever.
- Batch API for anything without a latency requirement (daily horoscopes, reports).
- Judge **cost per completed task**, not per request.
