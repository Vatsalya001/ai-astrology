"""Cross-validation against published astronomical events.

The testing rules say a golden file must be cross-validated against an
independent reference before it is frozen, because *"a golden file that
encodes your own bug makes that bug permanent — strictly worse than
having no test, because it converts a bug into an assertion."*

This file is that reference, and it runs before any golden file is
generated.

**What "independent" means here, precisely.** These are not comparisons
against a second astrology package. They are comparisons against dated
astronomical events whose times are published, observed and agreed on by
sources that know nothing about this codebase — equinoxes, an eclipse, a
conjunction people photographed, a retrograde season. Nothing in this
file was produced by the engine it tests.

That is a deliberate choice rather than a shortcut, and the trade is
worth stating. A second *implementation* would check more of the surface
at once. The obvious candidate is Swiss Ephemeris, and ADR-003 is still
open on its licence — pulling it in even as a test-only dependency would
pre-empt that decision. Published events check less surface but are
beyond dispute, and they catch every class of error that actually
threatens this engine:

  * wrong reference frame       → equinox off by days
  * ecliptic frozen at J2000    → equinox drifts with the epoch
  * Julian-day arithmetic slip  → everything off by a whole day
  * ayanamsa wrong or missing   → the sidereal ingress tests fail loudly
  * light-time or aberration
    handled wrongly             → the conjunction separation is wrong
  * sign of the daily motion    → the retrograde tests fail

Tolerances are stated per test and are generous relative to the
published precision, so a passing run means the engine agrees with
reality, not that the tolerance was tuned until it did.
"""

from __future__ import annotations

from datetime import UTC, datetime

import pytest
from skyfield.timelib import Time, Timescale

from app.core.ayanamsa import AyanamsaCalculator, AyanamsaSystem
from app.core.constants import Planet, normalise_longitude
from app.core.ephemeris import SkyfieldEphemeris

# Everything here is UTC. The events are published in UTC and the engine
# works in UTC; a local time anywhere in this file would be a bug.


def _time(ts: Timescale, moment: datetime) -> Time:
    return ts.from_datetime(moment.replace(tzinfo=UTC))


def _separation(first: float, second: float) -> float:
    """Shortest angular distance between two ecliptic longitudes."""
    delta = abs(normalise_longitude(first) - normalise_longitude(second))
    return min(delta, 360.0 - delta)


def _crossing(
    ephemeris: SkyfieldEphemeris,
    ts: Timescale,
    planet: Planet,
    target_longitude: float,
    start: datetime,
    end: datetime,
    *,
    sidereal: AyanamsaCalculator | None = None,
) -> datetime:
    """When a planet's longitude crosses a value, by bisection.

    Returns the instant rather than a boolean, so a failure says "three
    hours late" instead of "False" — and three hours is a very different
    diagnosis from three days.
    """

    def offset(moment: datetime) -> float:
        t = _time(ts, moment)
        longitude = ephemeris.longitude_of(planet, t)
        if sidereal is not None:
            longitude = normalise_longitude(longitude - sidereal.at_time(t, AyanamsaSystem.LAHIRI))
        # Centre the difference on the target so the sign flip happens
        # exactly at the crossing rather than at 0/360.
        return ((longitude - target_longitude + 180.0) % 360.0) - 180.0

    low, high = start, end
    if offset(low) > 0 or offset(high) < 0:
        pytest.fail(
            f"{planet} does not cross {target_longitude}° between {low} and {high}: "
            f"offsets are {offset(low):.3f}° and {offset(high):.3f}°"
        )

    # 60 halvings of any sane window lands far below a second.
    for _ in range(60):
        middle = low + (high - low) / 2
        if offset(middle) < 0:
            low = middle
        else:
            high = middle
        if (high - low).total_seconds() < 1:
            break

    return low + (high - low) / 2


# ─── 1. The equinoxes ────────────────────────────────────────────────
#
# The March equinox is the instant the Sun's TROPICAL longitude is 0°.
# Its time is published to the minute and is the single most direct check
# on the reference frame: a J2000-frozen ecliptic drifts the answer by a
# day per 70 years, and a Julian-day slip moves it by a whole day.


@pytest.mark.parametrize(
    ("year", "published"),
    [
        # Published March equinox times, UTC.
        (2020, datetime(2020, 3, 20, 3, 50, tzinfo=UTC)),
        (2024, datetime(2024, 3, 20, 3, 7, tzinfo=UTC)),
    ],
)
def test_march_equinox_matches_the_published_time(
    ephemeris: SkyfieldEphemeris, timescale: Timescale, year: int, published: datetime
) -> None:
    computed = _crossing(
        ephemeris,
        timescale,
        Planet.SUN,
        0.0,
        datetime(year, 3, 18, tzinfo=UTC),
        datetime(year, 3, 22, tzinfo=UTC),
    )

    drift_minutes = abs((computed - published).total_seconds()) / 60
    assert drift_minutes < 20, (
        f"the {year} March equinox computes to {computed:%Y-%m-%d %H:%M} UTC, "
        f"published {published:%Y-%m-%d %H:%M} UTC — {drift_minutes:.0f} minutes out. "
        "The Sun moves 0.04°/hour, so this is the tightest constraint available on "
        "the ecliptic frame and the Julian-day arithmetic together."
    )


# ─── 2. The great conjunction of 2020 ────────────────────────────────
#
# On 21 December 2020 Jupiter and Saturn passed within about a tenth of a
# degree of each other — close enough that people photographed both in one
# telescope field. It is the best-documented planetary event of the
# decade, and it constrains TWO outer-planet positions simultaneously.


def test_the_great_conjunction_of_2020(ephemeris: SkyfieldEphemeris, timescale: Timescale) -> None:
    t = _time(timescale, datetime(2020, 12, 21, 18, 20))
    separation = _separation(
        ephemeris.longitude_of(Planet.JUPITER, t),
        ephemeris.longitude_of(Planet.SATURN, t),
    )

    assert separation < 0.25, (
        f"Jupiter and Saturn are {separation:.3f}° apart at the great conjunction of "
        "21 December 2020; the published separation was about 0.1°. Two outer planets "
        "cannot both be wrong by the same amount in the same direction by accident."
    )

    # And they were NOT that close three weeks earlier. Without this, a
    # bug that collapsed every planet onto one longitude would pass.
    a_month_before = _time(timescale, datetime(2020, 12, 1, 0, 0))
    earlier = _separation(
        ephemeris.longitude_of(Planet.JUPITER, a_month_before),
        ephemeris.longitude_of(Planet.SATURN, a_month_before),
    )
    assert earlier > 1.0, (
        f"Jupiter and Saturn are only {earlier:.3f}° apart on 1 December 2020, three "
        "weeks before the conjunction — the planets are not moving relative to each "
        "other, so the conjunction test above proves nothing"
    )


# ─── 3. The total solar eclipse of 2017 ──────────────────────────────
#
# A solar eclipse is a new moon that lines up in latitude as well, so at
# maximum eclipse the Sun and Moon share an ecliptic longitude almost
# exactly. 21 August 2017 crossed the United States and is documented to
# the second. It constrains the Moon, which moves 13°/day and is
# therefore the most sensitive body in the whole chart to a timing error.


def test_the_total_solar_eclipse_of_2017(
    ephemeris: SkyfieldEphemeris, timescale: Timescale
) -> None:
    t = _time(timescale, datetime(2017, 8, 21, 18, 26))
    separation = _separation(
        ephemeris.longitude_of(Planet.SUN, t),
        ephemeris.longitude_of(Planet.MOON, t),
    )

    # The Moon covers 0.55°/hour, so 0.6° is about an hour of error —
    # comfortably inside the published maximum yet far tighter than any
    # bug that matters would survive.
    assert separation < 0.6, (
        f"the Sun and Moon are {separation:.3f}° apart at the maximum of the "
        "21 August 2017 total eclipse, where a conjunction is the definition of the "
        "event. The Moon moves 13°/day, so this is the sharpest timing check here."
    )


# ─── 4. The ayanamsa, end to end ─────────────────────────────────────
#
# The previous three tests all use TROPICAL longitudes, so an ayanamsa
# that was zero, doubled or frozen would pass every one of them. These
# two use the sidereal frame the product actually serves.
#
# The Sun enters tropical Aries at the March equinox, around 20 March, and
# sidereal Aries (Mesha Sankranti, the Tamil and Bengali new year) around
# 14 April. The roughly 24-day gap IS the ayanamsa, observed rather than
# computed, and it is a date hundreds of millions of people mark annually.


def test_the_sidereal_aries_ingress_lands_on_mesha_sankranti(
    ephemeris: SkyfieldEphemeris, timescale: Timescale, ayanamsa: AyanamsaCalculator
) -> None:
    ingress = _crossing(
        ephemeris,
        timescale,
        Planet.SUN,
        0.0,
        datetime(2020, 4, 5, tzinfo=UTC),
        datetime(2020, 4, 25, tzinfo=UTC),
        sidereal=ayanamsa,
    )

    assert ingress.month == 4 and 13 <= ingress.day <= 15, (
        f"the Sun enters sidereal Aries on {ingress:%Y-%m-%d %H:%M} UTC; "
        "Mesha Sankranti falls on 13-15 April. A tropical longitude would put this "
        "in March and a doubled ayanamsa in May, so this is the test that notices "
        "the sidereal frame is missing entirely."
    )


def test_the_tropical_and_sidereal_ingresses_differ_by_the_ayanamsa(
    ephemeris: SkyfieldEphemeris, timescale: Timescale, ayanamsa: AyanamsaCalculator
) -> None:
    tropical = _crossing(
        ephemeris,
        timescale,
        Planet.SUN,
        0.0,
        datetime(2020, 3, 18, tzinfo=UTC),
        datetime(2020, 3, 22, tzinfo=UTC),
    )
    sidereal = _crossing(
        ephemeris,
        timescale,
        Planet.SUN,
        0.0,
        datetime(2020, 4, 5, tzinfo=UTC),
        datetime(2020, 4, 25, tzinfo=UTC),
        sidereal=ayanamsa,
    )

    gap_days = (sidereal - tropical).total_seconds() / 86400
    # The Sun covers just under a degree a day near the equinox, and the
    # 2020 ayanamsa is a little over 24°.
    assert 23.5 < gap_days < 25.5, (
        f"the sidereal Aries ingress is {gap_days:.2f} days after the tropical one; "
        "the ayanamsa in 2020 is just over 24° and the Sun moves ~0.99°/day, so the "
        "gap has to be about 24.5 days. This measures the ayanamsa through the "
        "ephemeris rather than reading the constant back."
    )


# ─── 5. Saturn's sidereal sign, on dates Vedic sources publish ───────
#
# Saturn's sign changes are announced years ahead and are among the most
# widely reported events in Indian astrology, which makes them a
# reference the engine cannot have influenced. Sade Sati is computed from
# exactly this, so getting it wrong is not academic.


@pytest.mark.parametrize(
    ("moment", "expected_sign"),
    [
        # Saturn entered sidereal Sagittarius in early 2017, Capricorn in
        # January 2020, and Aquarius on 17 January 2023.
        (datetime(2019, 6, 1, tzinfo=UTC), "Sagittarius"),
        (datetime(2020, 6, 1, tzinfo=UTC), "Capricorn"),
        (datetime(2023, 6, 1, tzinfo=UTC), "Aquarius"),
    ],
)
def test_saturns_sidereal_sign_matches_published_transits(
    ephemeris: SkyfieldEphemeris,
    timescale: Timescale,
    ayanamsa: AyanamsaCalculator,
    moment: datetime,
    expected_sign: str,
) -> None:
    from app.core.constants import SIGNS, sign_index

    t = _time(timescale, moment)
    sidereal_longitude = normalise_longitude(
        ephemeris.longitude_of(Planet.SATURN, t) - ayanamsa.at_time(t, AyanamsaSystem.LAHIRI)
    )
    actual = SIGNS[sign_index(sidereal_longitude)]

    assert actual == expected_sign, (
        f"Saturn is in sidereal {actual} on {moment:%Y-%m-%d}, but published Vedic "
        f"transit tables put it in {expected_sign}. Sade Sati is read directly off "
        "this, so an error here is an error in the product's most-asked-about answer."
    )


# ─── 6. Retrograde motion ────────────────────────────────────────────
#
# Mars was retrograde from 9 September to 14 November 2020 — a season
# reported everywhere. It checks the SIGN of the daily motion, which no
# position test touches: an engine that returned every planet's speed
# with the sign flipped would pass everything above.


@pytest.mark.parametrize(
    ("moment", "expected_retrograde"),
    [
        (datetime(2020, 8, 1, tzinfo=UTC), False),  # before the station
        (datetime(2020, 10, 1, tzinfo=UTC), True),  # mid-retrograde
        (datetime(2020, 12, 1, tzinfo=UTC), False),  # after direct
    ],
)
def test_the_mars_retrograde_of_2020(
    ephemeris: SkyfieldEphemeris,
    timescale: Timescale,
    moment: datetime,
    expected_retrograde: bool,
) -> None:
    position = ephemeris.position_of(Planet.MARS, _time(timescale, moment))

    assert position.is_retrograde is expected_retrograde, (
        f"Mars reads {'retrograde' if position.is_retrograde else 'direct'} on "
        f"{moment:%Y-%m-%d} at {position.speed:+.4f}°/day; the published 2020 "
        "retrograde ran 9 September to 14 November. A globally flipped speed sign "
        "passes every position test in this file."
    )


# ─── 7. The sidereal month ───────────────────────────────────────────
#
# The Moon returns to the same sidereal longitude every 27.321661 days.
# That constant is published, not derived here, and it checks the Moon's
# motion over a long baseline rather than at one instant — a systematic
# rate error that a single-instant test absorbs shows up over a year.


def test_the_moon_completes_a_sidereal_month_in_the_published_time(
    ephemeris: SkyfieldEphemeris, timescale: Timescale, ayanamsa: AyanamsaCalculator
) -> None:
    published_days = 27.321661
    start = datetime(2020, 1, 1, tzinfo=UTC)

    def sidereal_longitude(moment: datetime) -> float:
        t = _time(timescale, moment)
        return normalise_longitude(
            ephemeris.longitude_of(Planet.MOON, t) - ayanamsa.at_time(t, AyanamsaSystem.LAHIRI)
        )

    target = sidereal_longitude(start)
    ret = _crossing(
        ephemeris,
        timescale,
        Planet.MOON,
        normalise_longitude(
            target + ayanamsa.at_time(_time(timescale, start), AyanamsaSystem.LAHIRI)
        ),
        start + (datetime(2020, 1, 27, tzinfo=UTC) - datetime(2020, 1, 1, tzinfo=UTC)),
        datetime(2020, 1, 29, tzinfo=UTC),
    )

    elapsed = (ret - start).total_seconds() / 86400
    # The Moon's orbit is perturbed enough that any single month differs
    # from the mean by several hours; a quarter of a day is generous for
    # one revolution and still catches a rate error of 1%.
    assert abs(elapsed - published_days) < 0.25, (
        f"the Moon takes {elapsed:.4f} days to return to its longitude from "
        f"{start:%Y-%m-%d}; the published sidereal month is {published_days} days. "
        "A systematic rate error hides at a single instant and shows up here."
    )
