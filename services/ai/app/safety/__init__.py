"""Layer 1 of three: screening input before anything is generated.

Layer 2 is the prompt rules in `safety_rules.v1.md`. Layer 3 is the
output validator. This one is the only layer that can stop a model
running at all, which is why the crisis path lives here.
"""

from app.safety.categories import (
    ACTION_FOR,
    SafetyAction,
    SafetyCategory,
    SafetyVerdict,
)
from app.safety.classifier import (
    CRISIS_THRESHOLD,
    DEFAULT_THRESHOLD,
    SAFETY_SCHEMA,
    SafetyClassifier,
)
from app.safety.crisis import (
    CRISIS_PATTERN,
    MissingCrisisResponseError,
    assert_crisis_responses_present,
    detect_crisis,
    load_crisis_response,
)

__all__ = [
    "ACTION_FOR",
    "CRISIS_PATTERN",
    "CRISIS_THRESHOLD",
    "DEFAULT_THRESHOLD",
    "SAFETY_SCHEMA",
    "MissingCrisisResponseError",
    "SafetyAction",
    "SafetyCategory",
    "SafetyClassifier",
    "SafetyVerdict",
    "assert_crisis_responses_present",
    "detect_crisis",
    "load_crisis_response",
]
