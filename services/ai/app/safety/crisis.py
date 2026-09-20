"""Crisis detection, and the response that is never generated.

PHASE-04 §7: "Crisis detection is not a place for cleverness. Keyword
list plus classifier, biased heavily toward false positives, routed to a
**static, human-written** response. An astrological reading is never the
right answer to a crisis message."

── Why the response is a file and not a prompt ──

A model asked to write a compassionate crisis response will write one,
and it will be different every time, and one time in ten thousand it
will say something harmful — to the person least able to absorb it. It
may also, being an astrology product, reach for the chart. There is no
prompt that reliably prevents that, and no test that could catch it
after the fact.

A file has none of those properties. It was written once by a person, it
is reviewed like code, and it says the same thing to everyone.

── Why the numbers are the riskiest lines in this repository ──

A wrong helpline number is worse than no number: it costs someone in
crisis the one attempt they were willing to make. They are therefore
kept in a file a human can read end to end, `test_crisis.py` asserts the
response carries at least one, and `docs/PROVIDER-VERIFICATION.md` is
not the right home for the check — a human must dial each one before
this ships. That is recorded as an open item, not as done.
"""

from __future__ import annotations

import re
from pathlib import Path

from app.safety.categories import SafetyCategory, SafetyVerdict

RESPONSES_DIR = Path(__file__).parent / "responses"

# Phrase-level, not word-level, and that is the entire reason this list
# is usable. "die" alone flags "I'm dying to know"; "want to die" does
# not. The bias toward false positives is real and deliberate, but it is
# spent on ambiguous EXPRESSIONS rather than on ambiguous words — which
# is how a list stays biased-to-safe without being biased-to-useless.
_CRISIS_PHRASES = (
    # Intent, stated directly.
    r"kill myself",
    r"killing myself",
    r"end my life",
    r"ending my life",
    r"take my own life",
    r"want to die",
    r"wanna die",
    r"wish i was dead",
    r"wish i were dead",
    r"better off dead",
    r"better off without me",
    r"suicide",
    r"suicidal",
    r"not want to live",
    r"don'?t want to live",
    r"dont want to live",
    r"no reason to live",
    r"no point (?:in )?living",
    r"can'?t go on",
    r"cant go on",
    r"end it all",
    r"harm myself",
    r"hurt myself",
    r"self harm",
    r"self-harm",
    r"cutting myself",
    r"overdose",
    r"jump off",
    r"hang myself",
    # Hinglish and Hindi transliteration. Omitting these would make the
    # guard work for the subset of this audience that writes in English
    # and silently fail for the rest — which is not a partial guard, it
    # is a guard with a gap shaped like a demographic.
    r"marna chahta",
    r"marna chahti",
    r"jeena nahi chahta",
    r"jeena nahi chahti",
    r"jeene ka mann nahi",
    r"khudkushi",
    r"aatmahatya",
    r"apni jaan",
    r"zindagi khatam",
)

CRISIS_PATTERN = re.compile("|".join(f"(?:{phrase})" for phrase in _CRISIS_PHRASES), re.IGNORECASE)


class MissingCrisisResponseError(RuntimeError):
    """The static response is absent or empty.

    Its own type, and fatal at startup rather than at the moment a
    distressed user sends a message. A service that boots without this
    file will detect a crisis correctly and then have nothing to say —
    the worst possible combination, because the detection succeeding
    means the short-circuit already fired and no model will run.
    """


def load_crisis_response(language: str = "en") -> str:
    """The exact bytes a person wrote.

    Falls back to English for an unknown language rather than raising:
    an unsupported locale must still get a helpline, and English with
    the right numbers beats nothing in the user's own language.
    """
    path = RESPONSES_DIR / f"crisis.{language}.md"
    if not path.exists():
        path = RESPONSES_DIR / "crisis.en.md"

    if not path.exists():
        raise MissingCrisisResponseError(
            f"no crisis response at {path}. This file is not optional: detection "
            f"firing without it means the astrology path is already bypassed and "
            f"there is nothing to send instead."
        )

    text = path.read_text().strip()
    if not text:
        raise MissingCrisisResponseError(f"{path} is empty")

    return text


def assert_crisis_responses_present() -> None:
    """Startup guard. Called from `app/guards.py`.

    Fails the boot, in the same spirit as the PII guard: a missing
    safety response is a configuration error, and finding it at deploy
    time costs a rollback while finding it in production costs somebody
    a great deal more.
    """
    for language in ("en", "hi"):
        load_crisis_response(language)


def detect_crisis(message: str) -> SafetyVerdict:
    """Keyword pass. Fast, offline, and first.

    Runs before the model classifier and before anything else, because
    it must still work when every provider in the chain is down. A
    safety control that depends on a network call is a safety control
    with an outage.
    """
    match = CRISIS_PATTERN.search(message)
    if match is None:
        return SafetyVerdict(category=SafetyCategory.NONE)

    return SafetyVerdict(
        category=SafetyCategory.CRISIS,
        confidence=1.0,
        # The matched PHRASE, never the message. A log line carrying
        # someone's worst moment verbatim is exactly what
        # `.claude/rules/security.md` forbids, and the phrase is all an
        # auditor needs to judge a false positive.
        matched=match.group(0)[:40],
        source="keywords",
    )
