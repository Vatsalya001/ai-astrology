"""The pre-pass that skips a model call entirely.

PHASE-04 §6: "roughly 40% of real messages are unambiguous and never
need a model call. On free local models this saves latency; in
production it saves real money at the highest-volume call site in the
system."

── Precision over coverage, and it is not close ──

A deferred message costs one `fast`-tier call, which is fractions of a
cent and a few hundred milliseconds. A WRONGLY matched message never
reaches the model at all: it retrieves the wrong chart facts, answers in
the wrong persona, and — if the true intent was MEDICAL — skips the
safety posture that intent carries.

So the rule is deliberately blunt: **exactly one intent's patterns
match, or defer.** Two matches is an ambiguous message and the model is
better at those than any ordering heuristic would be. Resolving ties by
specificity is the tempting alternative and it fails silently, because a
tie-break that picks wrong produces a confident answer rather than an
error.

── This is not the safety layer ──

The pre-pass sets `requires_safety_review` for the intents that always
carry it, but a crisis message is caught by `app/safety`, which runs on
every message regardless of what happened here. A keyword list is the
wrong shape for crisis detection: it must be biased hard toward false
positives, and this one is biased the other way on purpose.
"""

from __future__ import annotations

import re
from collections.abc import Iterable

from app.classification.intents import (
    SAFETY_REVIEWED_INTENTS,
    Entities,
    Intent,
    IntentResult,
)

FIRST_PERSON = re.compile(
    r"\b(?:i|i'm|im|i've|ive|i'd|id|i'll|ill|me|my|mine|myself|we|we're|our|ours|us|"
    # Hinglish is the register a large part of this audience writes in, and
    # "kya mujhe naukri milegi" is a first-person question by any reading.
    r"mujhe|mujhko|mera|meri|mere|hamara|hamari|humara|humari|apna|apni|apne)\b",
    re.IGNORECASE,
)
"""Does the message refer to the asker at all?

The pre-pass fires only when it does. On the 200-message labelled set
this condition removed three of the four wrong answers, and they were all
the same shape:

    "which house rules career in vedic astrology"
    "what does the 7th house signify for marriage generally"
    "is mercury retrograde a real thing or superstition"

Every one contains a strong keyword and none is about the person asking.
Answered as a personal question they retrieve the user's chart for a
question that was never about them — and the answer is confidently,
invisibly about the wrong subject.

── Why this is safe in a way a tie-break is not ──

A suppressor can only move a message from "decided" to "deferred". The
worst case is one cheap `fast`-tier call on a message the pre-pass would
have got right. A tie-break, by contrast, turns a deferral into a
decision, and when it picks wrong the result is a confident answer rather
than an error. The docstring above forbids the second; this is the first.

The fourth wrong answer was an ordinary gap — "kids" was missing from
FAMILY, so only RELATIONSHIP fired on "my partner and i are thinking
about kids". Adding the word makes both fire, which defers. Together:
**100% precision at 41.5% coverage**, measured by
`test_intent_dataset.py` on every run.

Coverage cost of the gate: 52% down to 41.5% — which is where PHASE-04
§6's "roughly 40%" estimate put it anyway.
"""

# Confidence for a keyword hit. Below 1.0 on purpose: the pre-pass is a
# cheap heuristic and marking it certain would make it outrank a model
# that genuinely looked at the sentence, if the two are ever compared.
# Comfortably above MIN_CONFIDENCE, because a hit here IS actionable.
_KEYWORD_CONFIDENCE = 0.9


def _any(*alternatives: str) -> re.Pattern[str]:
    r"""Compile alternatives with word boundaries where they make sense.

    `\b` around a Devanagari-transliterated word like `shaadi` behaves as
    expected, but around one ending in a non-word character it silently
    never matches. Alternatives are therefore written as plain words here
    and the boundary is applied once, around the group.
    """
    return re.compile(r"\b(?:" + "|".join(alternatives) + r")\b", re.IGNORECASE)


# Written as whole words rather than substrings. "car" inside "career",
# "art" inside "start", "job" inside "jobless" — substring matching turns
# a keyword list into a source of confident nonsense, and the failure is
# invisible because the match looks plausible in the log.
RULES: dict[Intent, tuple[re.Pattern[str], ...]] = {
    Intent.CAREER: (
        _any(
            "career",
            "job",
            "jobs",
            "promotion",
            "appraisal",
            "workplace",
            "resign",
            "resignation",
            "interview",
            "interviews",
            "employer",
            "boss",
            "salary hike",
            "naukri",
        ),
    ),
    Intent.MARRIAGE: (
        _any(
            "marriage",
            "married",
            "marry",
            "wedding",
            "shaadi",
            "vivah",
            "groom",
            "bride",
            "engagement",
        ),
    ),
    Intent.RELATIONSHIP: (
        _any(
            "relationship",
            "boyfriend",
            "girlfriend",
            "dating",
            "breakup",
            "break up",
            "crush",
            "love life",
            "partner",
        ),
    ),
    Intent.COMPATIBILITY: (
        _any(
            "compatible",
            "compatibility",
            "gun milan",
            "guna milan",
            "kundli matching",
            "horoscope matching",
            "match making",
            "matchmaking",
            "ashtakoota",
        ),
    ),
    Intent.FINANCE: (
        _any(
            "money",
            "wealth",
            "finance",
            "financial",
            "investment",
            "investments",
            "loan",
            "debt",
            "savings",
            "property purchase",
            "stocks",
            "share market",
        ),
    ),
    Intent.EDUCATION: (
        _any(
            "exam",
            "exams",
            "studies",
            "study",
            "college",
            "university",
            "admission",
            "entrance",
            "degree",
            "scholarship",
            "neet",
            "upsc",
        ),
    ),
    Intent.FAMILY: (
        _any(
            "mother",
            "father",
            "parents",
            "sibling",
            "siblings",
            "brother",
            "sister",
            "in-laws",
            "family",
            "children",
            "child",
            "kids",
            "son",
            "daughter",
            # The extended relations matter more here than in most
            # products: a joint family is the default frame for a large
            # part of this audience, and "my uncle's property dispute" is
            # a family question by any reading the user would recognise.
            "uncle",
            "aunt",
            "cousin",
            "nephew",
            "niece",
            "grandmother",
            "grandfather",
            "nani",
            "dadi",
            "nana",
            "dada",
            "bhai",
            "behen",
        ),
    ),
    Intent.TRAVEL: (
        _any("travel", "trip", "journey", "yatra", "pilgrimage", "vacation", "holiday"),
    ),
    Intent.RELOCATION: (
        _any(
            "relocate",
            "relocating",
            "relocation",
            "move abroad",
            "moving abroad",
            "settle abroad",
            "shift to",
            "migration",
            "immigration",
            "foreign settlement",
        ),
    ),
    Intent.DAILY_HOROSCOPE: (
        _any(
            "horoscope today",
            "today horoscope",
            "daily horoscope",
            "rashifal",
            "how is my day",
            "how will my day",
            "today's prediction",
        ),
    ),
    Intent.KUNDLI: (
        _any(
            "kundli",
            "kundali",
            "birth chart",
            "natal chart",
            "janam patri",
            "janampatri",
            "lagna chart",
            "d1 chart",
            "navamsa",
        ),
    ),
    Intent.DASHA: (
        _any(
            "dasha",
            "dasa",
            "mahadasha",
            "antardasha",
            "vimshottari",
            "pratyantardasha",
        ),
    ),
    Intent.TRANSIT: (
        _any(
            "transit",
            "transits",
            "gochar",
            "sade sati",
            "sadesati",
            "retrograde",
            "shani dhaiya",
        ),
    ),
    Intent.TAROT: (_any("tarot", "tarot card", "tarot cards", "card reading"),),
    Intent.NUMEROLOGY: (
        _any("numerology", "lucky number", "life path number", "name number", "ank jyotish"),
    ),
    Intent.HUMAN_ASTROLOGER: (
        _any(
            "talk to an astrologer",
            "speak to an astrologer",
            "real astrologer",
            "human astrologer",
            "book a consultation",
            "book an astrologer",
            "pandit ji",
            "call an astrologer",
        ),
    ),
    Intent.MEDICAL: (
        _any(
            "diagnosis",
            "disease",
            "illness",
            "surgery",
            "cancer",
            "diabetes",
            "medication",
            "prescription",
            "symptoms",
            "doctor",
            "hospital",
            "pregnancy test",
        ),
    ),
    Intent.LEGAL: (
        _any(
            "court case",
            "lawsuit",
            "litigation",
            "divorce case",
            "legal notice",
            "bail",
            "fir",
            "property dispute",
            "custody",
        ),
    ),
}
"""Deliberately incomplete.

GENERAL_ASTROLOGY, EMOTIONAL_SUPPORT and OTHER have no rules at all, and
that is the design rather than an omission:

  - GENERAL_ASTROLOGY is the fallback for a low-confidence model answer.
    Keyword-matching INTO it would convert "tell me about my chart" from
    a deferred message into a confident one, and the whole point of the
    fallback is that it is reached by not being sure.
  - EMOTIONAL_SUPPORT is where a distressed message lands. The words
    that would match it are the same words the crisis classifier needs
    to see, and a pre-pass that resolved them would skip the model on
    exactly the messages that most deserve one.
  - OTHER is the absence of a match, which the pre-pass expresses by
    returning None rather than by matching.
"""


def _matching_intents(message: str) -> list[Intent]:
    return [
        intent
        for intent, patterns in RULES.items()
        if any(pattern.search(message) for pattern in patterns)
    ]


def classify_by_keywords(message: str) -> IntentResult | None:
    """One intent, or `None` meaning "ask the model".

    `None` rather than a low-confidence result: the caller's branch is
    "did this save a model call", and a result object carrying
    `confidence=0.2` invites someone to use it anyway.
    """
    if not FIRST_PERSON.search(message):
        # Educational or context-free. See FIRST_PERSON above.
        return None

    matched = _matching_intents(message)

    if len(matched) != 1:
        # Zero matches: nothing recognisable, the model decides.
        # Two or more: genuinely ambiguous — "will my marriage be happy
        # after I relocate" is both MARRIAGE and RELOCATION, and picking
        # one by rule order would be picking one by accident.
        return None

    intent = matched[0]
    return IntentResult(
        primary=intent,
        confidence=_KEYWORD_CONFIDENCE,
        entities=Entities(),
        requires_safety_review=intent in SAFETY_REVIEWED_INTENTS,
        source="keywords",
    )


def coverage(messages: Iterable[str]) -> float:
    """Fraction of messages the pre-pass decides without a model.

    Exposed for the accuracy script and the admin dashboard. Worth
    watching over time: coverage that drops means real messages have
    drifted away from the rules, and the saving quietly stopped.
    """
    messages = list(messages)
    if not messages:
        return 0.0
    decided = sum(1 for message in messages if classify_by_keywords(message) is not None)
    return decided / len(messages)
