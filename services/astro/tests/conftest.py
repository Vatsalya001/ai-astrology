"""Shared fixtures.

The ephemeris kernel is loaded once per session: it is 17 MB and opening
it per test would dominate the suite's runtime.
"""

from __future__ import annotations

from pathlib import Path

import pytest
import skyfield.api as skyfield_api
from skyfield.timelib import Timescale

from app.core.ayanamsa import AyanamsaCalculator
from app.core.ephemeris import SkyfieldEphemeris

KERNEL_PATH = Path(__file__).resolve().parent.parent / "data" / "de421.bsp"


@pytest.fixture(scope="session")
def timescale() -> Timescale:
    """Skyfield's timescale, from the built-in tables.

    `builtin=True` matters: the default loader downloads leap-second and
    Earth-orientation files, and this service must never reach the
    network. The built-in tables are accurate to well under a second,
    which is far below anything astrology resolves.
    """
    return skyfield_api.load.timescale(builtin=True)


@pytest.fixture(scope="session")
def ephemeris(timescale: Timescale) -> SkyfieldEphemeris:
    if not KERNEL_PATH.exists():
        # Fail, never skip. A skipped ephemeris suite is a green run that
        # proved nothing — the exact failure mode this repo already hit
        # with silently-skipped Docker integration tests.
        pytest.fail(
            f"ephemeris kernel missing at {KERNEL_PATH}. Run `task astro:ephemeris` to vendor it."
        )
    return SkyfieldEphemeris(KERNEL_PATH, timescale)


@pytest.fixture(scope="session")
def ayanamsa(ephemeris: SkyfieldEphemeris, timescale: Timescale) -> AyanamsaCalculator:
    return AyanamsaCalculator(ephemeris.kernel, timescale)


@pytest.fixture(scope="session")
def ayanamsa_module(
    ayanamsa: AyanamsaCalculator, timescale: Timescale
) -> tuple[AyanamsaCalculator, Timescale]:
    """Both together, for hypothesis tests.

    Hypothesis re-runs a test function many times and does not combine
    well with multiple function-scoped fixtures, so the pair is handed
    over once.
    """
    return ayanamsa, timescale


@pytest.fixture(scope="session")
def ephemeris_module(
    ephemeris: SkyfieldEphemeris, timescale: Timescale
) -> tuple[SkyfieldEphemeris, Timescale]:
    """Both together, for hypothesis tests. See `ayanamsa_module`."""
    return ephemeris, timescale
