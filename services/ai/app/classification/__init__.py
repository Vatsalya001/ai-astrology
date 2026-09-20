"""Intent classification: what the user is asking about.

The keyword pre-pass and the model classifier produce the same
`IntentResult`, so nothing downstream branches on which one ran.
"""

from app.classification.classifier import INTENT_SCHEMA, IntentClassifier
from app.classification.intents import (
    MIN_CONFIDENCE,
    SAFETY_REVIEWED_INTENTS,
    Entities,
    Intent,
    IntentResult,
    min_confidence,
)
from app.classification.keywords import RULES, classify_by_keywords, coverage

__all__ = [
    "INTENT_SCHEMA",
    "MIN_CONFIDENCE",
    "RULES",
    "SAFETY_REVIEWED_INTENTS",
    "Entities",
    "Intent",
    "IntentClassifier",
    "IntentResult",
    "classify_by_keywords",
    "coverage",
    "min_confidence",
]
