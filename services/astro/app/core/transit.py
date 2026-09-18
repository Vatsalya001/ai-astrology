"""Gochara — transits, and Sade Sati.

Pure. The window to examine is always passed in; nothing here asks what
today is.

── Gochara is measured from the natal Moon ──

Western transit practice reads a transiting planet against the natal
ascendant. Vedic gochara reads it against the natal MOON sign. Both are
computed here, because the UI shows both, but the Moon-relative one is
the default and the one Sade Sati depends on.

── Sade Sati is the most-asked question in Indian astrology ──

Saturn transiting the 12th, 1st and 2nd signs from the natal Moon —
roughly seven and a half years, hence the name. People know whether they
are "in Sade Sati" the way they know their sun sign, so getting the dates
right matters more here than almost anywhere else in the engine.

The dates are genuinely awkward to compute because Saturn retrogrades.
Near a sign boundary it can cross forward, turn back, and cross forward
again — three ingresses for one real transition. So the period is bounded
by the FIRST entry into the 12th and the LAST exit from the 2nd, not by
the first crossing found in either direction.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import timedelta

from skyfield.timelib import Time, Timescale

from app.core.ayanamsa import AyanamsaCalculator, AyanamsaSystem
from app.core.constants import (
    HOUSE_COUNT,
    SIGN_ARC,
    SIGNS,
    Planet,
    degree_in_sign,
    normalise_longitude,
    sign_index,
)
from app.core.ephemeris import EphemerisProvider

#: Saturn moves about 0.03-0.13 degrees a day and a sign is 30 degrees,
#: so a five-day scan cannot step over an ingress: the largest single
#: step is about 0.65 degrees.
SCAN_STEP_DAYS = 5.0

#: Bisection target. One minute is far finer than any published Sade Sati
#: date, and the cost is about 13 extra evaluations.
INGRESS_PRECISION = timedelta(minutes=1)

#: How long Saturn must stay outside the stretch for an exit to count as
#: final.
#:
#: Saturn's retrograde arc lasts about 140 days, so a dip back into the
#: 2nd from the Moon always returns well inside a year. The stretch
#: cannot be genuinely re-entered for roughly 22 years, so anything in
#: between separates the two cases cleanly.
SETTLED_DAYS = 400.0

#: Which signs from the natal Moon constitute Sade Sati, and what each
#: stretch is called.
SADE_SATI_PHASES = {
    12: "rising",
    1: "peak",
    2: "setting",
}


@dataclass(frozen=True, slots=True)
class Transit:
    """Where a graha is now, relative to a natal chart."""

    planet: Planet
    longitude: float
    sign: str
    sign_index: int
    degree: float
    is_retrograde: bool

    house_from_moon: int
    """Gochara house, 1-12, counted from the natal Moon's sign. The Vedic
    default."""

    house_from_ascendant: int | None
    """Counted from the natal ascendant. None when the birth time is
    unknown, because there is no ascendant to count from."""


@dataclass(frozen=True, slots=True)
class SadeSatiPhase:
    """One of the three stretches."""

    phase: str
    sign: str
    start_sign_index: int


@dataclass(frozen=True, slots=True)
class SadeSati:
    """Whether Saturn is currently in the seven-and-a-half-year stretch."""

    is_active: bool
    current_phase: str | None
    saturn_sign: str
    moon_sign: str
    houses_from_moon: int


def compute_transits(
    ephemeris: EphemerisProvider,
    ayanamsa: AyanamsaCalculator,
    t: Time,
    natal_moon_sign: int,
    natal_ascendant_sign: int | None = None,
    system: AyanamsaSystem = AyanamsaSystem.LAHIRI,
) -> tuple[Transit, ...]:
    """Current positions, placed against a natal chart.

    `natal_ascendant_sign=None` is the unknown-birth-time case: gochara
    from the Moon still works, because the Moon's sign survives a missing
    birth time, but there is no ascendant to count houses from.
    """
    offset = ayanamsa.at_time(t, system)
    positions = ephemeris.positions(t)

    transits: list[Transit] = []
    for planet, position in positions.items():
        longitude = normalise_longitude(position.longitude - offset)
        index = sign_index(longitude)

        transits.append(
            Transit(
                planet=planet,
                longitude=longitude,
                sign=SIGNS[index],
                sign_index=index,
                degree=degree_in_sign(longitude),
                is_retrograde=position.is_retrograde,
                house_from_moon=(index - natal_moon_sign) % HOUSE_COUNT + 1,
                house_from_ascendant=(
                    (index - natal_ascendant_sign) % HOUSE_COUNT + 1
                    if natal_ascendant_sign is not None
                    else None
                ),
            )
        )

    return tuple(transits)


def sade_sati_at(
    ephemeris: EphemerisProvider,
    ayanamsa: AyanamsaCalculator,
    t: Time,
    natal_moon_sign: int,
    system: AyanamsaSystem = AyanamsaSystem.LAHIRI,
) -> SadeSati:
    """Is Saturn in the 12th, 1st or 2nd from the natal Moon right now?"""
    offset = ayanamsa.at_time(t, system)
    saturn = ephemeris.position_of(Planet.SATURN, t)
    saturn_sign = sign_index(normalise_longitude(saturn.longitude - offset))

    houses_from_moon = (saturn_sign - natal_moon_sign) % HOUSE_COUNT + 1
    phase = SADE_SATI_PHASES.get(houses_from_moon)

    return SadeSati(
        is_active=phase is not None,
        current_phase=phase,
        saturn_sign=SIGNS[saturn_sign],
        moon_sign=SIGNS[natal_moon_sign],
        houses_from_moon=houses_from_moon,
    )


def sade_sati_window_at(
    ephemeris: EphemerisProvider,
    ayanamsa: AyanamsaCalculator,
    timescale: Timescale,
    at: Time,
    natal_moon_sign: int,
    system: AyanamsaSystem = AyanamsaSystem.LAHIRI,
    ingresses: tuple[tuple[Time, int], ...] | None = None,
) -> tuple[Time, Time] | None:
    """The stretch CONTAINING `at`, or None if Saturn is not in it now.

    ── Why this exists alongside `sade_sati_window` ──

    `sade_sati_window` answers "the first stretch inside this window",
    which is the right question when you are scanning history and the
    wrong one when a person asks "when does MINE end". Over a forty-year
    span most Moon signs have a stretch somewhere in the past, and that
    is what the older function returns — dates twenty years stale,
    looking entirely plausible.

    It also cannot tell an ENTRY from a sign change WITHIN the stretch.
    If Saturn is already in the 12th at the start of the span, the first
    ingress found is 12th → 1st, and the reported window is the tail of a
    stretch rather than the whole of one. Both failures produce a short
    window: six months, or three years, where Sade Sati is about seven
    and a half.

    This one starts from `at`, refuses immediately when Saturn is not in
    the stretch, and walks out in both directions from there. There is no
    span for it to pick the wrong stretch out of.

    ── `ingresses` ──

    The ephemeris scan is the whole cost — about five seconds — and it
    does not depend on the Moon sign. Passing a precomputed list lets
    twelve signs share one scan, which is the difference between a
    sixty-eight-second request and a five-second one.
    """
    sade_sati_signs = {
        (natal_moon_sign + offset) % HOUSE_COUNT
        for offset in (-1, 0, 1)  # 12th, 1st, 2nd from the Moon
    }

    # Not in it now, so there is no window to report. Checked first
    # because it is one ephemeris call and it settles most signs.
    if _saturn_sign_at(ephemeris, ayanamsa, at, system) not in sade_sati_signs:
        return None

    if ingresses is None:
        # Wide enough to bracket one stretch either side of `at`: Saturn
        # spends ~2.5 years per sign, so a 7.5-year stretch plus slack
        # fits comfortably in twelve years each way.
        span = 12 * 365.25
        ingresses = find_saturn_ingresses(
            ephemeris,
            ayanamsa,
            timescale,
            timescale.tt_jd(at.tt - span),
            timescale.tt_jd(at.tt + span),
            system,
        )

    before = [(t, sign) for t, sign in ingresses if t.tt <= at.tt]
    after = [(t, sign) for t, sign in ingresses if t.tt > at.tt]

    entry = _walk_back_to_entry(before, sade_sati_signs)
    exit_ = _walk_forward_to_exit(after, sade_sati_signs)

    if entry is None or exit_ is None:
        # One end lies outside the scan. Reporting the other alone would
        # be a window with one edge, and guessing the missing one would
        # be inventing a date somebody plans around.
        return None

    return entry, exit_


def _walk_back_to_entry(
    before: list[tuple[Time, int]],
    sade_sati_signs: set[int],
) -> Time | None:
    """The ingress that began the stretch Saturn is in now.

    ── The mistake this is written against ──

    The obvious version asks, of each in-stretch ingress going backwards,
    "was the last outside ingress long ago?" — and that is true of every
    ingress DEEP INSIDE the stretch, not only of the entry. It reported
    Saturn's move into Aquarius as the start of a Capricorn Moon's Sade
    Sati: a real boundary, but the 12th-to-1st one, five years late. The
    window came out at 2.2 years instead of 8.2.

    ── What it does instead ──

    It finds the most recent REAL boundary before `at` — an outside
    ingress that is not a retrograde dip — and takes the first in-stretch
    ingress after it. That is the entry by construction, because Saturn
    was demonstrably outside immediately before it.

    A retrograde dip is an outside ingress followed by a return to the
    stretch within SETTLED_DAYS. Saturn's retrograde arc is about 140
    days; SETTLED_DAYS is well beyond it and well inside the ~22 years
    before the stretch could genuinely be re-entered.
    """
    boundary_index: int | None = None

    for index in range(len(before) - 1, -1, -1):
        _, sign = before[index]
        if sign in sade_sati_signs:
            continue

        moment, _ = before[index]
        returns = [
            t
            for t, s in before[index + 1 :]
            if s in sade_sati_signs and t.tt - moment.tt <= SETTLED_DAYS
        ]
        if returns:
            # Saturn came straight back: a dip, not a boundary.
            continue

        boundary_index = index
        break

    if boundary_index is None:
        # No sustained outside period in the scan, so the entry is older
        # than the window reaches. Reporting the oldest ingress we happen
        # to have would be a date that looks precise and is not.
        return None

    for moment, sign in before[boundary_index + 1 :]:
        if sign in sade_sati_signs:
            return moment

    # Saturn is inside now but never ingressed after the boundary, which
    # can only happen if the boundary is the last ingress before `at` and
    # Saturn re-entered without a recorded crossing. Not representable.
    return None


def _walk_forward_to_exit(
    after: list[tuple[Time, int]],
    sade_sati_signs: set[int],
) -> Time | None:
    """The exit Saturn does not come back from.

    The same walk `sade_sati_window` does, and the same reasoning: not
    the last exit in the scan, because once Saturn has left, every
    subsequent sign change is also an ingress into a non-stretch sign.
    Not the first, because a retrograde dip back into the 2nd would end
    the period up to nine months early.
    """
    for index, (moment, sign) in enumerate(after):
        if sign in sade_sati_signs:
            continue
        returns = [
            t
            for t, s in after[index + 1 :]
            if s in sade_sati_signs and t.tt - moment.tt <= SETTLED_DAYS
        ]
        if not returns:
            return moment

    return None


def _saturn_sign_at(
    ephemeris: EphemerisProvider,
    ayanamsa: AyanamsaCalculator,
    t: Time,
    system: AyanamsaSystem,
) -> int:
    offset = ayanamsa.at_time(t, system)
    longitude = ephemeris.longitude_of(Planet.SATURN, t)
    return sign_index(normalise_longitude(longitude - offset))


def find_saturn_ingresses(
    ephemeris: EphemerisProvider,
    ayanamsa: AyanamsaCalculator,
    timescale: Timescale,
    start: Time,
    end: Time,
    system: AyanamsaSystem = AyanamsaSystem.LAHIRI,
) -> tuple[tuple[Time, int], ...]:
    """Every moment Saturn changes sign in a window, and the sign entered.

    Scan then bisect. The scan step is sized against Saturn's own motion
    — at most about 0.65 degrees per step against a 30-degree sign, so a
    crossing cannot be stepped over.

    Retrograde crossings are included rather than filtered. Saturn near a
    boundary can cross forward, turn back, and cross again: three real
    ingresses for one apparent transition. Callers that want the
    seven-and-a-half-year envelope take the first and last, which is what
    `sade_sati_window` does — collapsing them here would throw away the
    information needed to do that correctly.
    """
    ingresses: list[tuple[Time, int]] = []

    previous_time = start
    previous_sign = _saturn_sign_at(ephemeris, ayanamsa, start, system)

    day = start.tt
    while day < end.tt:
        day = min(day + SCAN_STEP_DAYS, end.tt)
        current_time = timescale.tt_jd(day)
        current_sign = _saturn_sign_at(ephemeris, ayanamsa, current_time, system)

        if current_sign != previous_sign:
            moment = _bisect_ingress(
                ephemeris, ayanamsa, timescale, previous_time, current_time, previous_sign, system
            )
            ingresses.append((moment, current_sign))

        previous_time, previous_sign = current_time, current_sign

    return tuple(ingresses)


def _bisect_ingress(
    ephemeris: EphemerisProvider,
    ayanamsa: AyanamsaCalculator,
    timescale: Timescale,
    before: Time,
    after: Time,
    sign_before: int,
    system: AyanamsaSystem,
) -> Time:
    """Narrow a known crossing to within INGRESS_PRECISION.

    Bisection rather than a root-finder: the quantity is a step function,
    not a smooth one, so there is no derivative to exploit and nothing to
    converge on except the discontinuity itself.
    """
    precision_days = INGRESS_PRECISION.total_seconds() / 86400.0
    low, high = before.tt, after.tt

    while high - low > precision_days:
        middle = (low + high) / 2
        if _saturn_sign_at(ephemeris, ayanamsa, timescale.tt_jd(middle), system) == sign_before:
            low = middle
        else:
            high = middle

    return timescale.tt_jd(high)


def sade_sati_window(
    ephemeris: EphemerisProvider,
    ayanamsa: AyanamsaCalculator,
    timescale: Timescale,
    start: Time,
    end: Time,
    natal_moon_sign: int,
    system: AyanamsaSystem = AyanamsaSystem.LAHIRI,
) -> tuple[Time, Time] | None:
    """The first entry into the 12th and the last exit from the 2nd.

    Returns None when Saturn never enters the stretch inside the window.

    The end is the exit Saturn does not come back from — which is NOT
    simply the last exit in the scan. Taking the maximum over the whole
    window was the first implementation and it reported 14.27 years for a
    stretch that is supposed to run about 7.5: once Saturn has left, every
    subsequent sign change is also "an ingress into a non-stretch sign",
    so the maximum was just the final sign change of the scan.

    Nor is it the FIRST exit, because a retrograde dip back into the 2nd
    would end the period up to nine months early.

    So the state is walked forward from the first entry, and the period
    closes at the first exit that Saturn stays outside for longer than any
    retrograde loop can last. Saturn's retrograde arc is about 140 days;
    SETTLED_DAYS is comfortably beyond it and comfortably inside the ~22
    years before the stretch could be re-entered.
    """
    sade_sati_signs = {
        (natal_moon_sign + offset) % HOUSE_COUNT
        for offset in (-1, 0, 1)  # 12th, 1st, 2nd from the Moon
    }

    ingresses = find_saturn_ingresses(ephemeris, ayanamsa, timescale, start, end, system)

    entries = [t for t, sign in ingresses if sign in sade_sati_signs]
    if not entries:
        return None

    first_entry = min(entries, key=lambda t: t.tt)

    # Walk forward. The first exit with no re-entry within SETTLED_DAYS
    # is the real end of the stretch.
    later = [(t, sign) for t, sign in ingresses if t.tt > first_entry.tt]

    for index, (moment, sign) in enumerate(later):
        if sign in sade_sati_signs:
            continue
        returns = [
            t
            for t, s in later[index + 1 :]
            if s in sade_sati_signs and t.tt - moment.tt <= SETTLED_DAYS
        ]
        if not returns:
            return first_entry, moment

    # Never settled inside the window: still in the stretch at its end.
    return first_entry, end


def gochara_house(transit_sign: int, natal_sign: int) -> int:
    """Inclusive house count from a natal sign to a transiting one."""
    return (transit_sign - natal_sign) % HOUSE_COUNT + 1


def sign_of(longitude: float) -> int:
    """Exposed for callers that hold a longitude rather than a chart."""
    return int(normalise_longitude(longitude) // SIGN_ARC)
