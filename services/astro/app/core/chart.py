"""The rasi (D1) chart: ascendant, houses and placed grahas.

Pure. The instant, the place and the ephemeris all arrive as arguments.

── The ascendant is the fragile part ──

It moves roughly one degree every four minutes, so a 30-minute error in
the birth time can change the rising sign and with it every house
placement in the chart. That is why `time_accuracy` is a first-class
concept upstream and why an unknown birth time returns no ascendant at
all rather than a plausible guess.

It is also the one quantity here that depends on the birth PLACE as well
as the instant, which makes it the only place a latitude or longitude
mistake can hide.
"""

from __future__ import annotations

import math
from dataclasses import dataclass

from skyfield.timelib import Time

from app.core.ayanamsa import AyanamsaCalculator, AyanamsaSystem
from app.core.constants import (
    COMBUSTION_ORB,
    EXALTATION,
    HOUSE_COUNT,
    MOOLATRIKONA,
    NAKSHATRAS,
    NODES,
    PADA_COUNT,
    SIGN_ARC,
    SIGN_LORDS,
    SIGNS,
    Planet,
    degree_in_sign,
    nakshatra_index,
    normalise_longitude,
    pada,
    sign_index,
    subdivision_index,
)
from app.core.ephemeris import EphemerisProvider


@dataclass(frozen=True, slots=True)
class PlacedPlanet:
    """A graha, located and characterised."""

    planet: Planet
    longitude: float
    """Sidereal ecliptic longitude, [0, 360)."""

    sign: str
    sign_index: int
    degree: float
    house: int
    nakshatra: str
    nakshatra_index: int
    pada: int
    is_retrograde: bool
    is_combust: bool
    dignity: str
    speed: float


@dataclass(frozen=True, slots=True)
class Ascendant:
    """The rising point."""

    longitude: float
    sign: str
    sign_index: int
    degree: float
    nakshatra: str
    pada: int


@dataclass(frozen=True, slots=True)
class House:
    """One of the twelve bhavas, whole-sign."""

    house: int
    sign: str
    sign_index: int
    lord: Planet
    planets: tuple[Planet, ...]


@dataclass(frozen=True, slots=True)
class Rasi:
    """The D1 chart.

    `ascendant` and `houses` are None when the birth time is unknown.
    Modelled as absent rather than defaulted because the honest answer to
    "what is my rising sign" without a birth time is "we cannot know",
    and a type that cannot express absence forces a lie.
    """

    ascendant: Ascendant | None
    houses: tuple[House, ...] | None
    planets: tuple[PlacedPlanet, ...]
    ayanamsa_value: float


def obliquity_of_the_ecliptic(t: Time) -> float:
    """Mean obliquity in degrees — IAU 1980, valid across the kernel span.

    The tilt of the Earth's axis relative to its orbit. It drifts by
    about 47 arcseconds a century, which is small but not nothing over
    the 150 years of births this service handles.
    """
    centuries = float(t.tt - 2451545.0) / 36525.0
    seconds = 21.448 - centuries * (46.8150 + centuries * (0.00059 - centuries * 0.001813))
    return 23.0 + (26.0 + seconds / 60.0) / 60.0


def local_sidereal_degrees(t: Time, longitude_east: float) -> float:
    """Local apparent sidereal time, in degrees.

    Sidereal rather than solar time because the ascendant tracks the
    stars, not the Sun: the rising point completes a full circle in one
    sidereal day, about four minutes short of twenty-four hours.
    """
    return normalise_longitude(t.gast * 15.0 + longitude_east)


def tropical_ascendant(t: Time, latitude: float, longitude_east: float) -> float:
    """The tropical longitude rising on the eastern horizon.

    The standard spherical-trigonometry solution for where the ecliptic
    meets the horizon.

    `atan2` rather than `atan`: the naive form loses the quadrant and
    puts the ascendant on the *western* horizon for half of every day —
    a failure that produces a chart six signs out, which still looks like
    a chart.
    """
    ramc = math.radians(local_sidereal_degrees(t, longitude_east))
    obliquity = math.radians(obliquity_of_the_ecliptic(t))
    phi = math.radians(latitude)

    ascendant = math.atan2(
        math.cos(ramc),
        -(math.sin(ramc) * math.cos(obliquity) + math.tan(phi) * math.sin(obliquity)),
    )
    return normalise_longitude(math.degrees(ascendant))


def _dignity(planet: Planet, longitude: float) -> str:
    """Exalted, debilitated, own sign, moolatrikona, or neutral.

    The nodes are always neutral: dignity is ownership of a sign, and a
    mathematical point owns nothing.
    """
    if planet in NODES:
        return "neutral"

    index = sign_index(longitude)
    degrees = degree_in_sign(longitude)

    exaltation = EXALTATION.get(planet)
    if exaltation is not None:
        if sign_index(exaltation) == index:
            return "exalted"
        if sign_index(exaltation + 180.0) == index:
            return "debilitated"

    trikona = MOOLATRIKONA.get(planet)
    if trikona is not None:
        trikona_sign, low, high = trikona
        if index == trikona_sign and low <= degrees < high:
            return "moolatrikona"

    if SIGN_LORDS[index] == planet:
        return "own_sign"

    return "neutral"


def _is_combust(planet: Planet, longitude: float, sun_longitude: float) -> bool:
    """Too close to the Sun to be seen, and so considered weakened.

    The Sun cannot be combust by its own light, and the nodes have no
    body to be burnt — both are excluded rather than allowed to fall
    through a lookup and silently return False for the wrong reason.
    """
    if planet is Planet.SUN or planet in NODES:
        return False

    orb = COMBUSTION_ORB.get(planet)
    if orb is None:
        return False

    separation = abs(normalise_longitude(longitude - sun_longitude))
    if separation > 180.0:
        separation = 360.0 - separation
    return separation <= orb


def _house_of(longitude: float, ascendant_sign: int) -> int:
    """Whole-sign house number, 1-12.

    The ascendant's whole sign is house 1, the next sign house 2, and so
    on — the Vedic default. Simple enough that the only way to get it
    wrong is an unfolded negative, which `normalise_longitude` prevents
    upstream.
    """
    return (sign_index(longitude) - ascendant_sign) % HOUSE_COUNT + 1


def compute_rasi(
    ephemeris: EphemerisProvider,
    ayanamsa: AyanamsaCalculator,
    t: Time,
    latitude: float,
    longitude_east: float,
    *,
    time_known: bool = True,
    system: AyanamsaSystem = AyanamsaSystem.LAHIRI,
) -> Rasi:
    """Build the D1 chart.

    `time_known=False` is the unknown-birth-time case. Planetary
    longitudes are still meaningful — the Moon moves about 13° a day, so
    a midday assumption puts it within half a sign — but the ascendant
    moves a full circle in a day and cannot be estimated at all. It, the
    houses, and every planet's house are therefore omitted rather than
    computed from a fabricated time.
    """
    offset = ayanamsa.at_time(t, system)
    tropical = ephemeris.positions(t)

    sidereal = {
        planet: normalise_longitude(position.longitude - offset)
        for planet, position in tropical.items()
    }
    sun_longitude = sidereal[Planet.SUN]

    ascendant: Ascendant | None = None
    ascendant_sign: int | None = None

    if time_known:
        ascendant_longitude = normalise_longitude(
            tropical_ascendant(t, latitude, longitude_east) - offset
        )
        ascendant_sign = sign_index(ascendant_longitude)
        ascendant = Ascendant(
            longitude=ascendant_longitude,
            sign=SIGNS[ascendant_sign],
            sign_index=ascendant_sign,
            degree=degree_in_sign(ascendant_longitude),
            nakshatra=NAKSHATRAS[nakshatra_index(ascendant_longitude)],
            pada=pada(ascendant_longitude),
        )

    placed: list[PlacedPlanet] = []
    for planet, longitude in sidereal.items():
        index = sign_index(longitude)
        placed.append(
            PlacedPlanet(
                planet=planet,
                longitude=longitude,
                sign=SIGNS[index],
                sign_index=index,
                degree=degree_in_sign(longitude),
                # 0 means "not applicable" — the birth time is unknown, so
                # there are no houses to place anything in.
                house=_house_of(longitude, ascendant_sign) if ascendant_sign is not None else 0,
                nakshatra=NAKSHATRAS[nakshatra_index(longitude)],
                nakshatra_index=nakshatra_index(longitude),
                pada=pada(longitude),
                # The nodes are always retrograde; for bodies the sign of
                # the daily motion decides.
                is_retrograde=(planet in NODES) or tropical[planet].is_retrograde,
                is_combust=_is_combust(planet, longitude, sun_longitude),
                dignity=_dignity(planet, longitude),
                speed=tropical[planet].speed,
            )
        )

    houses: tuple[House, ...] | None = None
    if ascendant_sign is not None:
        houses = tuple(
            House(
                house=number,
                sign=SIGNS[(ascendant_sign + number - 1) % HOUSE_COUNT],
                sign_index=(ascendant_sign + number - 1) % HOUSE_COUNT,
                lord=SIGN_LORDS[(ascendant_sign + number - 1) % HOUSE_COUNT],
                planets=tuple(p.planet for p in placed if p.house == number),
            )
            for number in range(1, HOUSE_COUNT + 1)
        )

    return Rasi(
        ascendant=ascendant,
        houses=houses,
        planets=tuple(placed),
        ayanamsa_value=offset,
    )


#: One navamsa: 3°20', a ninth of a sign.
NAVAMSA_ARC = SIGN_ARC / 9


def navamsa_longitude(longitude: float) -> float:
    """Map a rasi longitude into the D9 chart.

    Each sign divides into nine navamsas of 3°20', and where they land is
    traditionally stated per element:

      fire  (Aries, Leo, Sagittarius)     count from Aries
      earth (Taurus, Virgo, Capricorn)    count from Capricorn
      air   (Gemini, Libra, Aquarius)     count from Libra
      water (Cancer, Scorpio, Pisces)     count from Cancer

    All four rules collapse into a single continuous count, because 108
    navamsas divided by 12 signs is exactly 9 whole cycles: the navamsa
    sign is just `floor(longitude / 3°20') mod 12`, walking the zodiac
    without restarting. The element rule is what that count looks like
    when you tabulate it per sign.

    Worth stating because the first version of this function did NOT use
    the continuous count. It used a per-element offset computed as
    `(sign_index % 4) * 3`, which put Taurus's navamsa in Cancer instead
    of Capricorn — earth and water swapped — and carried a comment
    claiming it had "no table to mistype". It had a formula to mistype
    instead, and did. The continuous form has neither.
    """
    # Shares subdivision_index with `pada`, which is the same 3°20'
    # division — and shares its float fix. Dividing by 3.333… first put
    # the start of Leo in Pisces rather than Aries.
    navamsa_index = subdivision_index(longitude, PADA_COUNT)
    target_sign = navamsa_index % 12

    # Position within the navamsa, expanded to fill the destination sign:
    # a 3°20' slice maps onto a full 30°.
    exact = normalise_longitude(longitude) * PADA_COUNT / 360.0
    within = (exact - navamsa_index) * SIGN_ARC

    return normalise_longitude(target_sign * SIGN_ARC + within)


def compute_navamsa(rasi: Rasi) -> Rasi:
    """Derive the D9 chart from the D1.

    Derived rather than recomputed from the ephemeris: D9 is a pure
    transformation of D1 longitudes, so recomputing would introduce a
    second path to the same answer and therefore a way for the two to
    disagree.
    """
    ascendant: Ascendant | None = None
    ascendant_sign: int | None = None

    if rasi.ascendant is not None:
        longitude = navamsa_longitude(rasi.ascendant.longitude)
        ascendant_sign = sign_index(longitude)
        ascendant = Ascendant(
            longitude=longitude,
            sign=SIGNS[ascendant_sign],
            sign_index=ascendant_sign,
            degree=degree_in_sign(longitude),
            nakshatra=NAKSHATRAS[nakshatra_index(longitude)],
            pada=pada(longitude),
        )

    placed: list[PlacedPlanet] = []
    for planet in rasi.planets:
        longitude = navamsa_longitude(planet.longitude)
        index = sign_index(longitude)
        placed.append(
            PlacedPlanet(
                planet=planet.planet,
                longitude=longitude,
                sign=SIGNS[index],
                sign_index=index,
                degree=degree_in_sign(longitude),
                house=_house_of(longitude, ascendant_sign) if ascendant_sign is not None else 0,
                nakshatra=NAKSHATRAS[nakshatra_index(longitude)],
                nakshatra_index=nakshatra_index(longitude),
                pada=pada(longitude),
                is_retrograde=planet.is_retrograde,
                # Combustion and dignity are properties of the rasi
                # placement. Recomputing them against D9 longitudes would
                # produce confident nonsense — the Sun's D9 position is
                # not where the Sun is.
                is_combust=planet.is_combust,
                dignity=_dignity(planet.planet, longitude),
                speed=planet.speed,
            )
        )

    houses: tuple[House, ...] | None = None
    if ascendant_sign is not None:
        houses = tuple(
            House(
                house=number,
                sign=SIGNS[(ascendant_sign + number - 1) % HOUSE_COUNT],
                sign_index=(ascendant_sign + number - 1) % HOUSE_COUNT,
                lord=SIGN_LORDS[(ascendant_sign + number - 1) % HOUSE_COUNT],
                planets=tuple(p.planet for p in placed if p.house == number),
            )
            for number in range(1, HOUSE_COUNT + 1)
        )

    return Rasi(
        ascendant=ascendant,
        houses=houses,
        planets=tuple(placed),
        ayanamsa_value=rasi.ayanamsa_value,
    )
