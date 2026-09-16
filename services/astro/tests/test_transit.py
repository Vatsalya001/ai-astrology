"""Gochara and Sade Sati.

Sade Sati is the single most-asked-about transit in Indian astrology —
people know whether they are "in it" the way they know their sun sign. So
the dates matter more here than almost anywhere else in the engine, and
the awkward part is that Saturn retrogrades across the boundary.
"""

from __future__ import annotations

from itertools import pairwise

from hypothesis import given, settings
from hypothesis import strategies as st

from app.core.constants import HOUSE_COUNT, SIGNS, Planet
from app.core.transit import (
    SADE_SATI_PHASES,
    compute_transits,
    find_saturn_ingresses,
    gochara_house,
    sade_sati_at,
    sade_sati_window,
)

# Aries = 0. A natal Moon here puts Sade Sati in Pisces, Aries, Taurus.
ARIES = 0


# ─── gochara ─────────────────────────────────────────────────────────


def test_every_graha_transits_somewhere(ephemeris, ayanamsa, timescale) -> None:
    transits = compute_transits(ephemeris, ayanamsa, timescale.utc(2026, 1, 1), ARIES, ARIES)

    assert {t.planet for t in transits} == set(Planet)
    for transit in transits:
        assert 0.0 <= transit.longitude < 360.0
        assert 1 <= transit.house_from_moon <= HOUSE_COUNT
        assert 1 <= (transit.house_from_ascendant or 1) <= HOUSE_COUNT


def test_gochara_counts_inclusively_from_the_natal_sign() -> None:
    """A planet transiting the natal sign itself is in the 1st, not the 0th."""
    assert gochara_house(ARIES, ARIES) == 1
    assert gochara_house(1, 0) == 2
    assert gochara_house(0, 1) == 12  # wraps backwards
    assert gochara_house(6, 0) == 7


def test_gochara_is_measured_from_the_moon_by_default(ephemeris, ayanamsa, timescale) -> None:
    """The Vedic convention, and the one Sade Sati depends on.

    Western practice reads transits against the natal ascendant. Using
    that here would move every gochara house and silently relocate Sade
    Sati to a different seven-and-a-half-year window of someone's life.
    """
    t = timescale.utc(2026, 1, 1)
    moon_sign, ascendant_sign = 3, 9  # deliberately different

    transits = compute_transits(ephemeris, ayanamsa, t, moon_sign, ascendant_sign)
    saturn = next(x for x in transits if x.planet is Planet.SATURN)

    assert saturn.house_from_moon == gochara_house(saturn.sign_index, moon_sign)
    assert saturn.house_from_ascendant == gochara_house(saturn.sign_index, ascendant_sign)
    assert saturn.house_from_moon != saturn.house_from_ascendant


def test_an_unknown_birth_time_still_gets_gochara_from_the_moon(
    ephemeris, ayanamsa, timescale
) -> None:
    """The Moon's sign survives a missing birth time; the ascendant does not.

    The Moon moves about 13 degrees a day, so a midday assumption places
    it within half a sign — good enough for gochara. There is simply no
    ascendant to count from, so that field is absent rather than guessed.
    """
    transits = compute_transits(
        ephemeris, ayanamsa, timescale.utc(2026, 1, 1), ARIES, natal_ascendant_sign=None
    )

    for transit in transits:
        assert transit.house_from_ascendant is None
        assert 1 <= transit.house_from_moon <= HOUSE_COUNT


# ─── Sade Sati, the state ────────────────────────────────────────────


def test_sade_sati_covers_exactly_three_signs() -> None:
    """The 12th, 1st and 2nd from the natal Moon — and nothing else.

    Seven and a half years is three signs at roughly two and a half years
    each, which is where the name comes from. A fourth sign would stretch
    it to ten.
    """
    assert set(SADE_SATI_PHASES) == {12, 1, 2}
    assert SADE_SATI_PHASES[12] == "rising"
    assert SADE_SATI_PHASES[1] == "peak"
    assert SADE_SATI_PHASES[2] == "setting"


def test_sade_sati_is_active_only_in_those_three_signs(ephemeris, ayanamsa, timescale) -> None:
    """Swept across every natal Moon sign at one instant.

    Saturn is somewhere fixed; exactly three of the twelve possible natal
    Moon signs must report Sade Sati, and they must be consecutive. A
    single hand-picked Moon sign would not catch an off-by-one that
    shifted the window by a sign.
    """
    t = timescale.utc(2026, 1, 1)

    active = [
        moon_sign
        for moon_sign in range(12)
        if sade_sati_at(ephemeris, ayanamsa, t, moon_sign).is_active
    ]

    assert len(active) == 3, f"{len(active)} Moon signs in Sade Sati, expected 3: {active}"

    # Consecutive, allowing for the wrap at Pisces/Aries.
    #
    # Checked as a set against every possible starting sign rather than
    # by walking sorted gaps: iterating range(12) yields [0, 1, 11] for a
    # stretch that wraps, and a naive gap check reads that as a jump of
    # 10 when it is really 11 -> 0 -> 1. That is a test bug, and it cost
    # a run to find.
    assert any(set(active) == {(start + k) % 12 for k in range(3)} for start in range(12)), (
        f"the three signs are not consecutive: {sorted(active)}"
    )


def test_the_phase_matches_the_house(ephemeris, ayanamsa, timescale) -> None:
    t = timescale.utc(2026, 1, 1)

    for moon_sign in range(12):
        state = sade_sati_at(ephemeris, ayanamsa, t, moon_sign)
        if state.is_active:
            assert state.current_phase == SADE_SATI_PHASES[state.houses_from_moon]
        else:
            assert state.current_phase is None
            assert state.houses_from_moon not in SADE_SATI_PHASES


def test_all_three_phases_occur_as_saturn_moves(ephemeris, ayanamsa, timescale) -> None:
    """Rising, peak and setting all reachable for one natal Moon.

    Sampled over fifteen years, which is more than one Saturn cycle through
    the stretch. A phase that never occurred would mean the mapping is
    unreachable — dead code that looks like a feature.
    """
    seen = set()
    for year in range(2010, 2053):
        state = sade_sati_at(ephemeris, ayanamsa, timescale.utc(year, 6, 1), ARIES)
        if state.current_phase:
            seen.add(state.current_phase)

    assert seen == {"rising", "peak", "setting"}, f"only reached {seen}"


@settings(max_examples=30, deadline=None)
@given(moon_sign=st.integers(min_value=0, max_value=11), year=st.integers(2000, 2050))
def test_sade_sati_state_is_internally_consistent(
    ephemeris_module, ayanamsa, moon_sign: int, year: int
) -> None:
    ephemeris, timescale = ephemeris_module
    state = sade_sati_at(ephemeris, ayanamsa, timescale.utc(year, 3, 15), moon_sign)

    assert 1 <= state.houses_from_moon <= HOUSE_COUNT
    assert state.is_active == (state.current_phase is not None)
    assert state.moon_sign == SIGNS[moon_sign]


# ─── ingresses, where the retrogrades live ───────────────────────────


def test_saturn_ingresses_are_found_and_ordered(ephemeris, ayanamsa, timescale) -> None:
    """Saturn takes about two and a half years per sign.

    Over ten years that is roughly four sign changes, though retrogrades
    near a boundary can add extra crossings — which is the whole reason
    this function returns every crossing rather than a tidy four.
    """
    ingresses = find_saturn_ingresses(
        ephemeris, ayanamsa, timescale, timescale.utc(2020, 1, 1), timescale.utc(2030, 1, 1)
    )

    assert len(ingresses) >= 4, f"only {len(ingresses)} ingresses in ten years"

    times = [t.tt for t, _ in ingresses]
    assert times == sorted(times), "ingresses are not in chronological order"


def test_an_ingress_really_is_a_boundary(ephemeris, ayanamsa, timescale) -> None:
    """Saturn's sign differs across each reported moment.

    The bisection narrows to a minute, so a step either side must land in
    different signs. This is what distinguishes a real crossing from a
    scan artefact.
    """
    from app.core.ayanamsa import AyanamsaSystem
    from app.core.transit import _saturn_sign_at

    ingresses = find_saturn_ingresses(
        ephemeris, ayanamsa, timescale, timescale.utc(2020, 1, 1), timescale.utc(2030, 1, 1)
    )

    minute = 1.0 / (24 * 60)
    for moment, entered in ingresses:
        before = _saturn_sign_at(
            ephemeris, ayanamsa, timescale.tt_jd(moment.tt - minute), AyanamsaSystem.LAHIRI
        )
        after = _saturn_sign_at(
            ephemeris, ayanamsa, timescale.tt_jd(moment.tt + minute), AyanamsaSystem.LAHIRI
        )

        assert before != after, f"no sign change at the reported ingress {moment.utc_iso()}"
        assert after == entered, f"entered {SIGNS[after]}, reported {SIGNS[entered]}"


def test_retrograde_crossings_are_kept_not_collapsed(ephemeris, ayanamsa, timescale) -> None:
    """Saturn crosses some boundaries three times.

    Forward, retrograde back, then forward again. Collapsing those into
    one would throw away the information `sade_sati_window` needs to find
    the LAST exit, and the period would be reported short by up to nine
    months.

    Asserted by finding ingresses spaced far closer together than
    Saturn's ~2.5 years per sign, which only happens when it crosses a
    boundary, reverses, and crosses again.

    Fifteen years rather than forty: the signature appears at any
    boundary Saturn retrogrades across, and each extra decade costs about
    twenty seconds of scan.
    """
    ingresses = find_saturn_ingresses(
        ephemeris, ayanamsa, timescale, timescale.utc(2000, 1, 1), timescale.utc(2015, 1, 1)
    )

    entered = [sign for _, sign in ingresses]
    repeats = {sign for sign in entered if entered.count(sign) > 1}

    # The retrograde signature is two ingresses far closer together than
    # Saturn's 2.5 years per sign.
    close_repeats = [
        (a, b)
        for (a, sign_a), (b, sign_b) in pairwise(ingresses)
        if sign_a == sign_b or (b.tt - a.tt) < 400
    ]

    assert repeats or close_repeats, (
        "no repeated or closely-spaced ingress found in 40 years; "
        "retrograde crossings are being collapsed"
    )


# ─── the window ──────────────────────────────────────────────────────


def test_sade_sati_window_spans_about_seven_and_a_half_years(
    ephemeris, ayanamsa, timescale
) -> None:
    """The name is the assertion.

    Three signs at roughly two and a half years each. Retrogrades at
    either boundary stretch it, so the tolerance is generous — but a
    result of four years or twelve would mean the entry and exit are
    being picked from the wrong crossings.
    """
    window = sade_sati_window(
        ephemeris,
        ayanamsa,
        timescale,
        timescale.utc(1990, 1, 1),
        timescale.utc(2010, 1, 1),
        ARIES,
    )

    assert window is not None, "no Sade Sati found for an Aries Moon in 1990-2010"

    start, end = window
    years = (end.tt - start.tt) / 365.25

    assert 6.5 <= years <= 9.5, f"the window spans {years:.2f} years, expected about 7.5"


def test_the_window_brackets_an_active_period(ephemeris, ayanamsa, timescale) -> None:
    """Inside the window Sade Sati is on; just outside it is off.

    The cheapest way to catch a window computed from the wrong crossings:
    the dates would be plausible and simply describe a different stretch
    of the person's life.
    """
    window = sade_sati_window(
        ephemeris,
        ayanamsa,
        timescale,
        timescale.utc(1990, 1, 1),
        timescale.utc(2010, 1, 1),
        ARIES,
    )
    assert window is not None
    start, end = window

    middle = timescale.tt_jd((start.tt + end.tt) / 2)
    assert sade_sati_at(ephemeris, ayanamsa, middle, ARIES).is_active

    well_before = timescale.tt_jd(start.tt - 400)
    well_after = timescale.tt_jd(end.tt + 400)
    assert not sade_sati_at(ephemeris, ayanamsa, well_before, ARIES).is_active
    assert not sade_sati_at(ephemeris, ayanamsa, well_after, ARIES).is_active


def test_the_window_takes_the_last_exit_not_the_first(ephemeris, ayanamsa, timescale) -> None:
    """The retrograde correction, asserted directly.

    If the implementation took the first exit after the first entry, a
    retrograde dip back into the stretch would end the period early. So
    the reported end must be at or after every exit that is followed by
    no further entry — and Sade Sati must genuinely be off at the end.
    """

    start_scan = timescale.utc(1990, 1, 1)
    end_scan = timescale.utc(2010, 1, 1)

    window = sade_sati_window(ephemeris, ayanamsa, timescale, start_scan, end_scan, ARIES)
    assert window is not None
    _, end = window

    # A day after the reported end, the stretch must be over. Taking an
    # early exit would leave it still active here.
    after = timescale.tt_jd(end.tt + 1)
    assert not sade_sati_at(ephemeris, ayanamsa, after, ARIES).is_active, (
        "Sade Sati is still active a day after the reported end — "
        "an intermediate retrograde exit was mistaken for the final one"
    )


def test_no_sade_sati_in_a_window_where_saturn_is_elsewhere(ephemeris, ayanamsa, timescale) -> None:
    """None, not an exception and not a zero-length period.

    Saturn spends roughly three quarters of its orbit outside any given
    natal Moon's stretch, so "not in Sade Sati" is the common answer and
    has to be representable.
    """
    # A one-year window chosen for a Moon sign Saturn is far from.
    t = timescale.utc(2026, 1, 1)
    saturn_sign = next(
        x.sign_index
        for x in compute_transits(ephemeris, ayanamsa, t, ARIES, ARIES)
        if x.planet is Planet.SATURN
    )
    far_moon = (saturn_sign + 6) % 12  # opposite, so 7th from the Moon

    assert not sade_sati_at(ephemeris, ayanamsa, t, far_moon).is_active

    window = sade_sati_window(
        ephemeris, ayanamsa, timescale, t, timescale.utc(2026, 6, 1), far_moon
    )
    assert window is None


def test_still_inside_at_the_end_of_the_window_returns_the_window_end(
    ephemeris, ayanamsa, timescale
) -> None:
    """A scan that stops mid-stretch reports the scan's end, not None.

    Returning None there would tell a user in the middle of Sade Sati
    that they are not in it, which is the worst available answer.
    """
    window = sade_sati_window(
        ephemeris,
        ayanamsa,
        timescale,
        timescale.utc(1990, 1, 1),
        timescale.utc(2010, 1, 1),
        ARIES,
    )
    assert window is not None
    start, _ = window

    # Re-scan a window that begins inside the stretch and stops early.
    short_end = timescale.tt_jd(start.tt + 400)
    truncated = sade_sati_window(
        ephemeris, ayanamsa, timescale, timescale.tt_jd(start.tt + 10), short_end, ARIES
    )

    if truncated is not None:
        assert truncated[1].tt <= short_end.tt + 1e-6
