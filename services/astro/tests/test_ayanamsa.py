"""The ayanamsa, which is the highest-risk number in the service.

An error here is invisible by construction: it shifts every planet by the
same amount, so the chart stays internally consistent and simply
describes the wrong sky. Nothing downstream can catch it — the houses
still form a permutation, the nodes are still opposite, the dashas still
sum to 120 years. Only a comparison against an outside source can.

ADR-003 chose skyfield (MIT) over pyswisseph and accepted, in writing,
that we would have to earn agreement with the reference implementations
rather than inherit it. This file is the first instalment.
"""

from __future__ import annotations

from itertools import pairwise

import pytest
from hypothesis import given, settings
from hypothesis import strategies as st

from app.core.ayanamsa import AyanamsaSystem

#: Swiss Ephemeris anchors its Lahiri implementation at 1900 Jan 0.5 TT
#: with an ayanamsa of 22.460148°. That constant is published, comes from
#: an implementation we deliberately did NOT use, and was fixed long
#: before this code existed — which is what makes it a genuine
#: independent reference rather than a restatement of our own output.
SWISSEPH_1900_ANCHOR = 22.460148

#: 20 arcseconds, in degrees.
#:
#: Chosen against what the number is actually used for, not to make a
#: test pass. The finest boundary any interpretation turns on is a pada
#: at 3°20'; a nakshatra is 13°20'. Twenty arcseconds is 0.0056° — three
#: orders of magnitude below the smallest thing that could change a
#: reading. Measured agreement is currently about 5 arcseconds, so this
#: tolerance also leaves room to detect a real regression rather than
#: sitting flush against the observed value.
ARCSECOND_TOLERANCE = 20 / 3600


def test_matches_the_definitional_anchor_exactly(ayanamsa, timescale) -> None:
    """23°15'00" on 21 March 1956.

    Not a fitted constant — the Indian Calendar Reform Committee's
    definition. Every other Lahiri value is this anchor carried by
    precession, so if this is wrong everything is wrong by the same
    amount and nothing else in the suite would notice.
    """
    t = timescale.utc(1956, 3, 21)
    assert ayanamsa.at_time(t) == pytest.approx(23.25, abs=1e-9)


def test_agrees_with_an_independent_implementation_at_1900(ayanamsa, timescale) -> None:
    """Cross-validation against Swiss Ephemeris's published anchor.

    This catches a wrong precession model or a J2000-versus-of-date
    frame mistake — both produce plausible-looking numbers that only
    diverge against an outside source. Measured: the frame mistake shows
    up here as a 2820-arcsecond divergence.

    It does NOT catch proper motion leaking into the measurement; at this
    epoch the two variants are about 1.5 arcseconds apart. That decision
    is pinned structurally instead, by
    test_reference_direction_has_no_proper_motion.
    """
    t = timescale.tt(1900, 1, 0, 12)  # 1900 Jan 0.5 TT
    computed = ayanamsa.at_time(t)

    assert computed == pytest.approx(SWISSEPH_1900_ANCHOR, abs=ARCSECOND_TOLERANCE), (
        f"Lahiri at the swisseph epoch is {computed:.6f}°, expected "
        f"{SWISSEPH_1900_ANCHOR}° — a divergence of "
        f"{abs(computed - SWISSEPH_1900_ANCHOR) * 3600:.1f} arcseconds"
    )


def test_precesses_at_the_observed_rate(ayanamsa, timescale) -> None:
    """Roughly 50.3 arcseconds a year.

    The general precession rate is a property of the Earth's axis, not of
    any ayanamsa convention, so this catches an implementation that is
    internally consistent but not actually tracking precession — which is
    exactly what a J2000-frame mistake produces: a number that barely
    moves across a century.
    """
    span_years = 100
    start = ayanamsa.at_time(timescale.utc(1950, 1, 1))
    end = ayanamsa.at_time(timescale.utc(1950 + span_years, 1, 1))

    arcsec_per_year = (end - start) * 3600 / span_years
    assert 50.0 <= arcsec_per_year <= 50.6, (
        f"precession measured at {arcsec_per_year:.2f} arcsec/year; "
        "the accepted general precession is about 50.3"
    )


def test_increases_monotonically(ayanamsa, timescale) -> None:
    """Precession does not reverse.

    A decrease over any interval means the sign of the correction is
    wrong somewhere, which would put every planet a full ayanamsa on the
    wrong side — around 48° of error by 2050.
    """
    values = [ayanamsa.at_time(timescale.utc(year, 1, 1)) for year in range(1900, 2051, 10)]
    assert values == sorted(values)
    assert all(b > a for a, b in pairwise(values))


def test_stays_within_plausible_bounds(ayanamsa, timescale) -> None:
    """Between 22° and 25° across the kernel's whole range.

    A coarse net, deliberately. It catches the catastrophic failures —
    a sign error, radians mistaken for degrees, an unfolded angle — that
    the finer tests might miss if they all shared one wrong assumption.
    """
    for year in (1900, 1950, 2000, 2050):
        value = ayanamsa.at_time(timescale.utc(year, 1, 1))
        assert 22.0 < value < 25.0, f"ayanamsa at {year} is {value:.4f}°"


def test_the_named_systems_differ(ayanamsa, timescale) -> None:
    """Raman and KP are not silently aliases of Lahiri.

    The ayanamsa is part of a chart's database identity and its cache
    key. If two systems returned the same number the distinction would be
    meaningless, and a user who chose Raman would be served Lahiri while
    the UI told them otherwise.
    """
    t = timescale.utc(2000, 1, 1)
    values = {system: ayanamsa.at_time(t, system) for system in AyanamsaSystem}

    assert len(set(values.values())) == len(AyanamsaSystem), f"systems collide: {values}"
    assert values[AyanamsaSystem.RAMAN] < values[AyanamsaSystem.LAHIRI]


def test_sidereal_conversion_round_trips(ayanamsa, timescale) -> None:
    t = timescale.utc(2000, 1, 1)
    offset = ayanamsa.at_time(t)

    for tropical in (0.0, 15.5, 179.9, 180.0, 359.99):
        sidereal = ayanamsa.to_sidereal(tropical, t)
        assert (sidereal + offset) % 360 == pytest.approx(tropical % 360, abs=1e-9)


@settings(max_examples=200, deadline=None)
@given(tropical=st.floats(min_value=0, max_value=360, exclude_max=True))
def test_sidereal_conversion_always_lands_in_range(ayanamsa_module, tropical: float) -> None:
    """The spec's property: to_sidereal never escapes [0, 360).

    Subtracting the ayanamsa from a small longitude goes negative, and an
    unfolded negative longitude becomes a negative sign index, which
    becomes house 0 or house -1. Property-based rather than
    example-based because the failure lives entirely in the wrap-around
    region, which is exactly where hand-picked examples are thinnest.
    """
    ayanamsa, timescale = ayanamsa_module
    result = ayanamsa.to_sidereal(tropical, timescale.utc(2000, 1, 1))
    assert 0.0 <= result < 360.0


def test_a_naive_datetime_is_refused(ayanamsa) -> None:
    """This service never guesses a timezone.

    A naive datetime is ambiguous, and the ascendant moves a degree every
    four minutes — silently assuming UTC for an Indian birth time would
    shift the chart by five and a half hours.
    """
    from datetime import datetime

    with pytest.raises(ValueError, match="aware datetime"):
        ayanamsa.at(datetime(2000, 1, 1, 12, 0, 0))


def test_reference_direction_has_no_proper_motion(ayanamsa) -> None:
    """The precession carrier is a direction, not a star.

    This is a structural assertion rather than a numerical one, and
    deliberately so. The numerical cross-validation at 1900 CANNOT tell
    the two variants apart — measured, they differ by about 1.5
    arcseconds there, and the proper-motion version is marginally closer.

    The divergence only becomes visible far from the anchor: roughly 25
    arcseconds by J2000, growing without bound thereafter, because
    Spica's own motion across the sky has nothing to do with the
    precession of the Earth's axis.

    So the guard has to be on the construction, not the output. Without
    it, somebody reasonably "improves" the reference by adding the
    catalogue's proper motion back, every test still passes, and charts
    drift further off the further the birth date is from 1956.
    """
    # Reaching into the internals is the point: the guard is on how the
    # reference is CONSTRUCTED, which no public output exposes.
    star = ayanamsa._reference

    for attribute in ("ra_mas_per_year", "dec_mas_per_year"):
        value = getattr(star, attribute, 0.0)
        assert not value, (
            f"the precession reference has {attribute}={value}; it must be a "
            "fixed direction. See the module docstring — proper motion "
            "contaminates the measurement and the error grows with distance "
            "from the 1956 anchor."
        )
