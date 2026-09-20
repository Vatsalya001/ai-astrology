"""What the safety layer can find, and what each finding means.

PHASE-04 §7, Layer 1. The table in the spec pairs each category with an
ACTION, and that pairing is the whole design: a category nobody acts on
differently is a label, not a safety control.
"""

from __future__ import annotations

from enum import StrEnum

from pydantic import BaseModel, Field

from app.providers.base import CallStats


class SafetyCategory(StrEnum):
    CRISIS = "crisis"
    """Self-harm, suicidal ideation, immediate danger.

    The only category that BYPASSES generation entirely. Everything else
    changes how the model is prompted; this one means no model runs.
    """

    MEDICAL = "medical"
    LEGAL = "legal"
    PROMPT_INJECTION = "prompt_injection"
    ABUSE = "abuse"
    NONE = "none"


class SafetyAction(StrEnum):
    """What the orchestrator does, derived from the category.

    Separate from the category because two categories can share an
    action and because the action is what the pipeline branches on — a
    caller matching on category has to know the mapping, and every
    caller knowing it is how one of them gets it wrong.
    """

    SHORT_CIRCUIT = "short_circuit"
    """Return the static response. No model call at all."""

    CONSTRAIN = "constrain"
    """Generate, but with the posture the category demands."""

    NEUTRALISE = "neutralise"
    """Generate, treating the message strictly as data."""

    DECLINE = "decline"
    """Refuse, log, and let rate limiting do the rest."""

    PROCEED = "proceed"


ACTION_FOR: dict[SafetyCategory, SafetyAction] = {
    SafetyCategory.CRISIS: SafetyAction.SHORT_CIRCUIT,
    SafetyCategory.MEDICAL: SafetyAction.CONSTRAIN,
    SafetyCategory.LEGAL: SafetyAction.CONSTRAIN,
    SafetyCategory.PROMPT_INJECTION: SafetyAction.NEUTRALISE,
    SafetyCategory.ABUSE: SafetyAction.DECLINE,
    SafetyCategory.NONE: SafetyAction.PROCEED,
}


class SafetyVerdict(BaseModel):
    """One message, judged before anything is generated."""

    category: SafetyCategory = SafetyCategory.NONE
    confidence: float = Field(default=1.0, ge=0.0, le=1.0)

    matched: str = Field(
        default="",
        description="Which rule fired, for auditing a false positive. A SHORT "
        "EXCERPT or a rule name — never the whole message, which would put a "
        "user's worst moment in a log line.",
    )

    source: str = Field(default="keywords", description="'keywords' or 'model'.")

    stats: CallStats = Field(
        default_factory=CallStats,
        description="What this screening cost. Zero when the offline keyword "
        "pass answered, which is the common case and the whole point of it.",
    )

    @property
    def action(self) -> SafetyAction:
        return ACTION_FOR[self.category]

    @property
    def blocks_generation(self) -> bool:
        return self.action is SafetyAction.SHORT_CIRCUIT
