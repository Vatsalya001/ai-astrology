"""The chart contract.

These models ARE the cross-service contract. They generate the OpenAPI
document, which generates the Go client, and from Phase 4 the AI layer
reads this JSON for the rest of the project's life. A field renamed here
is a breaking change three services away.

── Why `schema_version` exists from the first release ──

A stored chart outlives the code that produced it. Someone will ask in
2028 why a reading from 2026 said what it said, and answering that means
being able to tell which shape the stored JSON is in. Adding the version
later is impossible: the rows already written have no version, and you
cannot tell them apart from the ones that do.

── Why absence is modelled, not defaulted ──

`ascendant` and `dashas` are `| None` rather than optional-with-a-default
because an unknown birth time genuinely has no ascendant. A default would
let a caller treat "we could not know" as "0° Aries", which is precisely
the fabrication this service exists to prevent.
"""

from __future__ import annotations

from datetime import datetime
from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field

#: Bumped when the shape changes in a way a consumer must notice.
#:
#: Additive optional fields do not bump it. Renames, removals and changed
#: meanings do.
CHART_SCHEMA_VERSION = 1

Longitude = Annotated[float, Field(ge=0, lt=360, description="Sidereal ecliptic longitude")]
DegreeInSign = Annotated[float, Field(ge=0, lt=30)]
SignIndex = Annotated[int, Field(ge=0, le=11, description="0 = Aries")]
HouseNumber = Annotated[int, Field(ge=1, le=12)]
Pada = Annotated[int, Field(ge=1, le=4)]

TimeAccuracy = Literal["exact", "approximate", "unknown"]
Dignity = Literal["exalted", "debilitated", "own_sign", "moolatrikona", "neutral"]
ChartType = Literal["D1", "D9", "D10"]


class Strict(BaseModel):
    """Base for every model here.

    `extra="forbid"` on the REQUEST side is the point: a typo in a field
    name is a 422 rather than a silently ignored parameter that leaves
    the service computing a chart for a different birth time than the
    caller asked for.
    """

    model_config = ConfigDict(extra="forbid")


# ─── requests ────────────────────────────────────────────────────────


class BirthData(Strict):
    """Everything needed to compute a chart.

    The instant is a single aware UTC timestamp rather than a local date,
    a local time and a zone. Go owns timezone resolution — its stdlib
    carries the full historical tzdata, including the 1942-45 Indian
    wartime offset — and resolves it once when the profile is created.
    Re-deriving it here would be a second implementation of the hardest
    part of the problem, free to disagree with the first.
    """

    utc_instant: datetime = Field(
        description="Birth instant in UTC. Must be timezone-aware.",
        examples=["1994-08-17T09:05:00Z"],
    )
    latitude: Annotated[float, Field(ge=-90, le=90)]
    longitude: Annotated[float, Field(ge=-180, le=180)]

    time_accuracy: TimeAccuracy = "exact"
    """`unknown` omits the ascendant, the houses and the dashas rather
    than computing them from an assumed time."""

    ayanamsa: Literal["lahiri", "raman", "kp"] = "lahiri"
    house_system: Literal["whole_sign"] = "whole_sign"
    """Only whole sign in Phase 2. Placidus and Sripati are `pyswisseph`
    conveniences we gave up with ADR-003 and will implement when bhava
    chalit is actually wanted."""


class ChartRequest(Strict):
    birth: BirthData
    include_navamsa: bool = True
    include_dashas: bool = True
    include_yogas: bool = True


class DashaRequest(Strict):
    """Dashas alone, for a caller that already has the Moon's position."""

    moon_longitude: Longitude
    birth_instant: datetime
    max_level: Annotated[int, Field(ge=1, le=3)] = 3


class TransitRequest(Strict):
    at: datetime
    natal_moon_sign: SignIndex
    natal_ascendant_sign: SignIndex | None = None
    ayanamsa: Literal["lahiri", "raman", "kp"] = "lahiri"


# ─── responses ───────────────────────────────────────────────────────


class ChartMeta(Strict):
    """Provenance. Every one of these fields answers "why did it say that?"."""

    schema_version: int = CHART_SCHEMA_VERSION
    calculation_system: Literal["vedic"] = "vedic"
    ayanamsa: str
    ayanamsa_value: float
    """The actual offset applied, not just its name. Two releases can
    both say "lahiri" and disagree by arcseconds; this records which one
    ran."""

    house_system: str
    engine_version: str
    """e.g. "skyfield-1.55+de421+schema1". Lets a library upgrade be
    detected and the affected charts recomputed."""

    computed_at: datetime
    time_accuracy: TimeAccuracy


class PlanetPosition(Strict):
    planet: str
    longitude: Longitude
    sign: str
    sign_index: SignIndex
    degree: DegreeInSign
    house: Annotated[int, Field(ge=0, le=12)]
    """0 when the birth time is unknown — there are no houses to be in."""

    nakshatra: str
    nakshatra_index: Annotated[int, Field(ge=0, le=26)]
    pada: Pada
    is_retrograde: bool
    is_combust: bool
    dignity: Dignity
    speed: float
    aspects: list[HouseNumber] = Field(default_factory=list)


class AscendantPosition(Strict):
    longitude: Longitude
    sign: str
    sign_index: SignIndex
    degree: DegreeInSign
    nakshatra: str
    pada: Pada


class HousePosition(Strict):
    house: HouseNumber
    sign: str
    sign_index: SignIndex
    lord: str
    planets: list[str] = Field(default_factory=list)


class YogaResult(Strict):
    name: str
    strength: Literal["strong", "moderate"]
    """Never a number. A score would imply a precision the tradition does
    not have and the product could not defend."""

    involved_planets: list[str]
    involved_houses: list[HouseNumber]


class DashaPeriodOut(Strict):
    planet: str
    start: datetime
    end: datetime
    level: Annotated[int, Field(ge=1, le=3)]
    children: list[DashaPeriodOut] = Field(default_factory=list)


class DivisionalChart(Strict):
    """A D9 or D10. No yogas — they are read from the rasi."""

    ascendant: AscendantPosition | None
    houses: list[HousePosition] | None
    planets: list[PlanetPosition]


class ChartSummary(Strict):
    """The four facts every UI card and AI context needs.

    Present so a consumer does not have to walk the whole structure for
    "what is my moon sign" — which is the most-asked question the chart
    can answer.
    """

    sun_sign: str
    moon_sign: str
    ascendant_sign: str | None
    moon_nakshatra: str
    moon_nakshatra_pada: Pada


class ChartResponse(Strict):
    meta: ChartMeta
    ascendant: AscendantPosition | None
    houses: list[HousePosition] | None
    planets: list[PlanetPosition]
    navamsa: DivisionalChart | None = None
    dashas: list[DashaPeriodOut] | None = None
    yogas: list[YogaResult] = Field(default_factory=list)
    summary: ChartSummary


class TransitPosition(Strict):
    planet: str
    longitude: Longitude
    sign: str
    sign_index: SignIndex
    degree: DegreeInSign
    is_retrograde: bool
    house_from_moon: HouseNumber
    house_from_ascendant: HouseNumber | None


class SadeSatiResult(Strict):
    is_active: bool
    current_phase: Literal["rising", "peak", "setting"] | None
    saturn_sign: str
    moon_sign: str
    houses_from_moon: HouseNumber


class TransitResponse(Strict):
    at: datetime
    ayanamsa: str
    ayanamsa_value: float
    transits: list[TransitPosition]
    sade_sati: SadeSatiResult


class DashaResponse(Strict):
    periods: list[DashaPeriodOut]
    balance_at_birth_days: float
    """How much of the first mahadasha remained at birth. Derived rather
    than stored, so it cannot disagree with the tree."""
