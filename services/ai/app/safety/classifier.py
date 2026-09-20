"""Layer 1: screen every message before anything is generated.

PHASE-04 §7. Two passes, in this order and for this reason:

  1. **Keywords**, offline. Must work when every provider in the chain
     is down — a safety control that needs a network call is a safety
     control with an outage.
  2. **A `fast`-tier model**, for the indirect phrasing a list cannot
     hold. "I don't see the point of anything anymore" contains no
     crisis keyword and is a crisis message.

── The fail-open question, answered deliberately ──

If the model call fails, this returns `none` and the message proceeds.
That is fail-OPEN, and it is a choice worth stating plainly rather than
leaving implicit:

  - The keyword pass has already run and is unaffected by an outage, so
    the direct expressions are still caught.
  - Failing closed would mean showing a crisis response to everyone
    during an unrelated provider outage — telling thousands of people
    who asked about their career that the product thinks they are in
    danger. That is its own harm, and a large one.

The residual risk is an indirectly-phrased crisis message arriving
during an outage. It is real. It is smaller than the alternative, and it
is recorded here so the trade is visible rather than discovered.
"""

from __future__ import annotations

import json

from pydantic import ValidationError

from app.prompts import PromptBuilder
from app.providers.base import (
    CompletionRequest,
    LLMProvider,
    Message,
    ProviderError,
    RequestMetadata,
)
from app.safety.categories import SafetyCategory, SafetyVerdict
from app.safety.crisis import detect_crisis

SAFETY_SCHEMA: dict[str, object] = {
    "type": "object",
    "additionalProperties": False,
    "required": ["category", "confidence"],
    "properties": {
        "category": {"type": "string", "enum": [c.value for c in SafetyCategory]},
        "confidence": {"type": "number", "minimum": 0, "maximum": 1},
    },
}

# Deliberately low, and low in one direction only. A model that is 40%
# sure this is a crisis is a model saying "something here worries me",
# and the cost of acting on that is a helpline shown to someone who did
# not need it. The cost of ignoring it does not have a comparable
# bound.
CRISIS_THRESHOLD = 0.4

# Everything else needs ordinary confidence: over-flagging MEDICAL
# constrains an answer that did not need constraining, over-flagging
# ABUSE refuses a real question, and neither failure is protective.
DEFAULT_THRESHOLD = 0.6


class SafetyClassifier:
    """Keyword pass, then a model pass, then a verdict."""

    def __init__(
        self,
        provider: LLMProvider,
        *,
        prompt_version: str = "v1",
        use_model: bool = True,
    ) -> None:
        self._provider = provider
        self._prompt_version = prompt_version
        self._use_model = use_model

    def _prompt(self) -> PromptBuilder:
        return PromptBuilder().add("safety_classification", self._prompt_version).cache_breakpoint()

    def _parse(self, text: str) -> SafetyVerdict | None:
        stripped = text.strip()
        if stripped.startswith("```"):
            stripped = stripped.strip("`").removeprefix("json").strip()

        try:
            payload = json.loads(stripped)
        except (json.JSONDecodeError, ValueError):
            return None

        if not isinstance(payload, dict):
            return None

        try:
            return SafetyVerdict.model_validate({**payload, "source": "model"})
        except ValidationError:
            return None

    async def screen(self, message: str, *, trace_id: str = "") -> SafetyVerdict:
        """The verdict the orchestrator branches on.

        The keyword pass short-circuits: once a direct expression has
        matched there is nothing a model could add, and spending a
        round trip to ask permission to act on it would delay exactly
        the response that should be instant.
        """
        keyword_verdict = detect_crisis(message)
        if keyword_verdict.category is SafetyCategory.CRISIS:
            return keyword_verdict

        if not self._use_model:
            return SafetyVerdict(category=SafetyCategory.NONE)

        request = CompletionRequest(
            messages=[Message(role="user", content=message)],
            # Instructions in `system`, message in a `user` turn. The
            # structural split is what makes rule 6 of the prompt
            # enforceable rather than aspirational — a screener that
            # concatenated them could be talked out of screening.
            system=self._prompt().build(),
            tier="fast",
            max_tokens=128,
            temperature=0.0,
            json_schema=SAFETY_SCHEMA,
            metadata=RequestMetadata(
                trace_id=trace_id,
                prompt_version=f"safety_classification.{self._prompt_version}",
                job="safety_classification",
            ),
        )

        try:
            response = await self._provider.complete(request)
        except ProviderError:
            # Fail open. See the module docstring — this is a stated
            # trade, not an oversight, and the keyword pass has already
            # run.
            return SafetyVerdict(category=SafetyCategory.NONE, source="provider_error")

        verdict = self._parse(response.text)
        if verdict is None:
            return SafetyVerdict(category=SafetyCategory.NONE, source="unparseable")

        threshold = (
            CRISIS_THRESHOLD if verdict.category is SafetyCategory.CRISIS else DEFAULT_THRESHOLD
        )
        if verdict.confidence < threshold:
            return SafetyVerdict(category=SafetyCategory.NONE, source="low_confidence")

        return verdict
