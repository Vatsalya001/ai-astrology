"""The chart as it arrives from Go, typed.

PHASE-05 §12: "**Chart context comes from the chart Go loaded after an
ownership check** — never from an ID in the message body."

That sentence is the security design and it decides this module's shape.
`ai-service` does not look a chart up: it receives one that `api-service`
already proved the caller owns. The companion guarantee is the one §12
states next — "`ai-service` cannot fetch arbitrary user charts (it has no
such query path)" — and it is true by construction here, because there is
no query. A compromised prompt cannot ask for somebody else's chart
because nothing in this service knows how to fetch one.

── Why the models are permissive about extra fields ──

`astro-service` computes more than this service reads: speeds, combustion
flags, divisional charts, ayanamsa values. Rejecting unknown fields would
make every astro-service addition a breaking change here for no benefit,
so these models ignore what they do not use.

That is the opposite of the `extra="forbid"` rule on settings, and
deliberately: a typo in configuration is a silent misconfiguration, while
an extra field in a computed payload is just a field this consumer does
not need.
"""

from __future__ import annotations

from datetime import datetime
from typing import Any

from pydantic import BaseModel, Field


class PlanetPosition(BaseModel):
    """One body, as computed. Field names match astro-service's output."""

    planet: str
    sign: str
    house: int = Field(ge=1, le=12)
    degree: float = 0.0
    nakshatra: str = ""
    pada: int = 0
    is_retrograde: bool = False
    is_combust: bool = False
    dignity: str = ""


class HousePosition(BaseModel):
    house: int = Field(ge=1, le=12)
    sign: str
    lord: str = ""
    planets: list[str] = Field(default_factory=list)


class Ascendant(BaseModel):
    sign: str
    degree: float = 0.0
    nakshatra: str = ""
    pada: int = 0


class DashaPeriod(BaseModel):
    """One node of the Vimshottari tree.

    `children` is recursive and the tree is three levels deep by the time
    it reaches here. Typed rather than left as a dict because the context
    builder walks it to find the CURRENT period, and walking an untyped
    tree is where an off-by-one in the level becomes a wrong date.
    """

    planet: str
    start: datetime
    end: datetime
    level: int = 1
    children: list[DashaPeriod] = Field(default_factory=list)

    def contains(self, moment: datetime) -> bool:
        return self.start <= moment < self.end

    def active_chain(self, moment: datetime) -> list[DashaPeriod]:
        """This period and its active descendants, outermost first.

        Returns [] when `moment` falls outside this period, which is what
        makes the caller's search a simple scan rather than a search that
        has to know the tree's span in advance.
        """
        if not self.contains(moment):
            return []
        for child in self.children:
            if chain := child.active_chain(moment):
                return [self, *chain]
        return [self]


class ChartPayload(BaseModel):
    """What Go sends: one computed chart, plus its dasha tree.

    ── Why `ayanamsa` is carried and not assumed ──

    Because it changes the answer. A chart computed under Lahiri and read
    as though it were Raman moves planets across sign boundaries near a
    cusp, and nothing in the response would look wrong. It is recorded on
    the context version for the same reason `.claude/rules/database.md`
    requires it in every cache key.
    """

    ascendant: Ascendant
    planets: list[PlanetPosition] = Field(default_factory=list)
    houses: list[HousePosition] = Field(default_factory=list)

    dasha_periods: list[DashaPeriod] = Field(default_factory=list)

    ayanamsa: str = "lahiri"
    engine_version: str = ""
    chart_type: str = "rasi"

    model_config = {"extra": "ignore"}

    @classmethod
    def from_astro_payload(
        cls, chart_data: dict[str, Any], *, dasha_tree: dict[str, Any] | None = None
    ) -> ChartPayload:
        """Build from astro-service's `chart_data` JSONB, as Go stores it.

        The `rasi` key is unwrapped if present: astro-service returns
        `{"rasi": {...}, "navamsa": {...}, "dasamsa": {...}}` for a full
        computation, and `charts.chart_data` holds one divisional chart
        per row — so this has to accept both shapes. Guessing wrong would
        produce an empty chart and therefore an empty fact index, which
        the validator treats as "no chart was supplied" and which would
        read as a validator bug rather than a parsing one.
        """
        body = chart_data.get("rasi", chart_data)

        periods: list[DashaPeriod] = []
        if dasha_tree:
            raw_periods = dasha_tree.get("periods", dasha_tree.get("dasha_periods", []))
            periods = [DashaPeriod.model_validate(node) for node in raw_periods]

        return cls(
            ascendant=Ascendant.model_validate(body.get("ascendant", {"sign": ""})),
            planets=[PlanetPosition.model_validate(p) for p in body.get("planets", [])],
            houses=[HousePosition.model_validate(h) for h in body.get("houses", [])],
            dasha_periods=periods,
            ayanamsa=chart_data.get("ayanamsa", "lahiri"),
            engine_version=chart_data.get("engine_version", ""),
            chart_type=chart_data.get("chart_type", "rasi"),
        )

    def planet(self, name: str) -> PlanetPosition | None:
        wanted = name.strip().lower()
        for position in self.planets:
            if position.planet.strip().lower() == wanted:
                return position
        return None

    def house(self, number: int) -> HousePosition | None:
        for position in self.houses:
            if position.house == number:
                return position
        return None

    def lord_of(self, number: int) -> str:
        position = self.house(number)
        return position.lord if position else ""

    def house_of_lord(self, number: int) -> int | None:
        """Where the lord of house `number` actually sits.

        The single most-used derivation in a real reading — "the 10th lord
        is in the 11th" — and the one §3's worked example leads with.
        """
        lord = self.lord_of(number)
        if not lord:
            return None
        position = self.planet(lord)
        return position.house if position else None

    def active_dasha(self, moment: datetime) -> list[DashaPeriod]:
        """The mahadasha / antardasha / pratyantardasha chain at `moment`.

        Empty when the chart has no dasha tree, which is normal: a chart
        computed without a birth time has no Moon nakshatra and therefore
        no Vimshottari sequence at all. Treating that as an error would
        fail a chat request for a profile that is merely incomplete.
        """
        for period in self.dasha_periods:
            if chain := period.active_chain(moment):
                return chain
        return []
