"""The golden chart fixtures.

Thirty synthetic profiles, each with a frozen D1, a frozen navamsa and a
frozen dasha tree. Recomputing must reproduce them exactly.

**What this test is and is not.** It is a regression net: it catches any
change to chart assembly — houses, dignities, combustion, nakshatra
padas, dasha subdivision — that nobody decided to make. It is NOT
evidence that the numbers are right; the fixtures came from this engine,
so they can only ever agree with it.

Correctness lives in `test_external_reference.py`, which compares the
engine against dated astronomical events that came from elsewhere, and
which `scripts/generate_golden.py` refuses to freeze anything without.
The two together are the actual guarantee: reality says the ephemeris and
the ayanamsa are right, and these files say nothing has moved since.

If one of these fails, the question is never "how do I regenerate it".
It is "what changed, and is the new answer better". Regenerating makes a
failing test green without anyone having decided that.
"""

from __future__ import annotations

import json
from datetime import datetime
from pathlib import Path
from typing import Any
from zoneinfo import ZoneInfo

import pytest
from skyfield.timelib import Timescale

from app.core.ayanamsa import AyanamsaCalculator, AyanamsaSystem
from app.core.chart import compute_navamsa, compute_rasi
from app.core.constants import Planet
from app.core.dasha import build_vimshottari
from app.core.ephemeris import SkyfieldEphemeris

FIXTURES = Path(__file__).resolve().parents[3] / "tests" / "fixtures" / "charts"
EXPECTED = FIXTURES / "expected"

PROFILES: list[dict[str, Any]] = json.loads((FIXTURES / "profiles.json").read_text())["profiles"]
IDS = [profile["id"] for profile in PROFILES]

# Must match scripts/generate_golden.py. Six decimals of a degree is
# 3.6 milliarcseconds — finer than anything astrology resolves, coarser
# than last-bit float noise between platforms.
PLACES = 6

# How far a dasha boundary may differ from its golden value.
#
# The dasha arithmetic uses Decimal and is exact. Its INPUT is not: the
# Moon's longitude is a float from skyfield, and the last bit of it
# differs between CPUs. The Vimshottari balance scales one nakshatra —
# 13.33 degrees — across 120 years, which is 2.84e8 seconds per degree,
# so one ulp of longitude (5.7e-14 deg) moves a boundary by up to 16
# microseconds. Measured: 2.0 microseconds for this fixture set.
#
# This test asserted exact microsecond equality on the first attempt and
# CI disagreed with my machine by one and two microseconds. The claim
# that Decimal makes the boundaries reproducible to the microsecond was
# wrong; Decimal makes the arithmetic exact, and nothing can make a float
# input identical across platforms.
#
# One second is 500,000 times the observed drift, and still five orders
# of magnitude tighter than the failure Decimal exists to prevent — float
# error accumulating across three levels of subdivision into dates wrong
# by DAYS. A regression in the subdivision moves boundaries by months.
DASHA_TOLERANCE_SECONDS = 1.0


def _round(value: float | None) -> float | None:
    return None if value is None else round(value, PLACES)


def test_there_are_thirty_profiles() -> None:
    """The count is part of the contract.

    Phase 2's gate asks for thirty. Without this, deleting the awkward
    ones — the date-line pair, the quarter-hour offsets, the two
    boundary regressions — turns the suite green and removes exactly the
    coverage that was hard to get.
    """
    assert len(PROFILES) == 30, (
        f"profiles.json has {len(PROFILES)} profiles; the Phase 2 gate specifies 30. "
        "Rebuild with scripts/build_profiles.py."
    )


def test_every_profile_has_a_golden_directory() -> None:
    """A profile with no expected output is a profile nothing tests."""
    missing = [
        profile["id"]
        for profile in PROFILES
        if not (EXPECTED / profile["id"] / "expected_d1.json").exists()
    ]
    assert not missing, (
        f"{len(missing)} profiles have no golden output: {missing}. "
        "Run scripts/generate_golden.py — which will refuse if the external "
        "reference suite does not pass first."
    )


def test_no_golden_directory_is_orphaned() -> None:
    """And the reverse: a stale directory is a fixture nothing feeds.

    Renaming a profile leaves its old golden files behind, where they
    look like coverage and are never compared against anything.
    """
    if not EXPECTED.exists():
        pytest.fail(f"{EXPECTED} does not exist; run scripts/generate_golden.py")

    on_disk = {path.name for path in EXPECTED.iterdir() if path.is_dir()}
    orphans = sorted(on_disk - set(IDS))
    assert not orphans, (
        f"golden directories with no matching profile: {orphans}. "
        "They are compared against nothing and will drift silently."
    )


@pytest.mark.parametrize("profile", PROFILES, ids=IDS)
def test_chart_matches_its_golden_file(
    profile: dict[str, Any],
    ephemeris: SkyfieldEphemeris,
    timescale: Timescale,
    ayanamsa: AyanamsaCalculator,
) -> None:
    expected = json.loads((EXPECTED / profile["id"] / "expected_d1.json").read_text())

    local = datetime.fromisoformat(
        f"{profile['birth_date']}T{profile['birth_time'] or '12:00'}"
    ).replace(tzinfo=ZoneInfo(profile["timezone"]))
    t = timescale.from_datetime(local)

    # The offset is an assertion about tzdata, not about this engine, and
    # it is checked here as well as in Go so a tzdata update that moves a
    # historical zone is caught in whichever suite runs first.
    assert int(local.utcoffset().total_seconds() / 60) == expected["utc_offset_min"]  # type: ignore[union-attr]

    rasi = compute_rasi(
        ephemeris,
        ayanamsa,
        t,
        profile["latitude"],
        profile["longitude"],
        time_known=profile["time_accuracy"] != "unknown",
        system=AyanamsaSystem.LAHIRI,
    )

    assert _round(ayanamsa.at_time(t, AyanamsaSystem.LAHIRI)) == expected["ayanamsa_value"]
    _assert_rasi(rasi, expected["rasi"], profile["id"], "rasi")
    _assert_rasi(compute_navamsa(rasi), expected["navamsa"], profile["id"], "navamsa")


@pytest.mark.parametrize("profile", PROFILES, ids=IDS)
def test_dasha_tree_matches_its_golden_file(
    profile: dict[str, Any],
    ephemeris: SkyfieldEphemeris,
    timescale: Timescale,
    ayanamsa: AyanamsaCalculator,
) -> None:
    expected = json.loads((EXPECTED / profile["id"] / "expected_dasha.json").read_text())

    local = datetime.fromisoformat(
        f"{profile['birth_date']}T{profile['birth_time'] or '12:00'}"
    ).replace(tzinfo=ZoneInfo(profile["timezone"]))
    t = timescale.from_datetime(local)
    time_known = profile["time_accuracy"] != "unknown"

    if not time_known:
        # The invariant, asserted rather than assumed: a profile with no
        # birth time has NO dasha tree. Producing one from a guessed noon
        # gives dates wrong by months, and nothing downstream can tell.
        assert expected["periods"] is None, (
            f"{profile['id']} has an unknown birth time but a frozen dasha tree; "
            "the tree would have been built from a guessed noon"
        )
        return

    rasi = compute_rasi(
        ephemeris,
        ayanamsa,
        t,
        profile["latitude"],
        profile["longitude"],
        time_known=True,
        system=AyanamsaSystem.LAHIRI,
    )
    moon = next(p for p in rasi.planets if p.planet is Planet.MOON)
    tree = build_vimshottari(moon.longitude, local.astimezone(ZoneInfo("UTC")))

    _assert_dasha(tree, expected["periods"], profile["id"], "")


# ─── comparison helpers ──────────────────────────────────────────────
#
# Field by field rather than by serialising and diffing whole blobs. A
# blob comparison says "these 400 lines differ"; this says which planet,
# which field, and both values — which is the difference between a
# five-minute diagnosis and an afternoon.


def _assert_rasi(actual: Any, expected: dict[str, Any], fixture: str, chart: str) -> None:
    where = f"{fixture} [{chart}]"

    if expected["ascendant"] is None:
        assert actual.ascendant is None, (
            f"{where}: an ascendant was computed for a profile whose golden file has "
            "none. Without a birth time the rising sign is a guess, and a guess "
            "rendered as a fact is the worst outcome here."
        )
    else:
        assert actual.ascendant is not None, f"{where}: ascendant is missing"
        for field in ("sign", "sign_index", "nakshatra", "pada"):
            assert getattr(actual.ascendant, field) == expected["ascendant"][field], (
                f"{where}: ascendant {field} is "
                f"{getattr(actual.ascendant, field)!r}, golden says "
                f"{expected['ascendant'][field]!r}"
            )
        for field in ("longitude", "degree"):
            assert _round(getattr(actual.ascendant, field)) == expected["ascendant"][field], (
                f"{where}: ascendant {field} is "
                f"{_round(getattr(actual.ascendant, field))}, golden says "
                f"{expected['ascendant'][field]}"
            )

    if expected["houses"] is None:
        assert actual.houses is None, f"{where}: houses were computed with no birth time"
    else:
        assert actual.houses is not None, f"{where}: houses are missing"
        assert len(actual.houses) == len(expected["houses"])
        for house, golden in zip(actual.houses, expected["houses"], strict=True):
            assert house.house == golden["house"]
            assert house.sign == golden["sign"], (
                f"{where}: house {house.house} is in {house.sign}, golden says {golden['sign']}"
            )
            assert str(house.lord) == golden["lord"]
            assert [str(p) for p in house.planets] == golden["planets"], (
                f"{where}: house {house.house} holds "
                f"{[str(p) for p in house.planets]}, golden says {golden['planets']}"
            )

    assert len(actual.planets) == len(expected["planets"])
    for planet, golden in zip(actual.planets, expected["planets"], strict=True):
        assert str(planet.planet) == golden["planet"]
        for field in (
            "sign",
            "sign_index",
            "house",
            "nakshatra",
            "nakshatra_index",
            "pada",
            "is_retrograde",
            "is_combust",
            "dignity",
        ):
            assert getattr(planet, field) == golden[field], (
                f"{where}: {planet.planet} {field} is {getattr(planet, field)!r}, "
                f"golden says {golden[field]!r}"
            )
        for field in ("longitude", "degree", "speed"):
            assert _round(getattr(planet, field)) == golden[field], (
                f"{where}: {planet.planet} {field} is "
                f"{_round(getattr(planet, field))}, golden says {golden[field]}"
            )


def _assert_dasha(actual: Any, expected: list[dict[str, Any]], fixture: str, path: str) -> None:
    assert len(actual) == len(expected), (
        f"{fixture}{path}: {len(actual)} periods, golden has {len(expected)}"
    )

    for period, golden in zip(actual, expected, strict=True):
        here = f"{path}/{golden['planet']}"
        assert str(period.planet) == golden["planet"], (
            f"{fixture}{here}: planet is {period.planet}, golden says {golden['planet']}"
        )
        assert period.level == golden["level"]

        for field, actual_moment in (("start", period.start), ("end", period.end)):
            expected_moment = datetime.fromisoformat(golden[field])
            drift = abs((actual_moment - expected_moment).total_seconds())
            assert drift <= DASHA_TOLERANCE_SECONDS, (
                f"{fixture}{here}: {field} is {actual_moment.isoformat()}, golden says "
                f"{golden[field]} — {drift:.6f} seconds apart.\n"
                "The tolerance absorbs a last-bit difference in the Moon's longitude "
                "between CPUs, worth about 16 microseconds. A drift this large is a "
                "change in the subdivision itself."
            )

        _assert_dasha(period.children, golden["children"], fixture, here)
