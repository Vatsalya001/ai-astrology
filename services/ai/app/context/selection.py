"""Which parts of a chart each intent needs. PHASE-05 §3.

§3 gives a table for eleven intents and the `Intent` enum has
twenty-one, so nine of these rows are not in the spec and are derived
here. Each one says where it came from, because a selection nobody can
justify is a selection nobody can correct.

── Why selection exists at all ──

§3: "~700 tokens of precise, relevant, true facts. Versus dumping the
chart: ~3,000 tokens, most irrelevant, answer noticeably vaguer."

The cost argument is the smaller one. The real argument is that a career
question answered from the whole chart invites the model to reach for
whatever placement is most dramatic, and the most dramatic placement in
any chart is rarely the relevant one.

── Why four intents select NOTHING ──

MEDICAL, LEGAL, TAROT, NUMEROLOGY, HUMAN_ASTROLOGER and OTHER get an
empty selection, and that is a safety property rather than a saving.

The response to a medical question is a boundary statement, not a
reading. Supplying chart facts for one would let the model build a
health interpretation out of them — and an empty fact index means the
validator's strict branch fires on any personal claim, so the model
*cannot* make one without being blocked. The safety layer refuses the
question; this makes the refusal unforgeable.
"""

from __future__ import annotations

from dataclasses import dataclass, field

from app.classification import Intent

# The nine planets, in the classical order. Used where a selection wants
# "everything" rather than a short list.
ALL_PLANETS: tuple[str, ...] = (
    "sun",
    "moon",
    "mars",
    "mercury",
    "jupiter",
    "venus",
    "saturn",
    "rahu",
    "ketu",
)


@dataclass(frozen=True)
class Selection:
    """What to pull from the chart for one intent."""

    houses: tuple[int, ...] = ()
    """Houses whose sign, lord and occupants are included, in this order.

    Order matters: it is the order they appear in the prompt, and §4's
    "stable content first, volatile content last" applies within the
    context block too. The primary house for the intent goes first.
    """

    planets: tuple[str, ...] = ()
    """Planets whose full position is included."""

    house_lords: tuple[int, ...] = ()
    """Houses whose LORD's placement is included.

    Separate from `houses` because the two answer different questions.
    "What sign is on the 10th" is a house fact; "where is the 10th lord"
    is a derivation, and it is the one that does the work in a real
    reading — §3's worked example leads with it.
    """

    include_dasha: bool = True
    """The current mahadasha and antardasha.

    On by default. A reading that does not know what period the native is
    in is answering a different question from the one they asked, and the
    dasha is two lines of context.
    """

    include_dasha_tree: bool = False
    """The full three-level tree. Only DASHA asks for this."""

    include_ascendant: bool = True

    include_moon_nakshatra: bool = False
    """The Moon's nakshatra specifically.

    Not the same as including the Moon: the nakshatra is the most
    specific thing the chart says about emotional nature, and it anchors
    the whole dasha system. EMOTIONAL_SUPPORT needs it; CAREER does not.
    """

    notes: str = field(default="")
    """Why this row looks the way it does. Carried into the context's
    provenance so a reviewer can tell a spec row from a derived one."""

    from_spec: bool = False
    """True when §3's table defines this row verbatim."""

    @property
    def is_empty(self) -> bool:
        return not (
            self.houses
            or self.planets
            or self.house_lords
            or self.include_dasha
            or self.include_ascendant
        )


# An intent that supplies no chart facts at all. See the module docstring.
NOTHING = Selection(include_dasha=False, include_ascendant=False)


SELECTIONS: dict[Intent, Selection] = {
    # ── §3's table, verbatim ──
    Intent.CAREER: Selection(
        houses=(10, 6, 2, 11),
        planets=("saturn", "sun", "mercury"),
        house_lords=(10,),
        from_spec=True,
        notes="§3: houses 10, 6, 2, 11; 10th lord, Saturn, Sun, Mercury, current dasha",
    ),
    Intent.MARRIAGE: Selection(
        houses=(7, 2, 4, 8),
        planets=("venus", "jupiter", "mars"),
        house_lords=(7,),
        from_spec=True,
        notes="§3: houses 7, 2, 4, 8; 7th lord, Venus, Jupiter, Mangal dosha. "
        "Mars is included FOR the dosha check, which needs its house",
    ),
    Intent.RELATIONSHIP: Selection(
        houses=(5, 7, 11),
        planets=("venus", "moon"),
        house_lords=(5, 7),
        from_spec=True,
        notes="§3: houses 5, 7, 11; Venus, Moon, 5th and 7th lords",
    ),
    Intent.FINANCE: Selection(
        houses=(2, 11, 5, 9),
        planets=("jupiter", "venus"),
        house_lords=(2, 11),
        from_spec=True,
        notes="§3: houses 2, 11, 5, 9; 2nd and 11th lords, Jupiter, Venus",
    ),
    Intent.EDUCATION: Selection(
        houses=(4, 5, 9),
        planets=("mercury", "jupiter"),
        house_lords=(5,),
        from_spec=True,
        notes="§3: houses 4, 5, 9; Mercury, Jupiter, 5th lord",
    ),
    Intent.FAMILY: Selection(
        houses=(2, 4, 3, 9),
        planets=("moon", "sun", "jupiter"),
        house_lords=(4,),
        from_spec=True,
        notes="§3: houses 2, 4, 3, 9; Moon, Sun, Jupiter. The 4th lord is added "
        "because §3 lists the Moon and the 4th house and the lord ties them",
    ),
    Intent.TRAVEL: Selection(
        houses=(3, 9, 12),
        planets=("rahu",),
        house_lords=(12,),
        from_spec=True,
        notes="§3: houses 3, 9, 12; Rahu, 12th lord",
    ),
    Intent.RELOCATION: Selection(
        houses=(12, 9, 4, 3),
        planets=("rahu", "saturn"),
        house_lords=(12, 4),
        from_spec=True,
        notes="§3 pairs TRAVEL and RELOCATION. Split here: relocation leads with "
        "the 12th (residence abroad) and adds the 4th — what is being LEFT — "
        "because a relocation reading that ignores the 4th describes half the "
        "decision. See the travel guidance document",
    ),
    Intent.DASHA: Selection(
        include_dasha_tree=True,
        from_spec=True,
        notes="§3: no houses; full 3-level dasha tree",
    ),
    Intent.TRANSIT: Selection(
        planets=ALL_PLANETS,
        from_spec=True,
        notes="§3: all current transits vs natal, Sade Sati. Every natal position "
        "is needed because a transit is read AGAINST the natal chart, and "
        "Sade Sati is counted from the natal Moon",
    ),
    Intent.EMOTIONAL_SUPPORT: Selection(
        houses=(4,),
        planets=("moon",),
        include_moon_nakshatra=True,
        from_spec=True,
        notes="§3: house 4; Moon placement, Moon nakshatra, Chandra yogas",
    ),
    Intent.GENERAL_ASTROLOGY: Selection(
        houses=(1, 10, 7),
        planets=("sun", "moon", "saturn"),
        house_lords=(1,),
        include_moon_nakshatra=True,
        from_spec=True,
        notes="§3: houses 1, 10, 7; summary + current dasha + major transits. "
        "The ascendant lord and Moon nakshatra are the two anchors the "
        "general-question guidance says to lead with",
    ),
    # ── Derived. Not in §3's table ──
    Intent.KUNDLI: Selection(
        houses=(1, 10, 7, 4),
        planets=ALL_PLANETS,
        house_lords=(1,),
        include_moon_nakshatra=True,
        notes="DERIVED. A kundli question is about the chart itself — 'why is my "
        "sign different', 'what is a navamsa' — so it gets every position, "
        "because the answer is often a comparison. The widest selection here, "
        "and the one intent where a full dump is the right answer",
    ),
    Intent.DAILY_HOROSCOPE: Selection(
        houses=(1,),
        planets=("moon", "sun", "mars", "jupiter", "saturn"),
        include_moon_nakshatra=True,
        notes="DERIVED. Transit-driven, so it needs the natal anchors a transit is "
        "read against: the ascendant, the Moon (gochara is counted from it) and "
        "the slow planets whose transits actually matter",
    ),
    Intent.COMPATIBILITY: Selection(
        houses=(7, 5, 8),
        planets=("venus", "mars", "moon", "jupiter"),
        house_lords=(7,),
        include_moon_nakshatra=True,
        notes="DERIVED. Ashtakoota scores primarily from the two Moons' "
        "nakshatras, so the Moon nakshatra is mandatory. Mars is for the "
        "Mangal dosha check and its cancellation, which the compatibility "
        "guidance requires be stated with its frequency",
    ),
    # ── Deliberately empty. A safety property, not a saving ──
    Intent.MEDICAL: Selection(
        include_dasha=False,
        include_ascendant=False,
        notes="EMPTY ON PURPOSE. The response is a boundary statement, not a "
        "reading. With no facts the validator's strict empty-index branch "
        "fires on any personal claim, so the model cannot build a health "
        "interpretation even if the safety layer were bypassed",
    ),
    Intent.LEGAL: Selection(
        include_dasha=False,
        include_ascendant=False,
        notes="EMPTY ON PURPOSE. Same reasoning as MEDICAL. The outcome of a case "
        "depends on evidence and procedure, neither of which is in a chart",
    ),
    Intent.TAROT: Selection(
        include_dasha=False,
        include_ascendant=False,
        notes="EMPTY ON PURPOSE. Out of scope; this product computes planetary "
        "positions and has no card deck. Supplying chart facts would invite "
        "an improvised answer",
    ),
    Intent.NUMEROLOGY: Selection(
        include_dasha=False,
        include_ascendant=False,
        notes="EMPTY ON PURPOSE. Out of scope, same as TAROT",
    ),
    Intent.HUMAN_ASTROLOGER: Selection(
        include_dasha=False,
        include_ascendant=False,
        notes="EMPTY ON PURPOSE. The answer is how to reach a person. A request "
        "for a human is not an objection to overcome",
    ),
    Intent.OTHER: Selection(
        include_dasha=False,
        include_ascendant=False,
        notes="EMPTY ON PURPOSE. The classifier could not place the message. A "
        "narrow guess retrieves the wrong facts confidently, which PHASE-04 §6 "
        "says is worse than a broad one — and empty is the broadest",
    ),
}


def selection_for(intent: Intent) -> Selection:
    """The selection for an intent, never a KeyError.

    An intent with no row is a bug, and the safe failure is the EMPTY
    selection rather than a default that supplies facts: a new intent
    nobody mapped should produce a response that cannot make personal
    claims, not one that can make them about an arbitrary subset of the
    chart.

    `test_every_intent_has_a_selection` makes the bug a test failure
    rather than a runtime surprise, so this fallback should never be
    reached in a shipped build.
    """
    return SELECTIONS.get(intent, NOTHING)
