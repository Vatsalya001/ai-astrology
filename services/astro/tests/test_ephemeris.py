"""Planetary positions from the JPL kernel.

The properties here are the ones the spec's §9 names, plus the ones that
catch the specific way this code can go wrong: a frame mistake, an
unfolded angle, or a node computed twice instead of once.
"""

from __future__ import annotations

from pathlib import Path

import pytest
from hypothesis import given, settings
from hypothesis import strategies as st

from app.core.constants import BODIES, NODES, Planet
from app.core.ephemeris import SkyfieldEphemeris


def test_all_nine_grahas_are_present(ephemeris, timescale) -> None:
    positions = ephemeris.positions(timescale.utc(1994, 8, 17, 9, 5))

    assert set(positions) == set(Planet), "a graha is missing from the output"
    assert len(positions) == 9


@settings(max_examples=25, deadline=None)
@given(
    year=st.integers(min_value=1900, max_value=2050),
    month=st.integers(min_value=1, max_value=12),
    day=st.integers(min_value=1, max_value=28),
    hour=st.integers(min_value=0, max_value=23),
)
def test_every_longitude_stays_in_range(ephemeris_module, year, month, day, hour) -> None:
    """The spec's property, across the kernel's whole span.

    A longitude outside [0, 360) becomes a negative or out-of-bounds sign
    index, which becomes house 0 or house 13. Property-based because the
    failure would live at a wrap-around that hand-picked dates miss.
    """
    ephemeris, timescale = ephemeris_module
    positions = ephemeris.positions(timescale.utc(year, month, day, hour))

    for planet, position in positions.items():
        assert 0.0 <= position.longitude < 360.0, f"{planet} at {position.longitude}"


@settings(max_examples=25, deadline=None)
@given(
    year=st.integers(min_value=1900, max_value=2050),
    month=st.integers(min_value=1, max_value=12),
    day=st.integers(min_value=1, max_value=28),
)
def test_ketu_is_exactly_opposite_rahu(ephemeris_module, year, month, day) -> None:
    """The spec's property, and a structural one.

    Ketu is *defined* as the point opposite Rahu, so it is derived rather
    than observed. If the two were computed independently they could
    drift apart and produce a chart where the nodes are not 180° apart,
    which is impossible — and which every downstream calculation would
    then quietly build on.
    """
    ephemeris, timescale = ephemeris_module
    positions = ephemeris.positions(timescale.utc(year, month, day))

    separation = (positions[Planet.KETU].longitude - positions[Planet.RAHU].longitude) % 360
    assert separation == pytest.approx(180.0, abs=1e-9)


def test_the_nodes_are_always_retrograde(ephemeris, timescale) -> None:
    """The lunar nodes regress. Always.

    This is a property of the Moon's orbit, not a coincidence of a
    particular date, and a node showing direct motion means the sign of
    the polynomial's derivative is wrong.
    """
    for year in (1905, 1943, 1994, 2026, 2050):
        positions = ephemeris.positions(timescale.utc(year, 6, 15))
        for node in NODES:
            assert positions[node].is_retrograde, f"{node} direct in {year}"
            assert positions[node].speed < 0


def test_the_sun_and_moon_never_retrograde(ephemeris, timescale) -> None:
    """The complement of the node test.

    From Earth the Sun and Moon only ever move forward. If either shows
    retrograde motion the daily-motion derivation is broken — most likely
    an unwrapped 0°/360° crossing, which the Moon makes monthly.
    """
    for month in range(1, 13):
        positions = ephemeris.positions(timescale.utc(2024, month, 15))
        assert not positions[Planet.SUN].is_retrograde
        assert not positions[Planet.MOON].is_retrograde


def test_daily_motion_is_physically_plausible(ephemeris, timescale) -> None:
    """Speeds within known bounds.

    The tight one is the Moon at roughly 11-15°/day. A wrapping bug in
    the central difference would show up here as a wild value — the Moon
    crosses 0° every month, and an unwrapped crossing yields about
    -348°/day.
    """
    bounds = {
        Planet.SUN: (0.9, 1.1),
        Planet.MOON: (11.0, 15.5),
        Planet.MERCURY: (-2.0, 2.3),
        Planet.VENUS: (-0.7, 1.3),
        Planet.MARS: (-0.5, 0.9),
        Planet.JUPITER: (-0.15, 0.26),
        Planet.SATURN: (-0.09, 0.14),
    }

    for month in range(1, 13):
        positions = ephemeris.positions(timescale.utc(2024, month, 10))
        for planet, (low, high) in bounds.items():
            speed = positions[planet].speed
            assert low <= speed <= high, f"{planet} at {speed:.4f}°/day in month {month}"


def test_saturn_is_retrograde_when_it_should_be(ephemeris, timescale) -> None:
    """A known retrograde period, as an independent check.

    Saturn was retrograde through the middle of 1994 and direct at the
    start of the year. Asserting both directions means the test cannot
    pass by reporting everything retrograde.
    """
    positions_direct = ephemeris.positions(timescale.utc(1994, 1, 15))
    positions_retro = ephemeris.positions(timescale.utc(1994, 8, 17))

    assert not positions_direct[Planet.SATURN].is_retrograde
    assert positions_retro[Planet.SATURN].is_retrograde


def test_the_sun_enters_sidereal_leo_in_mid_august(ephemeris, ayanamsa, timescale) -> None:
    """A check a practitioner would recognise.

    Sidereal Leo begins around 17 August each year — one of the few
    dates in this system that is common knowledge and independently
    verifiable, which is what makes it worth asserting.

    This exercises the ayanamsa and the ephemeris together, so it would
    catch either being right alone while the pair disagree.
    """
    t = timescale.utc(1994, 8, 17, 9, 5)
    sun = ephemeris.positions(t)[Planet.SUN]
    sidereal = ayanamsa.to_sidereal(sun.longitude, t)

    assert 120.0 <= sidereal < 121.0, (
        f"the Sun is at {sidereal:.4f}° sidereal on 17 August 1994; "
        "sidereal Leo starts at 120°, so it should sit just inside it"
    )


def test_positions_are_reproducible(ephemeris, timescale) -> None:
    """The same instant gives the same answer.

    Determinism is the whole point of this service: a reading given three
    years ago has to remain explicable. A cache, a mutable default or an
    ambient clock would break this.
    """
    t = timescale.utc(1994, 8, 17, 9, 5)
    first = ephemeris.positions(t)
    second = ephemeris.positions(t)

    for planet in Planet:
        assert first[planet].longitude == second[planet].longitude
        assert first[planet].speed == second[planet].speed


def test_a_missing_kernel_fails_loudly(timescale, tmp_path: Path) -> None:
    """Never a silent download, never a silent skip.

    skyfield will happily fetch its own kernels, and must not: this
    service has no network, and the error has to name the fix rather
    than leaving someone to discover it from a stack trace.
    """
    with pytest.raises(FileNotFoundError, match="task astro:ephemeris"):
        SkyfieldEphemeris(tmp_path / "absent.bsp", timescale)


def test_bodies_and_nodes_partition_the_grahas() -> None:
    """The two groups are disjoint and complete.

    Several calculations branch on "is this a node", and an overlap would
    mean a planet handled twice or not at all.
    """
    assert set(BODIES) | set(NODES) == set(Planet)
    assert not set(BODIES) & set(NODES)


def test_the_kernel_is_the_one_we_vendored() -> None:
    """A checksum, because a corrupted kernel fails quietly.

    A flipped byte in a Chebyshev coefficient does not raise. It shifts a
    planet, and the chart still renders, still validates, still has
    twelve houses and nine grahas. Nothing downstream can tell.

    This is not hypothetical on this hardware: the project has recorded
    nine data-corruption events — a Go linker panic with an absurd index,
    a Turbopack cache checksum mismatch, others in PROJECT_STATUS.md.
    `memtest86+` is still unrun. A 17 MB binary read on every chart
    computation is exactly the kind of file that would be hit.
    """
    import hashlib

    kernel = Path(__file__).resolve().parent.parent / "data" / "de421.bsp"
    recorded = (kernel.parent / "de421.bsp.sha256").read_text().split()[0]

    digest = hashlib.sha256()
    with kernel.open("rb") as handle:
        for block in iter(lambda: handle.read(1 << 20), b""):
            digest.update(block)

    assert digest.hexdigest() == recorded, (
        "the vendored ephemeris kernel does not match its recorded checksum. "
        "It is corrupt, or it was replaced without updating the checksum. "
        "Charts computed against it cannot be trusted. Re-fetch with "
        "`task astro:ephemeris`."
    )
