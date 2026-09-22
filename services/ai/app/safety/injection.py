"""Offline prompt-injection detection.

§14 asks that "prompt-injection attempts [are] flagged and neutralised".
The NEUTRALISE half has always worked: `SafetyCategory.PROMPT_INJECTION`
maps to `SafetyAction.NEUTRALISE`, which adds steering text to the
prompt, and the user's message never enters the system section at all.

The FLAGGING half rested entirely on the model screener, and an audit
found that no test anywhere hands an injection STRING to a real
detector — every one feeds the screener a stubbed verdict. So the
property was asserted against a mock of itself, and on a provider
outage there was no injection detection at all.

This is the same shape as `crisis.py`, and for the same reason: a safety
control that needs a network call is a safety control with an outage.

── Why the list is tight rather than generous ──

`crisis.py` is deliberately biased toward false positives, because the
cost of missing one is a life and the cost of a false one is an
unnecessary helpline. The trade here is different in both directions.

A false positive costs a paragraph of "stay in role" steering added to
someone's prompt — cheap, but not free: it is tokens on every affected
request and it makes the cache prefix vary.

More importantly, this product invites phrasings that look like
injection and are not. People ask an astrologer hypotheticals — "pretend
I was born an hour later", "forget what I said about my job" — and
`pretend`, `forget` and `act as` are exactly the words a careless list
reaches for. None of them appear below on their own.

Every pattern here requires the user to be addressing the SYSTEM: its
instructions, its prompt, its rules, or its identity. That is the thing
no astrology question needs to mention.
"""

from __future__ import annotations

import re

from app.safety.categories import SafetyCategory, SafetyVerdict

_INJECTION_PHRASES = (
    # Instruction override. The canonical form and its near neighbours.
    r"ignore (?:all |any |the )?(?:previous|prior|above|earlier|preceding|your) "
    r"(?:\w+ )?(?:instruction|prompt|rule|direction|guideline)",
    r"disregard (?:all |any |the )?(?:previous|prior|above|earlier|your) "
    r"(?:\w+ )?(?:instruction|prompt|rule|direction|guideline)",
    r"forget (?:all |everything |your )?(?:(?:previous|prior|above|earlier) )?"
    r"(?:instruction|prompt|rule|training|guideline)",
    r"override (?:your|the|all) (?:\w+ )?(?:instruction|prompt|rule|setting|guardrail)",
    r"bypass (?:your|the|all) (?:\w+ )?(?:instruction|prompt|rule|filter|guardrail|restriction)",
    # Exfiltration of the prompt itself. `.claude/rules/security.md`
    # treats the system prompt as ours, not the user's, and the output
    # validator already blocks a leak — this flags the ASK, which is the
    # signal worth having in telemetry even when the block works.
    r"(?:reveal|show|print|repeat|display|output|tell me|give me) "
    r"(?:me )?(?:your|the) (?:\w+ )?(?:system )?(?:prompt|instruction)",
    r"what (?:are|were|is|was) your (?:\w+ )?(?:system )?(?:prompt|instruction|rule)",
    r"repeat (?:everything|the text) above",
    r"system prompt",
    # Identity replacement. Anchored on "from now on" / "you are now",
    # never on a bare "act as" or "pretend" — both of those are ordinary
    # astrology phrasings ("pretend I was born an hour later").
    r"from now on,? you (?:are|will be|must)",
    r"you are (?:now|no longer) (?:a |an |my )?(?:\w+ ?){0,3}"
    r"(?:assistant|model|ai|bot|chatbot|system)",
    r"(?:enter|enable|activate) (?:developer|debug|god|dan|jailbreak) mode",
    r"\bDAN\b mode",
    # Delimiter and role smuggling: a user writing chat-protocol markers
    # into a message body is not asking about their chart.
    r"<\s*/?\s*(?:system|assistant)\s*>",
    r"\[\s*(?:system|assistant)\s*\]",
    r"###\s*(?:system|instruction)",
    r"\bsudo\b\s+(?:mode|prompt|ignore)",
)

INJECTION_PATTERN = re.compile(
    "|".join(f"(?:{phrase})" for phrase in _INJECTION_PHRASES), re.IGNORECASE
)


def detect_prompt_injection(message: str) -> SafetyVerdict:
    """Keyword pass for injection. Free, offline, and always available.

    Returns `NONE` when nothing matches, so a caller can treat it as an
    upgrade: the model screener's verdict wins unless it saw nothing and
    this saw something.

    `confidence` is 0.9 rather than 1.0, unlike the crisis pass. A crisis
    phrase means what it says; an injection phrase can appear inside a
    quoted example or a question about how the product works, and the
    consequence of being right is steering text rather than a helpline.
    """
    match = INJECTION_PATTERN.search(message)
    if match is None:
        return SafetyVerdict(category=SafetyCategory.NONE)

    return SafetyVerdict(
        category=SafetyCategory.PROMPT_INJECTION,
        confidence=0.9,
        # The matched phrase, never the message — the same rule the
        # crisis pass follows, for the same reason.
        matched=match.group(0)[:40],
        source="keywords",
    )
