"""Hybrid classification: keywords first, model only when needed.

PHASE-04 §6. The ordering is the cost decision — this is the
highest-volume call site in the system, so the cheapest correct answer
wins, and for roughly the messages the pre-pass recognises the cheapest
correct answer costs nothing at all.

── The user's message never enters the system prompt ──

`.claude/rules/python.md`: "Never interpolate user input into the system
prompt section." The instructions go in `system`; the message goes in a
`user` turn, as data. This is not a stylistic split — it is the only
structural defence against prompt injection available at this layer, and
a f-string that builds one string out of both throws it away silently.
"""

from __future__ import annotations

import json

from pydantic import ValidationError

from app.classification.intents import (
    SAFETY_REVIEWED_INTENTS,
    Entities,
    Intent,
    IntentResult,
)
from app.classification.keywords import classify_by_keywords
from app.prompts import PromptBuilder
from app.providers.base import (
    CompletionRequest,
    LLMProvider,
    Message,
    ProviderError,
    RequestMetadata,
)

INTENT_SCHEMA: dict[str, object] = {
    "type": "object",
    "additionalProperties": False,
    "required": ["primary", "confidence", "requires_safety_review"],
    "properties": {
        "primary": {"type": "string", "enum": [intent.value for intent in Intent]},
        "secondary": {
            "type": ["string", "null"],
            "enum": [*[intent.value for intent in Intent], None],
        },
        "confidence": {"type": "number", "minimum": 0, "maximum": 1},
        "requires_safety_review": {"type": "boolean"},
        "entities": {
            "type": "object",
            "additionalProperties": False,
            "properties": {
                "timeframe": {"type": "string"},
                "person": {"type": "string"},
                "topic": {"type": "string"},
            },
        },
    },
}
"""Generated from the enum, not written out by hand.

A hand-written list drifts the day an intent is added: the model would
be told about twenty intents while the code knows twenty-one, and the
mismatch shows up as a validation failure on a rare message rather than
as a failing test.
"""


class IntentClassifier:
    """Keyword pre-pass, then a `fast`-tier model call."""

    def __init__(
        self,
        provider: LLMProvider,
        *,
        prompt_version: str = "v1",
        use_keywords: bool = True,
    ) -> None:
        self._provider = provider
        self._prompt_version = prompt_version
        self._use_keywords = use_keywords

    def _prompt(self) -> PromptBuilder:
        """Stable, and therefore fully cacheable.

        Every classification sends byte-identical instructions — the
        message is in the user turn — so the entire system prompt is
        stable prefix and the breakpoint goes at the very end. This is
        the one job in the product where that is true, and it is also the
        highest-volume one.
        """
        return PromptBuilder().add("intent_classification", self._prompt_version).cache_breakpoint()

    def _parse(self, text: str) -> IntentResult | None:
        """Model output to a validated result, or `None` if it is not one.

        `None` rather than an exception: an unparseable classification is
        recoverable — the caller falls back to broad context and the user
        still gets an answer. Raising would turn a slightly-worse answer
        into no answer, which is a much worse trade at the top of the
        pipeline.
        """
        stripped = text.strip()

        # Local models add a fence despite being asked not to, and
        # `json.loads` on ```json\n{...}\n``` fails for a reason that has
        # nothing to do with the classification.
        if stripped.startswith("```"):
            stripped = stripped.strip("`")
            stripped = stripped.removeprefix("json").strip()

        try:
            payload = json.loads(stripped)
        except (json.JSONDecodeError, ValueError):
            return None

        if not isinstance(payload, dict):
            return None

        try:
            result = IntentResult.model_validate({**payload, "source": "model"})
        except ValidationError:
            # Covers the common local-model failures at once: an invented
            # intent name, a confidence of 1.5, entities as a string.
            return None

        return result

    @staticmethod
    def _fallback(reason: str) -> IntentResult:
        """Broad context beats a narrow guess.

        PHASE-04 §6: below 0.6, fall back to GENERAL_ASTROLOGY "rather
        than guessing narrowly and retrieving the wrong chart facts". The
        confidence is carried through as zero so telemetry can count how
        often this path runs — a rising rate means the classifier or the
        model behind it has drifted.
        """
        return IntentResult(
            primary=Intent.GENERAL_ASTROLOGY,
            confidence=0.0,
            entities=Entities(),
            # Conservative: the reason the fallback fired is that nothing
            # is known about the message, and an unknown message is not a
            # safe one.
            requires_safety_review=True,
            source=reason,
        )

    async def classify(self, message: str, *, trace_id: str = "") -> IntentResult:
        if self._use_keywords:
            hit = classify_by_keywords(message)
            if hit is not None:
                return hit

        request = CompletionRequest(
            messages=[Message(role="user", content=message)],
            system=self._prompt().build(),
            tier="fast",
            # A label and a few short strings. Generous enough for the
            # longest plausible object and small enough that a model
            # ignoring the schema and writing an essay is cut off rather
            # than billed for.
            max_tokens=256,
            # Not zero: some backends reject 0.0, and this is a task where
            # the answer is constrained by a schema anyway.
            temperature=0.1,
            json_schema=INTENT_SCHEMA,
            metadata=RequestMetadata(
                trace_id=trace_id,
                prompt_version=f"intent_classification.{self._prompt_version}",
                job="intent_classification",
            ),
        )

        try:
            response = await self._provider.complete(request)
        except ProviderError:
            # Every provider in the chain has already been tried by the
            # time this raises. Classification is not worth failing the
            # user's whole question over.
            return self._fallback("provider_error")

        result = self._parse(response.text)
        if result is None:
            return self._fallback("unparseable")

        if not result.is_confident:
            return self._fallback("low_confidence")

        if result.primary in SAFETY_REVIEWED_INTENTS:
            # Asserted here rather than trusted from the model. The three
            # intents in that set carry a fixed product posture, and a
            # model that returned `false` would quietly disable it.
            result = result.model_copy(update={"requires_safety_review": True})

        return result
