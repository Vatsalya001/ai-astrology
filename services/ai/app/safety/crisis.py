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
import unicodedata
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
    # The same intent in the phrasings the block above misses. Each was
    # probed against CRISIS_PATTERN before being added here: "take my
    # own life" does not match "I want to take my life", and "end it
    # all" does not match "I've decided to end it". A list that reads as
    # though it covers direct statements of intent, and does not, is
    # worse than a short one — it is the false confidence that stops
    # anyone widening it.
    #
    # The lookaheads are not hedging. Each names the innocent
    # continuation the bare phrase would otherwise swallow, and they are
    # the likeliest sentences in this product's corpus rather than
    # hypotheticals: people ask an astrologer about life direction and
    # about breakups more than about anything else.
    #
    # Every exclusion ends in `\b`, and that anchor is load-bearing in
    # the direction that costs a life. Without it the alternation
    # matches a PREFIX, so `to` swallows "take my life tonight" and
    # "today", `in` swallows "instead", and `with` swallows "end it
    # without anyone knowing" — three of the most direct statements this
    # list exists to catch, silently dropped by an exclusion written for
    # "take my life in a new direction". `lessons?` for the same reason
    # in reverse: anchoring `lesson` would stop excluding the plural.
    r"take my life(?!\s+(?:in|back|to|savings|lessons?)\b)",
    # "end THINGS with X" is how a breakup is described; "end IT with X"
    # is how a method is. The exclusion therefore applies to `things`
    # only. Before this split, `(?!\s+with\b)` suppressed all of
    #
    #     "I've decided to end it with pills"
    #     "I want to end it with a rope"
    #
    # — method statements, which are the highest-risk category in the
    # whole list. The cost of the split is that "I want to end it with
    # him" now flags. That is the correct error: §7 asks for a list
    # "biased heavily toward false positives", and an exclusion is the
    # only kind of edit to this file that can move the bias the WRONG
    # way. One annoyed user against one missed method statement is not a
    # close call.
    r"(?:(?:decided|going|about|want|need) to|wanna) end things\b(?!\s+with\b)",
    r"(?:(?:decided|going|about|want|need) to|wanna) end it\b",
    # Same lesson. `(?!\s+(?:in|at|with|for)\b)` was meant to spare "I
    # don't want to be here in Delhi anymore" and instead swallowed
    #
    #     "...here at all anymore"     "...here for another day"
    #     "...here in this world anymore"
    #
    # Narrowed to a short list of words that name a PLACE, so only the
    # literal relocation complaint is spared. "in this world" is not on
    # it, and will not be.
    r"(?:don'?t|do not) want to be here(?!\s+(?:in|at)\s+(?:this\s+|the\s+|my\s+)?"
    r"(?:city|town|country|office|house|home|job|place|company|room|building|"
    r"school|college|flat|apartment|hostel|department|team)\b)",
    r"(?:want|wanted|need) (?:it all|everything|all of it) to (?:stop|end)",
    r"better off (?:if i (?:was|were) )?gone",
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
    # The Hinglish halves of the five English phrases added above. A
    # widening that lands in English only moves the hole rather than
    # closing it: the user who writes "mujhe nahi jeena" would be the
    # one left behind by a list that just learned "I don't want to be
    # here".
    #
    # "jaan de d…" covers dunga / dungi / di — the tense varies, the
    # meaning does not. "kar d…" likewise: sab khatam kar dunga / kar
    # dena hai. The "ho jaye" form is kept separate from "ho gaya",
    # which is the ordinary past tense of something running out.
    r"jaan de d",
    r"jaan dena hai",
    r"nahin? jeena",
    r"sab (?:kuch )?khatam kar d",
    r"sab kuch khatam ho jaye",
    r"mere bina (?:sab|sabhi)",
    # ─── Devanagari ──────────────────────────────────────────────────
    #
    # Until 2026-09-23 this list matched NOTHING in Devanagari script —
    # `मुझे नहीं जीना`, `मैं मरना चाहता हूँ` and `आत्महत्या करना चाहता हूँ` all
    # returned `none`, and no test asserted it either way. A guard that
    # works for users who transliterate and fails for users who type in
    # the script is a guard with a hole shaped like a keyboard setting.
    #
    # These are the DIRECT forms only — the ones whose meaning is
    # dictionary-level rather than idiomatic, so they can be added
    # without the native-speaker review that
    # `docs/HINGLISH-CRISIS-REVIEW.md` is still waiting for. That review
    # is about COVERAGE (what would a person actually type?) and this is
    # not a substitute for it. It is the difference between zero and
    # something.
    #
    # Written against the NORMALISED message — see `_normalise` below —
    # so patterns use base consonants and never a nuqta. `ज़िंदगी` and
    # `जिंदगी` are the same string by the time they reach here, which
    # matters because people type both and two Unicode encodings exist
    # for each.
    r"मरना चाहत",  # मरना चाहता / चाहती — "(I) want to die"
    r"मरना है",  # "(I) have to die"
    r"जीना नहीं चाहत",  # "(I) don't want to live"
    r"नहीं जीना",  # "(I) don't want to live" — the commonest short form
    r"जीने का मन नहीं",  # "(I) don't feel like living"
    r"जीने की इच्छा नहीं",  # same, more formal
    r"आत्महत्या",  # suicide (Sanskrit-derived)
    r"खुदकुशी",  # suicide (Urdu-derived; nuqta normalised away)
    r"अपनी जान",  # "(take) my own life"
    r"जान दे",  # "give up (my) life" — देना / दूंगा / दूँगी
    r"जिंदगी खत्म",  # "life over"
    r"सब खत्म कर",  # "end it all"
    # `सब`/`सभी` required, exactly as `mere bina (?:sab|sabhi)` requires
    # it on the Latin side. Bare `मेरे बिना` flags `मेरे बिना मत जाओ` —
    # "don't go without me" — whose transliteration `mere bina mat jao`
    # is already in the must-NOT-flag list. Caught by running the
    # negative cases, not by reading the pattern.
    r"मेरे बिना (?:सब|सभी)",  # "without me, everyone (would be better off)"
    r"मर जाऊ",  # "(I) will die" — जाऊं / जाऊँगा
)

CRISIS_PATTERN = re.compile("|".join(f"(?:{phrase})" for phrase in _CRISIS_PHRASES), re.IGNORECASE)

# The nuqta, which Devanagari encodes two ways.
#
# `ज़` is either U+095B or `ज` + U+093C, and a person typing on a phone
# has no idea which their keyboard produced. An NFD pass decomposes the
# precomposed form, dropping the mark then makes both identical, and NFC
# puts everything else back. Matras are separate characters and survive
# untouched — verified, because a normaliser that ate them would silently
# break every pattern here.
_NUQTA = "़"


def _normalise(message: str) -> str:
    """Collapse both nuqta encodings so one pattern matches either."""
    decomposed = unicodedata.normalize("NFD", message)
    return unicodedata.normalize("NFC", decomposed.replace(_NUQTA, ""))


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
    match = CRISIS_PATTERN.search(_normalise(message))
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
