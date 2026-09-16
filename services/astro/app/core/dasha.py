"""Vimshottari dasha — the 120-year planetary period system.

Pure. The birth instant arrives as an argument; nothing here reads a
clock.

── Why Decimal, stated against what was actually measured ──

The spec says this must not use `float`, and the reason given is that
dasha dates drift by days. I wrote a docstring here repeating that, then
tested it, and the claim does not hold for this arithmetic. Recording the
real finding, because a comment asserting a failure mode that does not
occur is worse than no comment: the next person to touch this will
"verify" it, find nothing, and stop trusting the rest of the file.

Measured, at microsecond resolution over the full 120-year cycle and
three levels of subdivision:

    Decimal, summed durations    0 microseconds of drift
    float,   summed durations    0 microseconds of drift

Both are exact, and not by accident. A Vimshottari year is
365.25 x 86400 x 10^6 = 31,557,600,000,000 microseconds exactly, the
lord years are small integers, and 120 divides their products evenly.

So `Decimal` is not load-bearing against a demonstrated bug here. It is
used because the margin is thinner than it looks:

    120 years in milliseconds   3.79e12    float64 headroom  x2378
    120 years in microseconds   3.79e15    float64 headroom  x2.4
    120 years in nanoseconds    3.79e18    EXCEEDS 2^53

Float64 represents integers exactly only up to 2^53. At the resolution
this module uses, the whole cycle fits with a factor of 2.4 to spare.
That is enough today and would evaporate on any of: nanosecond
timestamps, a longer cycle, a fourth subdivision level, or a year length
that is not a clean multiple. `Decimal` removes the dependence on that
margin rather than tracking it.

── Why cumulative boundaries ──

Same honesty applies: summing durations was measured at zero drift too,
so this is a structural choice, not a bug fix.

Each period's boundaries come from the CUMULATIVE fraction of the parent:

    boundary(i) = parent_start + parent_span * (cumulative_years(i) / 120)

The final cumulative total is exactly 120, so the last boundary lands on
the parent's end by construction. Summing durations instead makes the
same property depend on nine roundings happening to cancel — which they
currently do, and which nothing enforces. The tiling tests would catch a
regression either way; this way there is nothing to regress.

── The first mahadasha starts before birth ──

Vimshottari is seeded by the Moon's nakshatra. The lord of that nakshatra
rules the first mahadasha, and the portion already elapsed at birth is
proportional to how far through the nakshatra the Moon has travelled.

That period therefore began BEFORE the birth — notionally, in a previous
life, which is the tradition's own framing. The tree is built from that
notional start so every level tiles exactly; the "balance at birth" is
then simply the first mahadasha's end minus the birth instant, and
callers that only want current and future periods filter on the end date.

Clamping the first period to the birth instant instead would break the
tiling property at the very first node, which is the one place an error
would propagate through everything after it.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timedelta
from decimal import Decimal

from app.core.constants import (
    NAKSHATRA_ARC,
    VIMSHOTTARI_SEQUENCE,
    VIMSHOTTARI_TOTAL_YEARS,
    Planet,
    nakshatra_index,
    normalise_longitude,
)

#: Days in a Vimshottari year.
#:
#: 365.25 — the Julian year, which is the convention these periods are
#: reckoned in. Not 365, and not the tropical year: changing it shifts
#: every dasha date, so it is stated once, here.
YEAR_DAYS = Decimal("365.25")

TOTAL_YEARS = Decimal(VIMSHOTTARI_TOTAL_YEARS)

MAX_LEVEL = 3
"""Maha, Antar, Pratyantar. Deeper levels exist in the tradition but are
not computed: each level multiplies the node count by nine, and the
fourth would put 6,561 rows behind a single chart for a precision no
reading uses."""


@dataclass(frozen=True, slots=True)
class DashaPeriod:
    """One node of the dasha tree."""

    planet: Planet
    start: datetime
    end: datetime
    level: int
    children: tuple[DashaPeriod, ...] = field(default=())

    @property
    def duration(self) -> timedelta:
        return self.end - self.start

    def active_at(self, moment: datetime) -> bool:
        """Half-open: start <= moment < end.

        Half-open because one period's end is the next one's start. A
        closed interval would report two dashas running at the boundary
        instant, and "which period am I in" must have one answer.
        """
        return self.start <= moment < self.end


def _sequence_from(lord: Planet) -> list[tuple[Planet, Decimal]]:
    """The nine lords in order, rotated to begin at `lord`.

    The order is fixed and cyclic — the sequence after Ketu is always
    Venus, Sun, Moon — so a dasha tree is entirely determined by where it
    starts.
    """
    planets = [p for p, _ in VIMSHOTTARI_SEQUENCE]
    start = planets.index(lord)
    rotated = VIMSHOTTARI_SEQUENCE[start:] + VIMSHOTTARI_SEQUENCE[:start]
    return [(planet, Decimal(years)) for planet, years in rotated]


def nakshatra_lord(moon_longitude: float) -> Planet:
    """The dasha lord of the nakshatra the Moon occupies.

    The 27 nakshatras cycle through the 9 lords three times, so the lord
    is simply `nakshatra_index % 9`. This is why the nakshatra boundary
    fix in `subdivision_index` matters here: a Moon one arcsecond the
    wrong side of a cusp gets a different lord, and therefore a
    completely different life-period tree — not a slightly shifted one.
    """
    return VIMSHOTTARI_SEQUENCE[nakshatra_index(moon_longitude) % 9][0]


def elapsed_fraction(moon_longitude: float) -> Decimal:
    """How far through its nakshatra the Moon has travelled, in [0, 1).

    This single number decides the balance of the first mahadasha, and so
    the phase of every period that follows.
    """
    within = Decimal(str(normalise_longitude(moon_longitude) % NAKSHATRA_ARC))
    return within / Decimal(str(NAKSHATRA_ARC))


def _subdivide(
    parent: Planet,
    start: datetime,
    span: timedelta,
    level: int,
    max_level: int,
) -> tuple[DashaPeriod, ...]:
    """Split a period into its nine sub-periods.

    Boundaries come from cumulative fractions of the parent, never from
    summing durations — see the module docstring. The final cumulative
    total is exactly 120/120, so the last child ends exactly when the
    parent does.
    """
    if level > max_level:
        return ()

    # Microseconds, as an integer, is the finest unit a datetime can
    # represent. Doing the arithmetic in Decimal and rounding once at the
    # boundary keeps the tiling exact at the resolution that survives.
    span_microseconds = Decimal(span // timedelta(microseconds=1))

    children: list[DashaPeriod] = []
    cumulative = Decimal(0)
    previous_offset = 0

    for planet, years in _sequence_from(parent):
        cumulative += years
        offset = int(span_microseconds * cumulative / TOTAL_YEARS)

        child_start = start + timedelta(microseconds=previous_offset)
        child_end = start + timedelta(microseconds=offset)

        children.append(
            DashaPeriod(
                planet=planet,
                start=child_start,
                end=child_end,
                level=level,
                children=_subdivide(
                    planet, child_start, child_end - child_start, level + 1, max_level
                ),
            )
        )
        previous_offset = offset

    return tuple(children)


def build_vimshottari(
    moon_longitude: float,
    birth: datetime,
    *,
    max_level: int = MAX_LEVEL,
) -> tuple[DashaPeriod, ...]:
    """The full 120-year tree, three levels deep.

    Returns the mahadashas. The first began before `birth` — see the
    module docstring — so callers wanting only current and future periods
    filter on `end > birth`.

    `birth` must be timezone-aware. A naive datetime would silently
    assume a timezone, and this service never guesses: the dasha tree is
    anchored to an instant, and five and a half hours of error moves
    every boundary in it.
    """
    if birth.tzinfo is None:
        raise ValueError(
            "build_vimshottari needs an aware datetime; a naive one silently "
            "assumes a timezone and every dasha boundary would shift with it"
        )
    if not 0 <= max_level <= MAX_LEVEL:
        raise ValueError(f"max_level must be between 0 and {MAX_LEVEL}, got {max_level}")

    lord = nakshatra_lord(moon_longitude)
    sequence = _sequence_from(lord)

    first_years = sequence[0][1]
    elapsed_years = first_years * elapsed_fraction(moon_longitude)

    # The notional start of the first mahadasha, before the birth.
    cycle_start = birth - timedelta(
        microseconds=int(elapsed_years * YEAR_DAYS * Decimal(86_400_000_000))
    )

    mahadashas: list[DashaPeriod] = []
    cumulative = Decimal(0)

    for planet, years in sequence:
        start_offset = int(cumulative * YEAR_DAYS * Decimal(86_400_000_000))
        cumulative += years
        end_offset = int(cumulative * YEAR_DAYS * Decimal(86_400_000_000))

        start = cycle_start + timedelta(microseconds=start_offset)
        end = cycle_start + timedelta(microseconds=end_offset)

        mahadashas.append(
            DashaPeriod(
                planet=planet,
                start=start,
                end=end,
                level=1,
                children=_subdivide(planet, start, end - start, 2, max_level),
            )
        )

    return tuple(mahadashas)


def find_active(periods: tuple[DashaPeriod, ...], moment: datetime) -> list[DashaPeriod]:
    """The chain of periods running at an instant, outermost first.

    Returns at most one period per level. An empty list means the instant
    falls outside the 120-year cycle entirely, which is a real answer
    rather than an error — someone living past their 120th year has run
    out of tree.
    """
    chain: list[DashaPeriod] = []
    current = periods

    while current:
        match = next((p for p in current if p.active_at(moment)), None)
        if match is None:
            break
        chain.append(match)
        current = match.children

    return chain
