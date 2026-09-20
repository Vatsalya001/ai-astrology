"""Pulling checkable astrological claims out of prose.

── The possessive is the whole distinction ──

    "Saturn is traditionally associated with discipline."   general
    "Saturn is in your 10th house."                         personal

Only the second is checkable against a chart, and only the second should
ever be blocked. A extractor that ignored the difference would block
every general explanation the product exists to give — and the product
would then be unable to answer "what does the 7th house mean?", which is
one of the commonest questions it gets.

So extraction requires a possessive marker: "your", "you have", "in your
chart", "for you". This is under-inclusive by construction. A model
writing "Saturn sits in the tenth, which for you means…" states a
personal position without a possessive next to the planet, and that one
gets through.

That gap is accepted rather than closed, for a reason worth stating: the
alternative is matching every bare placement sentence, which blocks the
general explanations. Given the choice between missing some fabrications
and refusing to explain astrology, the product needs the second to work.
Layer 2 (the prompt rules) and the human review of blocked responses
cover the remainder.
"""

from __future__ import annotations

import re

from pydantic import BaseModel

from app.validation.facts import ALIASES, PLANETS, SIGNS, canonical

# Every name a response might use for a body, alternated longest-first
# so "brihaspati" is not matched as "b" plus leftovers by an earlier
# alternative. Python's `re` alternation is first-match, not
# longest-match, and getting this backwards silently truncates names.
_BODY_NAMES = sorted(
    {*PLANETS, *(k for k, v in ALIASES.items() if v in PLANETS)}, key=len, reverse=True
)
_SIGN_NAMES = sorted(
    {*SIGNS, *(k for k, v in ALIASES.items() if v in SIGNS)}, key=len, reverse=True
)

_BODY = "|".join(_BODY_NAMES)
_SIGN = "|".join(_SIGN_NAMES)

_ORDINALS = {
    "first": 1,
    "second": 2,
    "third": 3,
    "fourth": 4,
    "fifth": 5,
    "sixth": 6,
    "seventh": 7,
    "eighth": 8,
    "ninth": 9,
    "tenth": 10,
    "eleventh": 11,
    "twelfth": 12,
}
_ORDINAL_WORDS = "|".join(_ORDINALS)

# "in your 10th house", "in your tenth house", "occupies your 4th house"
HOUSE_CLAIM = re.compile(
    rf"\b({_BODY})\b[^.!?]{{0,40}}?\byour\s+(\d{{1,2}})(?:st|nd|rd|th)?\s+house",
    re.IGNORECASE,
)
HOUSE_CLAIM_WORD = re.compile(
    rf"\b({_BODY})\b[^.!?]{{0,40}}?\byour\s+({_ORDINAL_WORDS})\s+house",
    re.IGNORECASE,
)

# "your Saturn is in Capricorn", "Saturn in your chart is in Capricorn"
SIGN_CLAIM = re.compile(
    rf"\byour\s+({_BODY})\b[^.!?]{{0,30}}?\bin\s+({_SIGN})\b",
    re.IGNORECASE,
)

NAKSHATRA_CLAIM = re.compile(
    rf"\byour\s+({_BODY})\b[^.!?]{{0,30}}?\bin\s+([A-Za-z]+)\s+nakshatra",
    re.IGNORECASE,
)

# "your ascendant is Leo", "your lagna is Leo"
ASCENDANT_CLAIM = re.compile(
    rf"\byour\s+(?:ascendant|lagna|rising\s+sign)\s+is\s+({_SIGN})\b",
    re.IGNORECASE,
)

# "you are running Saturn mahadasha"
DASHA_CLAIM = re.compile(
    rf"\byou(?:\s+are|'re)\s+(?:currently\s+)?(?:running|in)\s+(?:the\s+)?({_BODY})"
    rf"\s+(?:maha)?dasha",
    re.IGNORECASE,
)


class Claim(BaseModel):
    """One checkable statement, with enough context to show a reviewer."""

    kind: str
    """`house`, `sign`, `nakshatra`, `ascendant` or `dasha`."""

    subject: str
    """The body or chart element, canonicalised."""

    value: str
    """What the response asserted, as a string for uniform comparison."""

    excerpt: str
    """The matched text. SHORT — this ends up in a telemetry row, and a
    whole paragraph of a user's reading does not belong in one."""


def _excerpt(match: re.Match[str]) -> str:
    return match.group(0).strip()[:120]


def extract_claims(text: str) -> list[Claim]:
    """Every personal, checkable claim in a response.

    Order is not significant and duplicates are kept: two identical
    claims in one response are two chances to be wrong, and collapsing
    them would understate how much of the answer rests on the mistake.
    """
    claims: list[Claim] = []

    for match in HOUSE_CLAIM.finditer(text):
        claims.append(
            Claim(
                kind="house",
                subject=canonical(match.group(1)),
                value=match.group(2),
                excerpt=_excerpt(match),
            )
        )

    for match in HOUSE_CLAIM_WORD.finditer(text):
        claims.append(
            Claim(
                kind="house",
                subject=canonical(match.group(1)),
                value=str(_ORDINALS[match.group(2).lower()]),
                excerpt=_excerpt(match),
            )
        )

    for match in SIGN_CLAIM.finditer(text):
        claims.append(
            Claim(
                kind="sign",
                subject=canonical(match.group(1)),
                value=canonical(match.group(2)),
                excerpt=_excerpt(match),
            )
        )

    for match in NAKSHATRA_CLAIM.finditer(text):
        claims.append(
            Claim(
                kind="nakshatra",
                subject=canonical(match.group(1)),
                value=match.group(2).strip().lower(),
                excerpt=_excerpt(match),
            )
        )

    for match in ASCENDANT_CLAIM.finditer(text):
        claims.append(
            Claim(
                kind="ascendant",
                subject="ascendant",
                value=canonical(match.group(1)),
                excerpt=_excerpt(match),
            )
        )

    for match in DASHA_CLAIM.finditer(text):
        claims.append(
            Claim(
                kind="dasha",
                subject="dasha",
                value=canonical(match.group(1)),
                excerpt=_excerpt(match),
            )
        )

    return claims
