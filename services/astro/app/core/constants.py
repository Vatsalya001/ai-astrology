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
from typing import Final

# ─── the circle ──────────────────────────────────────────────────────

FULL_CIRCLE: Final = 360.0

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


def normalise_longitude(longitude: float) -> float:
    """Fold any angle into [0, 360).

    Every longitude in this codebase passes through here. Doing it in one
    place means a negative intermediate result — which arithmetic on
    angles produces constantly — cannot leak into a sign index and
    silently produce house 13.
    """
    return longitude % FULL_CIRCLE


def sign_index(longitude: float) -> int:
    """0 = Aries … 11 = Pisces."""
    return int(normalise_longitude(longitude) // SIGN_ARC)


def degree_in_sign(longitude: float) -> float:
    """How far into its sign a longitude sits, in [0, 30)."""
    return normalise_longitude(longitude) % SIGN_ARC


def nakshatra_index(longitude: float) -> int:
    """0 = Ashwini … 26 = Revati."""
    return int(normalise_longitude(longitude) // NAKSHATRA_ARC)


def pada(longitude: float) -> int:
    """Which quarter of the nakshatra, 1-4."""
    return int((normalise_longitude(longitude) % NAKSHATRA_ARC) // PADA_ARC) + 1
