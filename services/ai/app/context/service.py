"""AstrologyContextService. PHASE-05 tasks 5.8 and 5.9.

Turns a chart Go already proved the caller owns into the two things the
pipeline needs: prose for the model, and a fact index for the validator.

── The one invariant this module exists to maintain ──

**The text and the fact index are built from the same traversal.**

`app/orchestrator/context.py` already states why, and it is worth
repeating where the code is: a prose summary that drifted from the fact
index would make the validator block CORRECT answers. That failure looks
like a validator bug, gets "fixed" by relaxing the validator, and the
determinism guarantee quietly stops being enforced.

So `build()` walks the chart once and emits both. There is no path that
produces one without the other, and `test_every_fact_in_the_text_is_in_the_index`
is what says so.

── Pure, by §3 ──

"Pure function over the chart JSON — no I/O, so it runs in well under
30 ms and is trivially unit-testable per intent."

`now` is a parameter rather than `datetime.now()`. The dasha depends on
it, so an ambient clock would make the context untestable and the
`context_version` unreproducible — the same rule `astro-service`'s
`app/core/` follows and for the same reason.
"""

from __future__ import annotations

from datetime import datetime

from app.classification import Intent, IntentResult
from app.context.chart import ChartPayload, DashaPeriod, PlanetPosition
from app.context.selection import Selection, selection_for
from app.orchestrator.context import ChartContext
from app.validation import FactIndex
from app.validation.facts import PlanetFact, canonical

# Bumped when the SELECTION or the RENDERING changes, not when the chart
# does. Recorded as `context_version` on every response, next to
# `prompt_version`: one says what we asked, the other says what we
# showed. Without both, a response from three weeks ago cannot be
# explained.
CONTEXT_VERSION = "astrology_context.v1"

# House names, for prose. The classical name is included because the
# corpus uses it and a response that says "the tenth house (karma bhava)"
# reads as informed rather than mechanical.
HOUSE_NAMES: dict[int, str] = {
    1: "first (tanu — self and body)",
    2: "second (dhana — wealth retained, speech)",
    3: "third (sahaja — siblings, initiative)",
    4: "fourth (bandhu — home, mother)",
    5: "fifth (putra — children, creativity)",
    6: "sixth (shatru — opposition, service)",
    7: "seventh (kalatra — partnership)",
    8: "eighth (ayur — transformation)",
    9: "ninth (dharma — fortune, teacher)",
    10: "tenth (karma — vocation, standing)",
    11: "eleventh (labha — income, gains)",
    12: "twelfth (vyaya — expenditure, foreign)",
}


class AstrologyContextService:
    """Builds the chart half of the prompt context.

    Satisfies `ChartContextBuilder`, so it drops into the Phase 4
    pipeline where `NoChartContext` currently sits — which is what
    PHASE-04 §9 meant by defining the seams early.

    ── Why the chart is passed to the constructor, not fetched ──

    One instance per request, holding the chart `api-service` sent. The
    Protocol's `build(user_id, intent)` signature takes a user id and
    this class ignores it, deliberately: there is no lookup, so there is
    no way to ask for a different user's chart. §12 requires that
    `ai-service` "cannot fetch arbitrary user charts (it has no such
    query path)", and the absence of the query is the enforcement.
    """

    def __init__(self, chart: ChartPayload | None, *, now: datetime) -> None:
        self._chart = chart
        self._now = now

    async def build(self, user_id: str, intent: IntentResult) -> ChartContext:
        """The context for one message.

        `user_id` is accepted to satisfy `ChartContextBuilder` and is not
        used. See the class docstring — that is the point.
        """
        del user_id

        if self._chart is None:
            # No chart: the profile has none computed yet, or the question
            # did not need one. Returns the same empty context Phase 4's
            # stub did, which the validator treats strictly — a personal
            # claim with no chart has invented the chart outright.
            return ChartContext(version=f"{CONTEXT_VERSION}:no-chart")

        selection = selection_for(intent.primary)
        lines, facts = self._render(self._chart, selection)

        if not lines:
            # An empty selection — MEDICAL, LEGAL, TAROT and the rest.
            # The version records WHICH intent produced nothing, so an
            # empty context in a log is distinguishable from a missing
            # chart.
            return ChartContext(version=f"{CONTEXT_VERSION}:{intent.primary}:no-facts")

        return ChartContext(
            text="\n".join(lines),
            facts=facts,
            version=f"{CONTEXT_VERSION}:{intent.primary}:{self._chart.ayanamsa}",
        )

    def _render(self, chart: ChartPayload, selection: Selection) -> tuple[list[str], FactIndex]:
        """One traversal, two outputs. See the module docstring.

        Every `lines.append` that states a checkable fact is accompanied
        by the corresponding entry in `facts`. That coupling is the
        invariant, and keeping them in one function is how it survives
        editing.

        The loop variables are named by TYPE — `planet_at`, `house_at`,
        `lord_at` — rather than all being `position`. `mypy --strict`
        rejected the shared name, and it was right to: the houses loop
        binds a `HousePosition` and the lords loop a `PlanetPosition`,
        and reading `position.lord` in the second one is a real mistake
        that a shared name invites.
        """
        lines: list[str] = []
        planet_facts: dict[str, PlanetFact] = {}
        ascendant = ""
        moon_sign = ""
        sun_sign = ""
        dasha = ""
        antardasha = ""

        def index_planet(at: PlanetPosition) -> None:
            """Record a position in the fact index.

            A closure rather than three copies, because the three places
            that index a planet — the selected list, the Moon nakshatra
            line, and the house lords — must agree about what gets
            recorded. They did not, in the first draft: the lords loop
            omitted the nakshatra, so a response correctly naming the
            10th lord's nakshatra would have been blocked as fabricated.
            """
            planet_facts[canonical(at.planet)] = PlanetFact(
                sign=at.sign,
                house=at.house,
                nakshatra=at.nakshatra,
                retrograde=at.is_retrograde,
            )

        if selection.include_ascendant and chart.ascendant.sign:
            ascendant = canonical(chart.ascendant.sign)
            ascendant_line = f"Ascendant: {chart.ascendant.sign} {chart.ascendant.degree:.1f}°"
            if chart.ascendant.nakshatra:
                ascendant_line += f", {chart.ascendant.nakshatra} nakshatra"
            lines.append(ascendant_line)

        # ── Planets ──
        #
        # Collected before the houses are rendered, because a house line
        # names its occupants and those names have to be the same ones
        # the fact index holds.
        for name in selection.planets:
            planet_at = chart.planet(name)
            if planet_at is None:
                # A chart missing a planet is a computation that failed
                # partway. Skipped rather than faked: a fact-index entry
                # for a planet nobody computed is exactly the fabrication
                # this module exists to prevent.
                continue

            index_planet(planet_at)

            detail = [
                f"{planet_at.planet}: {planet_at.sign} {planet_at.degree:.1f}°",
                f"house {planet_at.house}",
            ]
            if planet_at.nakshatra:
                detail.append(f"{planet_at.nakshatra} nakshatra")
            if planet_at.is_retrograde:
                detail.append("retrograde")
            if planet_at.is_combust:
                detail.append("combust")
            if planet_at.dignity and planet_at.dignity != "neutral":
                detail.append(planet_at.dignity)
            lines.append(", ".join(detail))

        # The luminaries' signs get their own fact-index fields, which the
        # validator checks claims against by name — "my Moon sign is
        # Scorpio" is checked against `moon_sign`, not against
        # `planets["moon"].sign`. Only set when the planet was actually
        # selected, so an unselected Moon is not silently asserted.
        moon_at = chart.planet("moon")
        if moon_at is not None and canonical("moon") in planet_facts:
            moon_sign = canonical(moon_at.sign)
        sun_at = chart.planet("sun")
        if sun_at is not None and canonical("sun") in planet_facts:
            sun_sign = canonical(sun_at.sign)

        if selection.include_moon_nakshatra and moon_at is not None and moon_at.nakshatra:
            # Stated on its own line even when the Moon's full line is
            # already present. It is the most specific thing the chart
            # says about emotional nature and it anchors the entire dasha
            # sequence, so burying it mid-line loses it.
            lines.append(f"Moon nakshatra: {moon_at.nakshatra} (pada {moon_at.pada})")
            index_planet(moon_at)
            moon_sign = canonical(moon_at.sign)

        # ── Houses ──
        for number in selection.houses:
            house_at = chart.house(number)
            if house_at is None:
                continue
            occupants = ", ".join(house_at.planets) if house_at.planets else "empty"
            lines.append(
                f"House {number} ({HOUSE_NAMES.get(number, '')}): "
                f"{house_at.sign}, lord {house_at.lord}, {occupants}"
            )

        # ── House lords ──
        #
        # The derivation §3's worked example leads with: "10th lord
        # (Saturn) → 11th house, own sign, retrograde".
        for number in selection.house_lords:
            lord = chart.lord_of(number)
            if not lord:
                continue
            lord_at = chart.planet(lord)
            if lord_at is None:
                # The lord's position was not computed. Named rather than
                # omitted, because "the 10th lord is Saturn" is itself a
                # fact worth stating and the model should not have to
                # infer the silence.
                lines.append(f"Lord of house {number}: {lord} (position not computed)")
                continue

            index_planet(lord_at)
            lord_line = f"Lord of house {number}: {lord}, in {lord_at.sign}, house {lord_at.house}"
            if lord_at.dignity and lord_at.dignity != "neutral":
                lord_line += f", {lord_at.dignity}"
            if lord_at.is_retrograde:
                lord_line += ", retrograde"
            lines.append(lord_line)

        # ── Dasha ──
        if selection.include_dasha or selection.include_dasha_tree:
            chain = chart.active_dasha(self._now)
            if chain:
                dasha = canonical(chain[0].planet)
                if len(chain) > 1:
                    antardasha = canonical(chain[1].planet)
                lines.extend(self._dasha_lines(chain, full_tree=selection.include_dasha_tree))

        facts = FactIndex(
            planets=planet_facts,
            ascendant=ascendant,
            moon_sign=moon_sign,
            sun_sign=sun_sign,
            current_dasha=dasha,
            current_antardasha=antardasha,
        )
        return lines, facts

    def _dasha_lines(self, chain: list[DashaPeriod], *, full_tree: bool) -> list[str]:
        """The active dasha chain as prose.

        Dates are rendered to the DAY, not the second. The underlying
        computation is exact — `astro-service` uses `Decimal` for the
        proportional arithmetic precisely so it is — but a response that
        quotes a dasha boundary to the second claims a precision the
        birth time does not support. A birth time rounded to the nearest
        five minutes moves a third-level boundary by hours.
        """
        labels = ("Mahadasha", "Antardasha", "Pratyantardasha")
        lines: list[str] = []

        for depth, period in enumerate(chain[: len(labels)]):
            lines.append(
                f"{labels[depth]}: {period.planet} "
                f"({period.start:%Y-%m-%d} to {period.end:%Y-%m-%d})"
            )

        if not full_tree or not chain:
            return lines

        # DASHA intent only. The siblings of the current antardasha —
        # what is coming next — which is the question somebody asking
        # about periods is usually actually asking.
        mahadasha = chain[0]
        if mahadasha.children:
            lines.append(f"Antardashas within {mahadasha.planet}:")
            for child in mahadasha.children:
                marker = "  → " if len(chain) > 1 and child is chain[1] else "    "
                lines.append(
                    f"{marker}{child.planet}: {child.start:%Y-%m-%d} to {child.end:%Y-%m-%d}"
                )

        return lines


def context_for(
    chart: ChartPayload | None, intent: Intent, *, now: datetime
) -> tuple[str, FactIndex]:
    """Synchronous convenience for tests and for the eval harness.

    The service is async only because `ChartContextBuilder` is, and the
    Protocol is async because the conversation and knowledge builders
    genuinely do I/O. This one does not, and a test that has to spin an
    event loop to check a pure function is a test nobody writes enough of.
    """
    service = AstrologyContextService(chart, now=now)
    if chart is None:
        return "", FactIndex()
    lines, facts = service._render(chart, selection_for(intent))
    return "\n".join(lines), facts
