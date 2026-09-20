"""Pulling checkable astrological claims out of prose.

── The possessive is the whole distinction ──

    "Saturn is traditionally associated with discipline."   general
    "Saturn is in your 10th house."                         personal

Only the second is checkable against a chart, and only the second should
ever be blocked. An extractor that ignored the difference would block
"what does the 7th house mean?" — one of the commonest questions the app
gets — so the validator would have made the product unable to EXPLAIN
astrology in order to stop it INVENTING astrology.

── Why a placement link and not a proximity window ──

The first version paired a planet with a possessive using a wildcard
window: `\\b(planet)\\b[^.!?]{0,40}?\\byour\\s+(\\d+)th\\s+house`. It had no
grammatical content, so it matched straight across a clause boundary:

    "Saturn rules discipline, and your 10th house is career."
     ^^^^^^                        ^^^^^^^^^^^^^^^^

A true, wholly general sentence, extracted as "Saturn is in house 10",
blocked as a fabrication, regenerated once, blocked again, and answered
with the graceful fallback. Four of five realistic general sentences
tripped it. `[^.!?]` also matches newlines, so consecutive markdown
bullets were joined into one phantom claim.

So the planet and the location must now be joined by something that
actually STATES a placement — `in`, `sits in`, `is placed in`,
`occupies` — with nothing but an optional comma-delimited appositive
between them. That is strictly narrower, and the direction is deliberate:

  - A false positive BLOCKS a correct answer to a common question, costs
    a regeneration, and shows the user a failure. It fires on ordinary
    prose.
  - A false negative lets one wrong placement reach the user. Serious,
    but Layer 2's prompt rules and the real chart in the context both
    push against it, and a human reviewing blocked responses will not
    see it either way.

Under-inclusive by construction, as before — but now the misses are
unusual phrasings rather than the product's main job.
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

# The 27, copied from services/astro/app/core/constants.py. Six are two
# words, which is why this is a list rather than `([A-Za-z]+)`: a
# single-word pattern silently exempts Purva Phalguni and its five
# siblings from checking altogether.
NAKSHATRAS = (
    "Ashwini",
    "Bharani",
    "Krittika",
    "Rohini",
    "Mrigashira",
    "Ardra",
    "Punarvasu",
    "Pushya",
    "Ashlesha",
    "Magha",
    "Purva Phalguni",
    "Uttara Phalguni",
    "Hasta",
    "Chitra",
    "Swati",
    "Vishakha",
    "Anuradha",
    "Jyeshtha",
    "Mula",
    "Purva Ashadha",
    "Uttara Ashadha",
    "Shravana",
    "Dhanishta",
    "Shatabhisha",
    "Purva Bhadrapada",
    "Uttara Bhadrapada",
    "Revati",
)

_BODY = "|".join(_BODY_NAMES)
_SIGN = "|".join(_SIGN_NAMES)
# Two-word names first, so "Purva Phalguni" is not matched as "Purva".
_NAKSHATRA = "|".join(sorted(NAKSHATRAS, key=len, reverse=True))

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

# An ordinal in either form: "10th", "tenth". Captured as one group and
# normalised by `_house_number`.
_HOUSE_NUMBER = rf"(\d{{1,2}}(?:st|nd|rd|th)?|{_ORDINAL_WORDS})"

# A comma-delimited appositive, and nothing else. "Saturn, the planet of
# structure, is in your 4th house" is a real sentence a model writes, and
# both commas are required — which is what stops this becoming the
# wildcard window again. No `your` inside, so it cannot swallow the
# possessive it is supposed to precede.
_ASIDE = r"(?:,\s*(?:(?!your\b)[^,.!?]){0,48},)?"

# Adverbs, which appear on BOTH sides of the verb: "Saturn currently sits
# in your 4th" and "Saturn is currently in your 4th" are both ordinary.
# The first version allowed only the first position, so "is currently in"
# — the commoner of the two — slipped past unchecked.
_ADVERBS = r"currently|presently|now|also|still|again|indeed|in\s+fact"
_ADVERB = rf"(?:\s+(?:{_ADVERBS}))?"

# The ways a placement is actually stated. Everything here asserts "X is
# located at Y"; nothing here is a lordship, rulership or influence
# claim, which are general statements about astrology rather than about
# the reader.
_PLACES = (
    r"(?:"
    r"\s+(?:is|are|was|were|sits|sit|falls|fall|resides|reside|lies|lie|stays|stay)?"
    rf"(?:\s+(?:{_ADVERBS}))?"
    r"\s*(?:been\s+)?(?:placed|posited|positioned|located|situated)?"
    r"\s*(?:in|within)\s+"
    r"|"
    rf"(?:\s+(?:{_ADVERBS}))?\s+(?:occupies|occupy|occupying|tenants|tenanting)\s+"
    r")"
)

# House-first: "your 10th house is occupied by Saturn". A separate
# pattern rather than a reversible one, because the verbs differ — a
# house CONTAINS a planet, a planet OCCUPIES a house.
_CONTAINS = r"\s+(?:is\s+|are\s+)?(?:occupied\s+by|tenanted\s+by|contains|holds|has)\s+"

_CHART = r"(?:chart|kundli|kundali|kundali|horoscope|birth\s+chart|natal\s+chart)"


def _compile(pattern: str) -> re.Pattern[str]:
    return re.compile(pattern, re.IGNORECASE)


# ─── house ───────────────────────────────────────────────────────────

# "Saturn is in your 10th house", "Saturn occupies your tenth house"
HOUSE_CLAIM = _compile(rf"\b({_BODY})\b{_ASIDE}{_ADVERB}{_PLACES}your\s+{_HOUSE_NUMBER}\s+house")

# "your 10th house is occupied by Saturn", "your 4th house holds Saturn"
HOUSE_CLAIM_REVERSED = _compile(rf"\byour\s+{_HOUSE_NUMBER}\s+house{_CONTAINS}({_BODY})\b")

# "you have Saturn in the 10th house" — `you have` carries the possessive,
# so `the` is personal here in a way it is not on its own.
HOUSE_CLAIM_YOU_HAVE = _compile(
    rf"\byou\s+have\s+({_BODY})\b{_ADVERB}{_PLACES}(?:your|the)\s+{_HOUSE_NUMBER}\s+house"
)

# "Saturn in your chart is in the 10th house"
HOUSE_CLAIM_IN_CHART = _compile(
    rf"\b({_BODY})\b\s+in\s+your\s+{_CHART}{_ADVERB}{_PLACES}(?:your|the)\s+{_HOUSE_NUMBER}\s+house"
)

# ─── sign ────────────────────────────────────────────────────────────

SIGN_CLAIM = _compile(rf"\byour\s+({_BODY})\b{_ASIDE}{_ADVERB}{_PLACES}({_SIGN})\b")
SIGN_CLAIM_YOU_HAVE = _compile(rf"\byou\s+have\s+({_BODY})\b{_ADVERB}{_PLACES}({_SIGN})\b")
SIGN_CLAIM_IN_CHART = _compile(
    rf"\b({_BODY})\b\s+in\s+your\s+{_CHART}{_ADVERB}{_PLACES}({_SIGN})\b"
)

# ─── nakshatra ───────────────────────────────────────────────────────

NAKSHATRA_CLAIM = _compile(
    rf"\byour\s+({_BODY})\b{_ADVERB}{_PLACES}(?:the\s+)?({_NAKSHATRA})(?:\s+nakshatra)?\b"
)

# ─── ascendant and luminary signs ────────────────────────────────────

ASCENDANT_CLAIM = _compile(rf"\byour\s+(?:ascendant|lagna|rising\s+sign)\s+is\s+({_SIGN})\b")

# `moon_sign` and `sun_sign` are in the fact index because astro-service
# computes them, and until now nothing read them — the index carried two
# checkable facts that no pattern could ever contradict. "What's my moon
# sign" is among the commonest questions the product gets, so a wrong
# answer to it was unguarded.
MOON_SIGN_CLAIM = _compile(
    rf"\byour\s+(?:moon\s+sign|rashi|raashi|chandra\s+rashi)\s+is\s+({_SIGN})\b"
)
SUN_SIGN_CLAIM = _compile(rf"\byour\s+(?:sun\s+sign|surya\s+rashi)\s+is\s+({_SIGN})\b")

# ─── dasha ───────────────────────────────────────────────────────────

# "you are running Saturn mahadasha"
DASHA_CLAIM = _compile(
    rf"\byou(?:\s+are|'re)\s+(?:currently\s+)?(?:running|in|under)\s+(?:the\s+)?({_BODY})"
    rf"\s+(?:maha)?dasha"
)
# "your current mahadasha is Saturn", "your mahadasha lord is Saturn"
DASHA_CLAIM_IS = _compile(
    rf"\byour\s+(?:current\s+)?(?:maha)?dasha(?:\s+lord)?\s+is\s+(?:the\s+)?({_BODY})\b"
)
# "you are in the mahadasha of Saturn"
DASHA_CLAIM_OF = _compile(
    rf"\b(?:maha)?dasha\s+of\s+({_BODY})\b(?=[^.!?]{{0,30}}\byour\b)|"
    rf"\byour\b[^.!?]{{0,30}}?(?:maha)?dasha\s+of\s+({_BODY})\b"
)


class Claim(BaseModel):
    """One checkable statement, with enough context to show a reviewer."""

    kind: str
    """`house`, `sign`, `nakshatra`, `ascendant`, `moon_sign`, `sun_sign` or `dasha`."""

    subject: str
    """The body or chart element, canonicalised."""

    value: str
    """What the response asserted, as a string for uniform comparison."""

    excerpt: str
    """The matched text. SHORT — this ends up in a telemetry row, and a
    whole paragraph of a user's reading does not belong in one."""


def _house_number(raw: str) -> str | None:
    """ "10th", "10" or "tenth" to a canonical "10".

    `None` for a number outside 1..12: "your 40th house" is not a house
    claim, and treating it as one would produce a violation naming a
    house that does not exist.
    """
    lowered = raw.strip().lower()
    if lowered in _ORDINALS:
        return str(_ORDINALS[lowered])

    digits = re.sub(r"(?:st|nd|rd|th)$", "", lowered)
    if not digits.isdigit():
        return None
    number = int(digits)
    return str(number) if 1 <= number <= 12 else None


def _excerpt(match: re.Match[str]) -> str:
    return match.group(0).strip()[:120]


def _add_house(claims: list[Claim], body: str, house: str, match: re.Match[str]) -> None:
    number = _house_number(house)
    if number is None:
        return
    claims.append(
        Claim(kind="house", subject=canonical(body), value=number, excerpt=_excerpt(match))
    )


def extract_claims(text: str) -> list[Claim]:
    """Every personal, checkable claim in a response.

    Order is not significant and duplicates are kept: two identical
    claims in one response are two chances to be wrong, and collapsing
    them would understate how much of the answer rests on the mistake.
    """
    claims: list[Claim] = []

    for pattern in (HOUSE_CLAIM, HOUSE_CLAIM_YOU_HAVE, HOUSE_CLAIM_IN_CHART):
        for match in pattern.finditer(text):
            _add_house(claims, match.group(1), match.group(2), match)

    for match in HOUSE_CLAIM_REVERSED.finditer(text):
        # Groups are the other way round in the house-first form.
        _add_house(claims, match.group(2), match.group(1), match)

    for pattern in (SIGN_CLAIM, SIGN_CLAIM_YOU_HAVE, SIGN_CLAIM_IN_CHART):
        for match in pattern.finditer(text):
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

    for pattern, kind in (
        (ASCENDANT_CLAIM, "ascendant"),
        (MOON_SIGN_CLAIM, "moon_sign"),
        (SUN_SIGN_CLAIM, "sun_sign"),
    ):
        for match in pattern.finditer(text):
            claims.append(
                Claim(
                    kind=kind,
                    subject=kind,
                    value=canonical(match.group(1)),
                    excerpt=_excerpt(match),
                )
            )

    for pattern in (DASHA_CLAIM, DASHA_CLAIM_IS, DASHA_CLAIM_OF):
        for match in pattern.finditer(text):
            body = next((g for g in match.groups() if g), None)
            if body is None:
                continue
            claims.append(
                Claim(
                    kind="dasha",
                    subject="dasha",
                    value=canonical(body),
                    excerpt=_excerpt(match),
                )
            )

    return claims
