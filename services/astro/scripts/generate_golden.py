"""Generate the golden chart fixtures.

Run:  uv run python scripts/generate_golden.py

**Read this before regenerating.** A golden file that encodes a bug makes
that bug permanent — strictly worse than having no test, because it
converts a bug into an assertion. Regenerating turns a failing test green
without anyone deciding the new answer is right.

So the rule is: `tests/test_external_reference.py` must pass first, and
this script refuses to run if it does not. Those thirteen assertions
compare the engine against dated astronomical events — two equinoxes, the
2020 great conjunction, the 2017 total eclipse, Mesha Sankranti, three
published Saturn transits, a Mars retrograde season and the sidereal
month. None of them came from this engine.

If a golden file changes and the reference tests still pass, the change
is in chart ASSEMBLY — houses, dignities, dasha subdivision — and a human
has to look at the diff and say why. That is the whole point of freezing
them.
"""

from __future__ import annotations

import json
import subprocess
import sys
from datetime import datetime
from pathlib import Path
from typing import Any
from zoneinfo import ZoneInfo

import skyfield.api as skyfield_api

from app.core.ayanamsa import AyanamsaCalculator, AyanamsaSystem
from app.core.chart import Rasi, compute_navamsa, compute_rasi
from app.core.constants import Planet
from app.core.dasha import build_vimshottari
from app.core.ephemeris import SkyfieldEphemeris

ROOT = Path(__file__).resolve().parents[3]
SERVICE = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "tests" / "fixtures" / "charts"
EXPECTED = FIXTURES / "expected"
KERNEL = SERVICE / "data" / "de421.bsp"

# Rounded before writing. The ephemeris is deterministic, but a float
# written at full precision makes a diff unreadable and turns a
# last-bit difference between platforms into a failing gate. Six decimal
# places of a degree is 3.6 milliarcseconds — far finer than anything
# astrology resolves and far coarser than platform noise.
PLACES = 6


def _round(value: float | None) -> float | None:
    return None if value is None else round(value, PLACES)


def require_external_reference_passes() -> None:
    """Refuse to freeze anything the independent reference disputes."""
    print("running the external reference suite before freezing …")
    result = subprocess.run(
        [sys.executable, "-m", "pytest", "tests/test_external_reference.py", "-q"],
        cwd=SERVICE,
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        print(result.stdout)
        print(result.stderr, file=sys.stderr)
        raise SystemExit(
            "\nREFUSING TO GENERATE GOLDEN FILES.\n"
            "The engine disagrees with published astronomical events, so every "
            "golden file written now would freeze that disagreement into an "
            "assertion. Fix the engine, not the fixtures."
        )
    print("  reference suite passed\n")


def rasi_to_json(rasi: Rasi) -> dict[str, Any]:
    return {
        "ascendant": (
            None
            if rasi.ascendant is None
            else {
                "longitude": _round(rasi.ascendant.longitude),
                "sign": rasi.ascendant.sign,
                "sign_index": rasi.ascendant.sign_index,
                "degree": _round(rasi.ascendant.degree),
                "nakshatra": rasi.ascendant.nakshatra,
                "pada": rasi.ascendant.pada,
            }
        ),
        "houses": (
            None
            if rasi.houses is None
            else [
                {
                    "house": house.house,
                    "sign": house.sign,
                    "sign_index": house.sign_index,
                    "lord": str(house.lord),
                    "planets": [str(p) for p in house.planets],
                }
                for house in rasi.houses
            ]
        ),
        "planets": [
            {
                "planet": str(planet.planet),
                "longitude": _round(planet.longitude),
                "sign": planet.sign,
                "sign_index": planet.sign_index,
                "degree": _round(planet.degree),
                "house": planet.house,
                "nakshatra": planet.nakshatra,
                "nakshatra_index": planet.nakshatra_index,
                "pada": planet.pada,
                "is_retrograde": planet.is_retrograde,
                "is_combust": planet.is_combust,
                "dignity": planet.dignity,
                "speed": _round(planet.speed),
            }
            for planet in rasi.planets
        ],
    }


def dasha_to_json(periods: Any) -> list[dict[str, Any]]:
    return [
        {
            "planet": str(period.planet),
            "start": period.start.isoformat(),
            "end": period.end.isoformat(),
            "level": period.level,
            "children": dasha_to_json(period.children),
        }
        for period in periods
    ]


def main() -> None:
    require_external_reference_passes()

    profiles = json.loads((FIXTURES / "profiles.json").read_text())["profiles"]

    timescale = skyfield_api.load.timescale(builtin=True)
    if not KERNEL.exists():
        raise SystemExit(f"ephemeris kernel missing at {KERNEL}; run `task astro:ephemeris`")
    ephemeris = SkyfieldEphemeris(KERNEL, timescale)
    ayanamsa = AyanamsaCalculator(ephemeris.kernel, timescale)

    EXPECTED.mkdir(parents=True, exist_ok=True)

    for profile in profiles:
        clock = profile["birth_time"] or "12:00"
        local = datetime.fromisoformat(f"{profile['birth_date']}T{clock}").replace(
            tzinfo=ZoneInfo(profile["timezone"])
        )

        stated = profile["expected_utc_offset_min"]
        actual = int(local.utcoffset().total_seconds() / 60)  # type: ignore[union-attr]
        if stated != actual:
            raise SystemExit(
                f"{profile['id']}: profiles.json says {stated} minutes, tzdata says "
                f"{actual}. Rebuild with scripts/build_profiles.py rather than "
                "generating a chart against a stale offset."
            )

        # skyfield accepts an aware datetime and converts to UTC itself.
        t = timescale.from_datetime(local)

        time_known = profile["time_accuracy"] != "unknown"
        rasi = compute_rasi(
            ephemeris,
            ayanamsa,
            t,
            profile["latitude"],
            profile["longitude"],
            time_known=time_known,
            system=AyanamsaSystem.LAHIRI,
        )
        navamsa = compute_navamsa(rasi)

        directory = EXPECTED / profile["id"]
        directory.mkdir(parents=True, exist_ok=True)

        d1 = {
            "$comment": "GENERATED by scripts/generate_golden.py. Do not hand-edit.",
            "profile_id": profile["id"],
            "utc_instant": local.astimezone(ZoneInfo("UTC")).isoformat(),
            "utc_offset_min": actual,
            "ayanamsa": "lahiri",
            "ayanamsa_value": _round(ayanamsa.at_time(t, AyanamsaSystem.LAHIRI)),
            "rasi": rasi_to_json(rasi),
            "navamsa": rasi_to_json(navamsa),
        }
        (directory / "expected_d1.json").write_text(
            json.dumps(d1, indent=2) + "\n", encoding="utf-8"
        )

        # Dashas need the Moon's exact nakshatra position, which needs a
        # birth time. An unknown one gets null — not a guess from noon.
        if time_known:
            moon = next(p for p in rasi.planets if p.planet is Planet.MOON)
            tree = build_vimshottari(moon.longitude, local.astimezone(ZoneInfo("UTC")))
            dasha: dict[str, Any] = {
                "$comment": "GENERATED by scripts/generate_golden.py. Do not hand-edit.",
                "profile_id": profile["id"],
                "periods": dasha_to_json(tree),
            }
        else:
            dasha = {
                "$comment": "GENERATED by scripts/generate_golden.py. Do not hand-edit.",
                "profile_id": profile["id"],
                "periods": None,
                "why_null": "time_accuracy is unknown; the Moon's nakshatra pada "
                "cannot be pinned down, and a dasha tree from a guessed birth time "
                "is wrong by months in a way nothing downstream can detect.",
            }

        (directory / "expected_dasha.json").write_text(
            json.dumps(dasha, indent=2) + "\n", encoding="utf-8"
        )

        print(f"  {profile['id']}")

    print(f"\nwrote {len(profiles)} golden fixtures to {EXPECTED}")


if __name__ == "__main__":
    main()
