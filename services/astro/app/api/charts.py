"""The chart endpoints.

A thin adapter, deliberately: parse the request, call `core`, shape the
response. No astrology happens in this file. The constitution says
`app/api/` is an adapter and `app/core/` holds the logic, and the
practical reason is that everything in `core` is golden-file testable
precisely because it never touches HTTP.

The ephemeris and the timescale are built once at import and shared. The
kernel is 17 MB and opening it per request would dominate the 200ms
budget the spec sets for a whole chart.
"""

from __future__ import annotations

import functools
from datetime import UTC, datetime
from pathlib import Path

import skyfield
import skyfield.api as skyfield_api
from fastapi import APIRouter, HTTPException, status
from skyfield.timelib import Time, Timescale

from app.core.aspects import aspects_of
from app.core.ayanamsa import AyanamsaCalculator, AyanamsaSystem
from app.core.chart import PlacedPlanet, Rasi, compute_navamsa, compute_rasi
from app.core.constants import Planet
from app.core.dasha import DashaPeriod, build_vimshottari
from app.core.ephemeris import SkyfieldEphemeris
from app.core.transit import compute_transits, sade_sati_at
from app.core.yoga import detect_all
from app.schemas.chart import (
    CHART_SCHEMA_VERSION,
    AscendantPosition,
    ChartMeta,
    ChartRequest,
    ChartResponse,
    ChartSummary,
    DashaPeriodOut,
    DashaRequest,
    DashaResponse,
    DivisionalChart,
    HousePosition,
    PlanetPosition,
    SadeSatiResult,
    TransitPosition,
    TransitRequest,
    TransitResponse,
    YogaResult,
)
from app.settings import settings

router = APIRouter(prefix="/v1", tags=["charts"])

KERNEL_PATH = Path(__file__).resolve().parent.parent.parent / "data" / "de421.bsp"


@functools.cache
def _timescale() -> Timescale:
    """Built-in tables, never downloaded.

    `builtin=True` matters: the default loader fetches leap-second and
    Earth-orientation files, and this service has no network. The
    built-in tables are accurate to well under a second, far below
    anything astrology resolves.
    """
    return skyfield_api.load.timescale(builtin=True)


@functools.cache
def _ephemeris() -> SkyfieldEphemeris:
    return SkyfieldEphemeris(KERNEL_PATH, _timescale())


@functools.cache
def _ayanamsa() -> AyanamsaCalculator:
    return AyanamsaCalculator(_ephemeris().kernel, _timescale())


def engine_version() -> str:
    """Identifies exactly what produced a chart.

    Stored on every row so a library upgrade can be detected and the
    affected charts recomputed, rather than leaving two subtly different
    generations in one table with no way to tell them apart.
    """
    return f"skyfield-{skyfield.__version__}+de421+schema{CHART_SCHEMA_VERSION}"


def _to_skyfield_time(moment: datetime) -> Time:
    """Convert an aware datetime, refusing a naive one.

    A naive timestamp would be silently read as UTC. For an Indian birth
    that is five and a half hours of error, and the ascendant moves a
    degree every four minutes — so the chart would be wrong by more than
    a whole sign while looking entirely normal.
    """
    if moment.tzinfo is None:
        raise HTTPException(
            status_code=status.HTTP_422_UNPROCESSABLE_ENTITY,
            detail="utc_instant must be timezone-aware; this service never assumes a zone",
        )
    return _timescale().from_datetime(moment.astimezone(UTC))


def _planet_out(placed: PlacedPlanet) -> PlanetPosition:
    aspects = [a.house for a in aspects_of(placed.planet, placed.house)] if placed.house else []
    return PlanetPosition(
        planet=str(placed.planet),
        longitude=placed.longitude,
        sign=placed.sign,
        sign_index=placed.sign_index,
        degree=placed.degree,
        house=placed.house,
        nakshatra=placed.nakshatra,
        nakshatra_index=placed.nakshatra_index,
        pada=placed.pada,
        is_retrograde=placed.is_retrograde,
        is_combust=placed.is_combust,
        dignity=placed.dignity,
        speed=placed.speed,
        aspects=aspects,
    )


def _ascendant_out(rasi: Rasi) -> AscendantPosition | None:
    if rasi.ascendant is None:
        return None
    return AscendantPosition(
        longitude=rasi.ascendant.longitude,
        sign=rasi.ascendant.sign,
        sign_index=rasi.ascendant.sign_index,
        degree=rasi.ascendant.degree,
        nakshatra=rasi.ascendant.nakshatra,
        pada=rasi.ascendant.pada,
    )


def _houses_out(rasi: Rasi) -> list[HousePosition] | None:
    if rasi.houses is None:
        return None
    return [
        HousePosition(
            house=h.house,
            sign=h.sign,
            sign_index=h.sign_index,
            lord=str(h.lord),
            planets=[str(p) for p in h.planets],
        )
        for h in rasi.houses
    ]


def _dasha_out(period: DashaPeriod) -> DashaPeriodOut:
    return DashaPeriodOut(
        planet=str(period.planet),
        start=period.start,
        end=period.end,
        level=period.level,
        children=[_dasha_out(child) for child in period.children],
    )


@router.post("/charts/compute", response_model=ChartResponse)
def compute_chart(request: ChartRequest) -> ChartResponse:
    """Birth data in, full chart out.

    Idempotent and stateless. Nothing is written anywhere — `api-service`
    owns every byte of persistence, and this service is a pure function
    that happens to be reachable over HTTP.
    """
    t = _to_skyfield_time(request.birth.utc_instant)
    system = AyanamsaSystem(request.birth.ayanamsa)
    time_known = request.birth.time_accuracy != "unknown"

    rasi = compute_rasi(
        _ephemeris(),
        _ayanamsa(),
        t,
        request.birth.latitude,
        request.birth.longitude,
        time_known=time_known,
        system=system,
    )

    moon = next(p for p in rasi.planets if p.planet is Planet.MOON)
    sun = next(p for p in rasi.planets if p.planet is Planet.SUN)

    navamsa = None
    if request.include_navamsa:
        d9 = compute_navamsa(rasi)
        navamsa = DivisionalChart(
            ascendant=_ascendant_out(d9),
            houses=_houses_out(d9),
            planets=[_planet_out(p) for p in d9.planets],
        )

    # Dashas need the Moon's nakshatra, which needs a birth time accurate
    # enough that the Moon has not changed nakshatra within the day. It
    # can move a full nakshatra in under a day, so an unknown time means
    # no dashas — not approximate ones.
    dashas = None
    if request.include_dashas and time_known:
        dashas = [
            _dasha_out(period)
            for period in build_vimshottari(moon.longitude, request.birth.utc_instant)
        ]

    yogas = (
        [
            YogaResult(
                name=y.name,
                strength=y.strength,  # type: ignore[arg-type]
                involved_planets=[str(p) for p in y.involved_planets],
                involved_houses=list(y.involved_houses),
            )
            for y in detect_all(rasi)
        ]
        if request.include_yogas
        else []
    )

    return ChartResponse(
        meta=ChartMeta(
            ayanamsa=request.birth.ayanamsa,
            ayanamsa_value=rasi.ayanamsa_value,
            house_system=request.birth.house_system,
            engine_version=engine_version(),
            computed_at=datetime.now(UTC),
            time_accuracy=request.birth.time_accuracy,
        ),
        ascendant=_ascendant_out(rasi),
        houses=_houses_out(rasi),
        planets=[_planet_out(p) for p in rasi.planets],
        navamsa=navamsa,
        dashas=dashas,
        yogas=yogas,
        summary=ChartSummary(
            sun_sign=sun.sign,
            moon_sign=moon.sign,
            ascendant_sign=rasi.ascendant.sign if rasi.ascendant else None,
            moon_nakshatra=moon.nakshatra,
            moon_nakshatra_pada=moon.pada,
        ),
    )


@router.post("/dashas/compute", response_model=DashaResponse)
def compute_dashas(request: DashaRequest) -> DashaResponse:
    """The Vimshottari tree from a Moon longitude.

    Separate from the chart endpoint so `api-service` can recompute a
    tree — after a birth-time correction, say — without recomputing every
    planet.
    """
    if request.birth_instant.tzinfo is None:
        raise HTTPException(
            status_code=status.HTTP_422_UNPROCESSABLE_ENTITY,
            detail="birth_instant must be timezone-aware",
        )

    periods = build_vimshottari(
        request.moon_longitude, request.birth_instant, max_level=request.max_level
    )

    # The balance is the first mahadasha's remainder at birth. That period
    # began BEFORE the birth, so this subtraction is the whole reason the
    # tree is built from its notional start rather than clamped.
    balance = periods[0].end - request.birth_instant

    return DashaResponse(
        periods=[_dasha_out(p) for p in periods],
        balance_at_birth_days=balance.total_seconds() / 86400.0,
    )


@router.post("/transits/compute", response_model=TransitResponse)
def compute_transit(request: TransitRequest) -> TransitResponse:
    """Current positions against a natal chart, plus Sade Sati."""
    t = _to_skyfield_time(request.at)
    system = AyanamsaSystem(request.ayanamsa)

    transits = compute_transits(
        _ephemeris(),
        _ayanamsa(),
        t,
        request.natal_moon_sign,
        request.natal_ascendant_sign,
        system,
    )
    sade_sati = sade_sati_at(_ephemeris(), _ayanamsa(), t, request.natal_moon_sign, system)

    return TransitResponse(
        at=request.at,
        ayanamsa=request.ayanamsa,
        ayanamsa_value=_ayanamsa().at_time(t, system),
        transits=[
            TransitPosition(
                planet=str(x.planet),
                longitude=x.longitude,
                sign=x.sign,
                sign_index=x.sign_index,
                degree=x.degree,
                is_retrograde=x.is_retrograde,
                house_from_moon=x.house_from_moon,
                house_from_ascendant=x.house_from_ascendant,
            )
            for x in transits
        ],
        sade_sati=SadeSatiResult(
            is_active=sade_sati.is_active,
            current_phase=sade_sati.current_phase,  # type: ignore[arg-type]
            saturn_sign=sade_sati.saturn_sign,
            moon_sign=sade_sati.moon_sign,
            houses_from_moon=sade_sati.houses_from_moon,
        ),
    )


__all__ = ["engine_version", "router", "settings"]
