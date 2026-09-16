"""The rasi (D1) and navamsa (D9) charts.

The properties §9 of the spec names, plus the ones that catch the
failures this code can actually have: an ascendant on the wrong horizon,
a house count that is not a permutation, and a navamsa element rule with
two of its four cases transposed.
"""

from __future__ import annotations

import math
from itertools import pairwise

import pytest
from hypothesis import given, settings
from hypothesis import strategies as st
from skyfield import almanac
from skyfield.api import wgs84

from app.core.chart import (
    HOUSE_COUNT,
    compute_navamsa,
    compute_rasi,
    navamsa_longitude,
    tropical_ascendant,
)
from app.core.constants import SIGNS, Planet, sign_index

# Jaipur, and a birth instant of 14:35 IST on 17 August 1994.
JAIPUR_LAT = 26.9124
JAIPUR_LON = 75.7873


@pytest.fixture(scope="session")
def jaipur_chart(ephemeris, ayanamsa, timescale):
    t = timescale.utc(1994, 8, 17, 9, 5)
    return compute_rasi(ephemeris, ayanamsa, t, JAIPUR_LAT, JAIPUR_LON)


# ─── the ascendant ───────────────────────────────────────────────────


def test_ascendant_matches_the_sun_at_geometric_sunrise(ephemeris, timescale) -> None:
    """The strongest independent check available for the ascendant.

    At the instant the Sun's centre crosses the geometric horizon it IS
    on the eastern horizon — which is the definition of the ascendant. So
    the two must agree, and they are computed by completely different
    routes: skyfield's rise/set almanac on one side, spherical
    trigonometry on local sidereal time on the other.

    Measured agreement: 0.7 arcseconds.

    The obvious version of this test uses `almanac.sunrise_sunset`, and
    it fails by 50 arcminutes — because conventional sunrise is the upper
    LIMB appearing, which adds the Sun's semi-diameter (~16') and
    atmospheric refraction (~34'). That near-miss is a much better
    confirmation than agreement would have been: the residual is exactly
    the physics the geometric definition excludes.
    """
    place = wgs84.latlon(JAIPUR_LAT, JAIPUR_LON)
    observer = ephemeris.kernel["earth"] + place

    rising = almanac.risings_and_settings(
        ephemeris.kernel,
        ephemeris.kernel["sun"],
        place,
        horizon_degrees=0.0,
        radius_degrees=0.0,
    )
    times, events = almanac.find_discrete(
        timescale.utc(1994, 8, 17), timescale.utc(1994, 8, 18), rising
    )

    checked = 0
    for t, is_rise in zip(times, events, strict=True):
        if not is_rise:
            continue
        _, sun_longitude, _ = (
            observer.at(t).observe(ephemeris.kernel["sun"]).apparent().ecliptic_latlon(epoch=t)
        )
        ascendant = tropical_ascendant(t, JAIPUR_LAT, JAIPUR_LON)

        separation = abs(ascendant - sun_longitude.degrees)
        separation = min(separation, 360 - separation)

        assert separation * 3600 < 30, (
            f"at geometric sunrise the Sun is at {sun_longitude.degrees:.5f}° and the "
            f"ascendant at {ascendant:.5f}° — {separation * 3600:.1f} arcseconds apart"
        )
        checked += 1

    assert checked == 1, "no sunrise found; the test asserted nothing"


def test_ascendant_traverses_every_sign_in_a_day(ephemeris, timescale) -> None:
    """Twelve signs rise in twenty-four hours.

    Catches an ascendant stuck on one horizon, and a quadrant error that
    would visit only half the zodiac. Sampled every ten minutes so a
    fast-rising sign at this latitude cannot be missed.
    """
    seen = set()
    for minutes in range(0, 24 * 60, 10):
        t = timescale.utc(1994, 8, 17, 0, minutes)
        seen.add(sign_index(tropical_ascendant(t, JAIPUR_LAT, JAIPUR_LON)))

    assert seen == set(range(12)), f"only {len(seen)} signs rose: {sorted(seen)}"


def test_ascendant_advances_about_a_degree_every_four_minutes(ephemeris, timescale) -> None:
    """The rate that makes birth-time accuracy matter.

    It is why `time_accuracy` exists, why an unknown time returns no
    ascendant, and why a 30-minute error can change the rising sign.
    Averaged over a full day because the instantaneous rate varies a lot
    with latitude and sign.
    """
    start = tropical_ascendant(timescale.utc(1994, 8, 17, 0, 0), JAIPUR_LAT, JAIPUR_LON)
    total = 0.0
    previous = start

    for minutes in range(1, 24 * 60):
        current = tropical_ascendant(timescale.utc(1994, 8, 17, 0, minutes), JAIPUR_LAT, JAIPUR_LON)
        total += (current - previous) % 360
        previous = current

    assert total == pytest.approx(360.0, abs=1.0), (
        f"the ascendant covered {total:.2f}° in a day; it must complete exactly one circle"
    )


@settings(max_examples=30, deadline=None)
@given(
    latitude=st.floats(min_value=-60, max_value=60),
    longitude=st.floats(min_value=-180, max_value=180),
    hour=st.integers(min_value=0, max_value=23),
)
def test_ascendant_is_always_a_valid_longitude(ephemeris_module, latitude, longitude, hour) -> None:
    """Across the inhabited latitudes and every longitude.

    Excludes the polar regions, where the ecliptic can fail to cross the
    horizon at all and the ascendant is genuinely undefined rather than
    merely hard.
    """
    _, timescale = ephemeris_module
    value = tropical_ascendant(timescale.utc(2000, 6, 21, hour), latitude, longitude)

    assert 0.0 <= value < 360.0
    assert not math.isnan(value)


# ─── houses and placement ────────────────────────────────────────────


def test_houses_are_a_permutation_of_one_to_twelve(jaipur_chart) -> None:
    """The spec's property, on one known chart."""
    assert jaipur_chart.houses is not None
    assert [h.house for h in jaipur_chart.houses] == list(range(1, HOUSE_COUNT + 1))
    assert len({h.sign_index for h in jaipur_chart.houses}) == HOUSE_COUNT


@settings(max_examples=40, deadline=None)
@given(
    year=st.integers(min_value=1900, max_value=2050),
    month=st.integers(min_value=1, max_value=12),
    day=st.integers(min_value=1, max_value=28),
    hour=st.integers(min_value=0, max_value=23),
    minute=st.integers(min_value=0, max_value=59),
    # The full inhabited range, and then some. 66.5 degrees is the Arctic
    # Circle; 78 is Ny-Alesund, which people do live at.
    latitude=st.floats(min_value=-78.0, max_value=78.0),
    longitude=st.floats(min_value=-180.0, max_value=180.0, exclude_max=True),
)
def test_houses_are_a_permutation_at_any_time_and_place(
    ephemeris, ayanamsa, timescale, year, month, day, hour, minute, latitude, longitude
) -> None:
    """The same property, as a property rather than an example.

    The test above checks one chart in Jaipur, which cannot fail for a
    reason that depends on WHERE the chart is. The ascendant is computed
    with an atan2 over the obliquity and the local sidereal time, and its
    degenerate cases are all latitude-driven — near the poles the
    quadrant house systems break down entirely, and whole-sign is
    supposed to keep working. Fixtures 026 (Tromso, polar night) and 005
    (Anchorage) exist for the same reason; this covers the space between
    and beyond them.

    Two assertions, because a permutation is two separate claims: every
    house number appears exactly once, AND the twelve signs are distinct.
    A rotation bug satisfies the first and fails the second.
    """
    # All three fixtures are session-scoped, which is what makes them
    # safe to take alongside @given — the conftest's warning about
    # combining hypothesis with fixtures is about FUNCTION-scoped ones.
    t = timescale.utc(year, month, day, hour, minute)

    rasi = compute_rasi(ephemeris, ayanamsa, t, latitude, longitude)

    assert rasi.houses is not None, "whole-sign houses must exist at every latitude"
    assert [h.house for h in rasi.houses] == list(range(1, HOUSE_COUNT + 1)), (
        f"houses at lat {latitude:.3f} are not 1..12 in order"
    )
    assert len({h.sign_index for h in rasi.houses}) == HOUSE_COUNT, (
        f"houses at lat {latitude:.3f} do not cover twelve distinct signs — "
        "whole-sign houses are a rotation of the zodiac, so a repeat means "
        "the rotation is wrong"
    )

    # And every graha lands in exactly one of them. A house number
    # outside 1..12 is the "house 13" failure in a different disguise.
    for planet in rasi.planets:
        assert 1 <= planet.house <= HOUSE_COUNT, (
            f"{planet.planet} is in house {planet.house} at lat {latitude:.3f}"
        )


def test_every_graha_is_in_exactly_one_house(jaipur_chart) -> None:
    """The spec's property.

    Asserted from both directions: each planet reports one house, and the
    houses between them mention each planet once. A one-directional check
    would pass if a planet were listed in two houses' contents.
    """
    assert jaipur_chart.houses is not None

    for planet in jaipur_chart.planets:
        assert 1 <= planet.house <= HOUSE_COUNT

    listed = [p for house in jaipur_chart.houses for p in house.planets]
    assert sorted(listed) == sorted(p.planet for p in jaipur_chart.planets)
    assert len(listed) == len(set(listed)), "a graha appears in two houses"


def test_the_ascendant_sign_is_house_one(jaipur_chart) -> None:
    """Whole-sign houses, by definition."""
    assert jaipur_chart.ascendant is not None
    assert jaipur_chart.houses is not None
    assert jaipur_chart.houses[0].sign_index == jaipur_chart.ascendant.sign_index


def test_house_lords_follow_the_sign(jaipur_chart) -> None:
    from app.core.constants import SIGN_LORDS

    assert jaipur_chart.houses is not None
    for house in jaipur_chart.houses:
        assert house.lord == SIGN_LORDS[house.sign_index]


# ─── the unknown-birth-time contract ─────────────────────────────────


def test_unknown_birth_time_omits_the_ascendant_and_houses(ephemeris, ayanamsa, timescale) -> None:
    """The determinism principle applied to missing input.

    The Moon moves ~13°/day, so planetary longitudes from an assumed
    midday are still broadly meaningful. The ascendant moves a full
    circle in a day and cannot be estimated at all — so it is absent,
    not guessed. `None` rather than a sentinel, so a caller cannot treat
    a missing ascendant as present.
    """
    t = timescale.utc(1994, 8, 17, 6, 30)
    chart = compute_rasi(ephemeris, ayanamsa, t, JAIPUR_LAT, JAIPUR_LON, time_known=False)

    assert chart.ascendant is None
    assert chart.houses is None

    # Planets are still computed, and every one reports "no house".
    assert len(chart.planets) == 9
    assert all(p.house == 0 for p in chart.planets)
    assert all(0.0 <= p.longitude < 360.0 for p in chart.planets)


# ─── dignity and combustion ──────────────────────────────────────────


def test_exaltation_and_debilitation_are_opposites(ephemeris, ayanamsa, timescale) -> None:
    """Checked directly against the definition rather than a chart.

    Each planet is exalted at a known degree and debilitated exactly
    opposite. Driving it through real charts would only ever exercise
    whichever placements happened to occur.
    """
    from app.core.chart import _dignity
    from app.core.constants import EXALTATION

    for planet, point in EXALTATION.items():
        assert _dignity(planet, point) == "exalted", f"{planet} at {point}"
        assert _dignity(planet, (point + 180) % 360) == "debilitated", f"{planet} opposite"


def test_the_nodes_have_no_dignity(ephemeris, ayanamsa, timescale) -> None:
    """Dignity is ownership of a sign, and a point owns nothing."""
    from app.core.chart import _dignity

    for node in (Planet.RAHU, Planet.KETU):
        for longitude in (0.0, 45.0, 123.4, 300.0):
            assert _dignity(node, longitude) == "neutral"


def test_the_sun_is_never_combust(jaipur_chart) -> None:
    """It cannot be burnt by its own light.

    A lookup miss would return False for the right answer by the wrong
    reason, so the Sun is excluded explicitly.
    """
    sun = next(p for p in jaipur_chart.planets if p.planet is Planet.SUN)
    assert not sun.is_combust


def test_combustion_tracks_distance_from_the_sun(ephemeris, ayanamsa, timescale) -> None:
    from app.core.chart import _is_combust
    from app.core.constants import COMBUSTION_ORB

    for planet, orb in COMBUSTION_ORB.items():
        assert _is_combust(planet, 100.0, 100.0), f"{planet} conjunct the Sun"
        assert _is_combust(planet, 100.0 + orb - 0.1, 100.0), f"{planet} just inside its orb"
        assert not _is_combust(planet, 100.0 + orb + 0.1, 100.0), f"{planet} just outside"
        # Across the 0°/360° seam, where a naive subtraction gives ~359.
        assert _is_combust(planet, 0.5, 359.5), f"{planet} across the seam"


# ─── navamsa ─────────────────────────────────────────────────────────


def test_navamsa_follows_the_element_rule() -> None:
    """The rule that was implemented wrongly first.

    Fire counts from Aries, earth from Capricorn, air from Libra, water
    from Cancer. The original implementation used a per-element offset of
    `(sign_index % 4) * 3`, which swapped earth and water — Taurus landed
    in Cancer instead of Capricorn — while every other test still passed,
    because nothing else in the suite knew what the answer should be.

    Tabulated explicitly for that reason. A property test would not have
    caught it; only knowing the expected answer does.
    """
    expected_start = {
        0: "Aries",
        4: "Aries",
        8: "Aries",  # fire
        1: "Capricorn",
        5: "Capricorn",
        9: "Capricorn",  # earth
        2: "Libra",
        6: "Libra",
        10: "Libra",  # air
        3: "Cancer",
        7: "Cancer",
        11: "Cancer",  # water
    }

    for index, start in expected_start.items():
        navamsa = navamsa_longitude(index * 30.0)
        assert SIGNS[sign_index(navamsa)] == start, (
            f"the first navamsa of {SIGNS[index]} is {SIGNS[sign_index(navamsa)]}, expected {start}"
        )


def test_each_sign_maps_to_nine_consecutive_navamsas() -> None:
    """108 navamsas, 12 signs, exactly 9 whole cycles.

    Walking one sign in 3°20' steps must visit nine consecutive signs
    without repeating — which is what makes the continuous-count
    formulation correct.
    """
    for index in range(12):
        visited = [
            sign_index(navamsa_longitude(index * 30.0 + step * (30 / 9) + 0.01))
            for step in range(9)
        ]
        assert len(set(visited)) == 9, f"{SIGNS[index]} repeats a navamsa: {visited}"
        for previous, current in pairwise(visited):
            assert current == (previous + 1) % 12


@settings(max_examples=300, deadline=None)
@given(longitude=st.floats(min_value=0, max_value=360, exclude_max=True))
def test_navamsa_always_lands_in_range(longitude: float) -> None:
    result = navamsa_longitude(longitude)
    assert 0.0 <= result < 360.0


def test_navamsa_chart_has_the_same_grahas(jaipur_chart) -> None:
    """D9 is a transformation of D1, not a second computation.

    Recomputing from the ephemeris would create a second path to the same
    answer, and therefore a way for the two charts to disagree about
    which planets exist.
    """
    navamsa = compute_navamsa(jaipur_chart)

    assert {p.planet for p in navamsa.planets} == {p.planet for p in jaipur_chart.planets}
    assert navamsa.ascendant is not None
    assert navamsa.houses is not None
    assert [h.house for h in navamsa.houses] == list(range(1, HOUSE_COUNT + 1))


def test_navamsa_of_an_unknown_time_chart_stays_unknown(ephemeris, ayanamsa, timescale) -> None:
    """Absence propagates.

    A D9 ascendant derived from a D1 that has none would be invented out
    of nothing — the exact fabrication the unknown-time contract exists
    to prevent.
    """
    t = timescale.utc(1994, 8, 17, 6, 30)
    rasi = compute_rasi(ephemeris, ayanamsa, t, JAIPUR_LAT, JAIPUR_LON, time_known=False)
    navamsa = compute_navamsa(rasi)

    assert navamsa.ascendant is None
    assert navamsa.houses is None
    assert all(p.house == 0 for p in navamsa.planets)


def test_retrograde_state_survives_into_the_navamsa(jaipur_chart) -> None:
    """Retrogradation is a fact about the body, not about a divisional map."""
    navamsa = compute_navamsa(jaipur_chart)
    rasi_state = {p.planet: p.is_retrograde for p in jaipur_chart.planets}

    for planet in navamsa.planets:
        assert planet.is_retrograde == rasi_state[planet.planet]


def test_every_exact_boundary_lands_in_the_right_subdivision() -> None:
    """The float bug that was actually shipped in the first draft.

    `longitude // (360 / divisions)` is wrong wherever 360/divisions is
    not representable in binary. For the 27 nakshatras the arc is
    13.333…°, and at an exact boundary the division lands a hair under
    the integer.

    Measured on the first version: NINE of twenty-seven nakshatra
    boundaries returned the previous nakshatra, and the same nine padas
    returned 4 instead of 1. Signs were unaffected — 30 is exact in
    binary — which is precisely why a spot check on signs would have
    missed it.

    It mattered. A planet exactly on a nakshatra cusp would get the wrong
    nakshatra, hence the wrong pada, hence the wrong Vimshottari dasha
    lord, hence a completely different life-period tree.
    """
    from app.core.constants import NAKSHATRA_COUNT, PADA_COUNT, SIGN_COUNT, nakshatra_index, pada

    for n in range(SIGN_COUNT):
        assert sign_index(n * (360 / SIGN_COUNT)) == n, f"sign boundary {n}"

    for n in range(NAKSHATRA_COUNT):
        longitude = n * (360 / NAKSHATRA_COUNT)
        assert nakshatra_index(longitude) == n, f"nakshatra boundary {n}"
        assert pada(longitude) == 1, f"pada at nakshatra boundary {n}"

    for n in range(PADA_COUNT):
        longitude = n * (360 / PADA_COUNT)
        assert pada(longitude) == n % 4 + 1, f"pada boundary {n}"
        assert sign_index(navamsa_longitude(longitude)) == n % 12, f"navamsa boundary {n}"
