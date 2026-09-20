"""What the user is asking about, as a closed set.

The intent drives four downstream decisions — which chart facts to
retrieve, which persona answers, which model tier pays for it, and
whether the safety layer looks harder. Getting it wrong is therefore not
a cosmetic mislabel: a CAREER question classified as MEDICAL retrieves
the wrong houses and answers in the wrong voice.

── Why a closed set and not free text ──

Every one of those four decisions is a lookup keyed on this value. A
free-text label makes all four `.get(label, default)` calls, and a
default is how an unrecognised label silently becomes GENERAL_ASTROLOGY
without anything reporting that the classifier produced something new.
"""

from __future__ import annotations

from enum import StrEnum

from pydantic import BaseModel, Field

from app.providers.base import CallStats


class Intent(StrEnum):
    """The 21 intents from PHASE-04 §6.

    `StrEnum` so an intent survives a round trip through JSON, a log line
    and an admin dashboard as itself, rather than as an integer nobody
    can read at 3am.
    """

    GENERAL_ASTROLOGY = "general_astrology"
    CAREER = "career"
    RELATIONSHIP = "relationship"
    MARRIAGE = "marriage"
    FINANCE = "finance"
    EDUCATION = "education"
    FAMILY = "family"
    TRAVEL = "travel"
    RELOCATION = "relocation"
    DAILY_HOROSCOPE = "daily_horoscope"
    KUNDLI = "kundli"
    DASHA = "dasha"
    TRANSIT = "transit"
    COMPATIBILITY = "compatibility"
    TAROT = "tarot"
    NUMEROLOGY = "numerology"
    HUMAN_ASTROLOGER = "human_astrologer"
    EMOTIONAL_SUPPORT = "emotional_support"
    MEDICAL = "medical"
    LEGAL = "legal"
    OTHER = "other"


SAFETY_REVIEWED_INTENTS = frozenset(
    {
        # Three intents where the product's posture is fixed regardless of
        # what the model thinks: cultural and spiritual framing only, and
        # a clear pointer to a qualified human. PHASE-04 §7, Layer 1.
        Intent.MEDICAL,
        Intent.LEGAL,
        # Not because emotional support is dangerous, but because it is
        # the intent nearest to the crisis path, and the cost of looking
        # twice is one cheap classification.
        Intent.EMOTIONAL_SUPPORT,
    }
)

# Below this the classifier is guessing, and a narrow guess is worse than
# a broad one: it retrieves the wrong chart facts confidently. PHASE-04
# §6 sets the number.
MIN_CONFIDENCE = 0.6


class Entities(BaseModel):
    """The few things worth pulling out of a message.

    ── `person` holds a RELATION, never a name ──

    "my partner", "my elder brother" — never "Priya". A name is PII by
    itself and this object is passed to context selection and may end up
    in a debug log. `.claude/rules/security.md`: log IDs, not objects,
    and redaction is key-based so it cannot descend into a free-text
    field. Keeping names out at extraction time is the only place this
    can be enforced.
    """

    timeframe: str = Field(
        default="",
        description="A period the answer should cover: 'next 6 months', '2026'.",
    )
    person: str = Field(
        default="",
        description="Who the question is about, as a RELATIONSHIP not a name.",
    )
    topic: str = Field(
        default="",
        description="A short noun phrase for what specifically is being asked.",
    )


class IntentResult(BaseModel):
    """One classification, whatever produced it.

    Identical shape from the keyword pre-pass and from the model, so no
    call site has to know which ran. `source` exists only so telemetry
    can measure the pre-pass hit rate — the thing that decides whether
    the cost saving is real.
    """

    primary: Intent
    secondary: Intent | None = None
    confidence: float = Field(ge=0.0, le=1.0)
    entities: Entities = Field(default_factory=Entities)
    requires_safety_review: bool = False

    source: str = Field(
        default="model",
        description="'keywords' or 'model'. Telemetry only; never routing.",
    )

    stats: CallStats = Field(
        default_factory=CallStats,
        description="What this classification cost. Zero when the keyword "
        "pre-pass answered, which is what makes the saving measurable.",
    )

    fallback_from: Intent | None = Field(
        default=None,
        description="What the model actually said, when POLICY overrode it.",
    )
    fallback_confidence: float = Field(default=0.0, ge=0.0, le=1.0)
    """The confidence that was too low, recorded rather than discarded.

    Without these two fields, a model answering CAREER at 0.55 and a
    model answering nothing at all are indistinguishable downstream:
    both arrive as GENERAL_ASTROLOGY with confidence 0.0. That made two
    very different problems look identical —

        "the classifier is wrong"      -> change the model or the prompt
        "our threshold is too high"    -> change one number

    — and the measurement script could not tell them apart either, so
    `scripts/measure_intent_accuracy.py` was reporting post-policy
    accuracy as though it were model accuracy.

    They are diagnostic, never routing: nothing branches on them. The
    fallback is still GENERAL_ASTROLOGY with broad context, which is
    what §6 asks for.
    """

    @property
    def is_confident(self) -> bool:
        return self.confidence >= MIN_CONFIDENCE
