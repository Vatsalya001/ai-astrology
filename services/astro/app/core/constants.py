"""The fixed vocabulary of Vedic astrology.

Pure data. No I/O, no clock, no imports outside the standard library —
this module is the bottom of `core` and everything else builds on it.

Names are the Sanskrit-derived forms in common use in India rather than
the Latin ones, because that is what the product's users read. The
English sign names are kept alongside because the UI and the AI layer in
later phases both need them.
"""

from __future__ import annotations

from enum import StrEnum
from typing import Final, Literal

# ─── the circle ──────────────────────────────────────────────────────

FULL_CIRCLE: Final = 360.0

# The same constant as an int, for the exact integer arithmetic in
# subdivision_index. Named rather than inlined so the two cannot drift.
FULL_CIRCLE_INT: Final = 360

SIGN_COUNT: Final = 12
SIGN_ARC: Final = FULL_CIRCLE / SIGN_COUNT  # 30°

NAKSHATRA_COUNT: Final = 27
NAKSHATRA_ARC: Final = FULL_CIRCLE / NAKSHATRA_COUNT  # 13°20'

PADAS_PER_NAKSHATRA: Final = 4
PADA_ARC: Final = NAKSHATRA_ARC / PADAS_PER_NAKSHATRA  # 3°20'

HOUSE_COUNT: Final = 12


class Planet(StrEnum):
    """The nine grahas.

    Uranus, Neptune and Pluto are deliberately absent: classical Vedic
    astrology does not use them, and including them would invite
    interpretations the tradition has no basis for.
    """

    SUN = "Sun"
    MOON = "Moon"
    MARS = "Mars"
    MERCURY = "Mercury"
    JUPITER = "Jupiter"
    VENUS = "Venus"
    SATURN = "Saturn"
    RAHU = "Rahu"
    KETU = "Ketu"


#: Rahu and Ketu are the lunar nodes — mathematical points, not bodies.
#: They are always retrograde and have no physical position to observe,
#: which is why several calculations must special-case them.
NODES: Final = (Planet.RAHU, Planet.KETU)

#: The seven grahas that are actual bodies.
BODIES: Final = tuple(p for p in Planet if p not in NODES)


SIGNS: Final = (
    "Aries",
    "Taurus",
    "Gemini",
    "Cancer",
    "Leo",
    "Virgo",
    "Libra",
    "Scorpio",
    "Sagittarius",
    "Capricorn",
    "Aquarius",
    "Pisces",
)

#: The sign each planet rules. Used for house lords and for dignity.
#: The nodes rule nothing — a point cannot own a sign.
SIGN_LORDS: Final = (
    Planet.MARS,  # Aries
    Planet.VENUS,  # Taurus
    Planet.MERCURY,  # Gemini
    Planet.MOON,  # Cancer
    Planet.SUN,  # Leo
    Planet.MERCURY,  # Virgo
    Planet.VENUS,  # Libra
    Planet.MARS,  # Scorpio
    Planet.JUPITER,  # Sagittarius
    Planet.SATURN,  # Capricorn
    Planet.SATURN,  # Aquarius
    Planet.JUPITER,  # Pisces
)

NAKSHATRAS: Final = (
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

#: Vimshottari dasha lords in sequence, with their years.
#:
#: The order is fixed and the total is exactly 120 — both are load-bearing.
#: The sequence repeats indefinitely; the starting point is set by the
#: nakshatra the Moon occupied at birth.
VIMSHOTTARI_SEQUENCE: Final = (
    (Planet.KETU, 7),
    (Planet.VENUS, 20),
    (Planet.SUN, 6),
    (Planet.MOON, 10),
    (Planet.MARS, 7),
    (Planet.RAHU, 18),
    (Planet.JUPITER, 16),
    (Planet.SATURN, 19),
    (Planet.MERCURY, 17),
)

VIMSHOTTARI_TOTAL_YEARS: Final = 120

#: Exaltation point for each planet, as an absolute sidereal longitude.
#: A planet is "exalted" in the sign containing this degree and
#: "debilitated" in the opposite sign.
EXALTATION: Final = {
    Planet.SUN: 10.0,  # 10° Aries
    Planet.MOON: 33.0,  # 3° Taurus
    Planet.MARS: 298.0,  # 28° Capricorn
    Planet.MERCURY: 165.0,  # 15° Virgo
    Planet.JUPITER: 95.0,  # 5° Cancer
    Planet.VENUS: 357.0,  # 27° Pisces
    Planet.SATURN: 200.0,  # 20° Libra
}

#: Moolatrikona ranges, as (sign index, start degree, end degree).
#: A planet here is stronger than in its own sign but weaker than exalted.
MOOLATRIKONA: Final = {
    Planet.SUN: (4, 0.0, 20.0),  # Leo 0-20
    Planet.MOON: (1, 3.0, 30.0),  # Taurus 3-30
    Planet.MARS: (0, 0.0, 12.0),  # Aries 0-12
    Planet.MERCURY: (5, 15.0, 20.0),  # Virgo 15-20
    Planet.JUPITER: (8, 0.0, 10.0),  # Sagittarius 0-10
    Planet.VENUS: (6, 0.0, 15.0),  # Libra 0-15
    Planet.SATURN: (10, 0.0, 20.0),  # Aquarius 0-20
}

#: Combustion orbs, in degrees from the Sun.
#:
#: A planet too close to the Sun is "burnt" and considered weakened. The
#: orb differs per planet and, traditionally, by retrograde state; the
#: simpler direct-motion orbs are used here and the distinction is left
#: for a later phase rather than guessed at now.
COMBUSTION_ORB: Final = {
    Planet.MOON: 12.0,
    Planet.MARS: 17.0,
    Planet.MERCURY: 14.0,
    Planet.JUPITER: 11.0,
    Planet.VENUS: 10.0,
    Planet.SATURN: 15.0,
}

#: Special aspects beyond the 7th house, which every planet has.
#:
#: Counted inclusively in the Vedic manner: the planet's own house is 1,
#: so "aspects the 7th" means six houses forward.
SPECIAL_ASPECTS: Final = {
    Planet.MARS: (4, 8),
    Planet.JUPITER: (5, 9),
    Planet.SATURN: (3, 10),
}

UNIVERSAL_ASPECT: Final = 7

#: The closed vocabulary of planetary dignity.
#:
#: A Literal rather than a bare str so a typo — "exalt", "own sign" —
#: fails at the type level rather than travelling to the API boundary and
#: being rejected there, or worse, reaching a UI that silently renders
#: nothing for an unrecognised value.
Dignity = Literal["exalted", "debilitated", "own_sign", "moolatrikona", "neutral"]


def normalise_longitude(longitude: float) -> float:
    """Fold any angle into [0, 360).

    Every longitude in this codebase passes through here. Doing it in one
    place means a negative intermediate result — which arithmetic on
    angles produces constantly — cannot leak into a sign index and
    silently produce house 13.

    The explicit fold back to zero is not defensive padding. Python's `%`
    returns a non-negative result, but for a tiny negative input the true
    answer is a hair below 360 and the nearest float IS 360.0:

        (-1e-18) % 360.0  ==  360.0

    which gave sign index 12 — the exact failure this function promises
    to prevent, in the one line meant to prevent it. A value like -1e-18
    is what subtracting two nearly-equal angles produces, which this
    codebase does on every chart when it converts tropical to sidereal.
    """
    folded = longitude % FULL_CIRCLE
    return 0.0 if folded >= FULL_CIRCLE else folded


def subdivision_index(longitude: float, divisions: int) -> int:
    """Which of `divisions` equal arcs a longitude falls in.

    Computed on the float's EXACT value, in integer arithmetic. A float
    is a rational number: `as_integer_ratio` gives its numerator and
    denominator with no loss at all, and the comparison then has no
    rounding step to get wrong.

    Why not the obvious float form, in either ordering. 360/27 is not
    representable in binary, so both `x * 27 / 360` and `x / (360 / 27)`
    round, and both land on the wrong side of a boundary for some inputs.
    Measured against exact arithmetic in `tests/test_subdivision.py`:

        boundaries and their float neighbours, 27 arcs
            x * d / 360     wrong at 4 of 81
            x / (360 / d)   wrong at 1 of 81
            this function   wrong at 0 of 81

    An earlier version of this code used `x / (360 / d)`; I changed it to
    `x * d / 360` and called that a correctness fix, citing nine of
    twenty-seven failing boundaries. Both halves of that were wrong. The
    count was recalled rather than measured, and multiply-first is in
    fact wrong MORE often, not less. The measurement is in the test now,
    so neither claim has to be taken on trust again.

    Practical impact of any of this: none. Over 500,000 random
    longitudes all three forms agree exactly, because they diverge only
    within one ulp of a boundary and a real planet is never there. The
    reason to be exact anyway is that the alternative is a comment
    asserting something no test checks — and this arithmetic sits under
    every nakshatra, pada and sign in the product.

    Cost: 268 ns against 82 ns for the float form. About 0.011 ms per
    chart, against a 200 ms budget.

    The failure it prevents, when it does happen, is not subtle: a planet
    on a nakshatra cusp reported in the previous nakshatra gets a
    different pada, a different dasha lord, and therefore an entirely
    different Vimshottari tree.
    """
    numerator, denominator = normalise_longitude(longitude).as_integer_ratio()
    return (numerator * divisions) // (denominator * FULL_CIRCLE_INT)


def sign_index(longitude: float) -> int:
    """0 = Aries … 11 = Pisces."""
    return subdivision_index(longitude, SIGN_COUNT)


def degree_in_sign(longitude: float) -> float:
    """How far into its sign a longitude sits, in [0, 30)."""
    return normalise_longitude(longitude) % SIGN_ARC


def nakshatra_index(longitude: float) -> int:
    """0 = Ashwini … 26 = Revati."""
    return subdivision_index(longitude, NAKSHATRA_COUNT)


#: 108 arcs of 3°20' around the zodiac.
#:
#: A pada and a navamsa are the SAME division: 27 nakshatras by 4 padas
#: and 12 signs by 9 navamsas both give 108. Sharing one constant makes
#: that identity explicit instead of leaving two 3°20' subdivisions to
#: drift apart.
PADA_COUNT: Final = NAKSHATRA_COUNT * PADAS_PER_NAKSHATRA

# Ten dasamsas per sign, 120 around the circle. Named because 120 also
# happens to be the Vimshottari cycle in years, and an unexplained 120
# in this file would read as that.
DASAMSAS_PER_SIGN: Final = 10
DASAMSA_COUNT: Final = SIGN_COUNT * DASAMSAS_PER_SIGN


def pada(longitude: float) -> int:
    """Which quarter of the nakshatra, 1-4."""
    return subdivision_index(longitude, PADA_COUNT) % PADAS_PER_NAKSHATRA + 1
