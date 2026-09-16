"""The Vimshottari tree.

The spec names this as the place float error does its damage, and says
what to assert: the mahadashas sum to 120 years for ANY Moon longitude,
and every sub-sequence sums exactly to its parent's span.

Those properties are the whole point. A dasha date wrong by three days
looks completely plausible in a UI, so nothing but an exact invariant
will catch it.
"""

from __future__ import annotations

from datetime import UTC, datetime, timedelta
from itertools import pairwise

import pytest
from hypothesis import given, settings
from hypothesis import strategies as st

from app.core.constants import VIMSHOTTARI_SEQUENCE, VIMSHOTTARI_TOTAL_YEARS, Planet
from app.core.dasha import (
    MAX_LEVEL,
    YEAR_DAYS,
    build_vimshottari,
    elapsed_fraction,
    find_active,
    nakshatra_lord,
)

BIRTH = datetime(1994, 8, 17, 9, 5, tzinfo=UTC)

#: Every boundary is rounded to the nearest microsecond exactly once, so
#: a whole 120-year cycle can drift by at most a few of them.
MICROSECOND_SLACK = timedelta(microseconds=10)


# ─── the sequence itself ─────────────────────────────────────────────


def test_the_sequence_totals_one_hundred_and_twenty_years() -> None:
    """The constant the whole system is named for.

    Vimshottari means "one hundred and twenty". If this were wrong every
    downstream property would still hold internally and every date would
    be wrong.
    """
    assert sum(years for _, years in VIMSHOTTARI_SEQUENCE) == VIMSHOTTARI_TOTAL_YEARS
    assert len(VIMSHOTTARI_SEQUENCE) == 9
    assert len({planet for planet, _ in VIMSHOTTARI_SEQUENCE}) == 9


def test_nakshatra_lords_cycle_three_times() -> None:
    """27 nakshatras, 9 lords, three full cycles.

    Ashwini, Magha and Mula all belong to Ketu — the property that makes
    `nakshatra_index % 9` correct rather than coincidental.
    """
    from app.core.constants import NAKSHATRA_ARC

    lords = [nakshatra_lord(n * NAKSHATRA_ARC + 1.0) for n in range(27)]

    assert lords[0:9] == lords[9:18] == lords[18:27]
    assert lords[0] is Planet.KETU  # Ashwini
    assert lords[9] is Planet.KETU  # Magha
    assert lords[18] is Planet.KETU  # Mula


# ─── the spec's properties ───────────────────────────────────────────


@settings(max_examples=150, deadline=None)
@given(moon=st.floats(min_value=0, max_value=360, exclude_max=True))
def test_mahadashas_sum_to_one_hundred_and_twenty_years(moon: float) -> None:
    """The spec's headline property, for ANY Moon longitude.

    Any longitude, because the failure would live in the first partial
    period — whose length depends continuously on where the Moon sits —
    and hand-picked examples never land near a nakshatra cusp.
    """
    tree = build_vimshottari(moon, BIRTH, max_level=1)

    total = sum((period.duration for period in tree), timedelta())
    expected = timedelta(days=float(YEAR_DAYS * VIMSHOTTARI_TOTAL_YEARS))

    assert abs(total - expected) < MICROSECOND_SLACK, (
        f"the cycle spans {total.days} days, expected {expected.days}"
    )


@settings(max_examples=40, deadline=None)
@given(moon=st.floats(min_value=0, max_value=360, exclude_max=True))
def test_every_sub_sequence_tiles_its_parent_exactly(moon: float) -> None:
    """The spec's property at all three levels.

    'Tiles' is stronger than 'sums to': no gaps, no overlaps, and the
    first and last children flush against the parent's own boundaries.
    Summing durations alone would pass even if the children were all
    shifted by a day.

    The cumulative-boundary algorithm makes this true by construction.
    Summing independent durations was MEASURED to give the same answer —
    zero drift — so this test does not currently distinguish the two, and
    the module docstring says so rather than implying otherwise. What it
    does catch is any change that breaks the tiling: a different year
    length, a fourth level, or a resolution change.
    """
    for mahadasha in build_vimshottari(moon, BIRTH):
        _assert_tiles(mahadasha)


def _assert_tiles(period) -> None:
    if not period.children:
        return

    assert period.children[0].start == period.start, "first child does not start with its parent"
    assert period.children[-1].end == period.end, "last child does not end with its parent"

    for left, right in pairwise(period.children):
        assert left.end == right.start, "a gap or overlap between siblings"

    for child in period.children:
        assert child.end > child.start, "a zero-length or inverted period"
        _assert_tiles(child)


@settings(max_examples=40, deadline=None)
@given(moon=st.floats(min_value=0, max_value=360, exclude_max=True))
def test_every_level_has_all_nine_lords_in_order(moon: float) -> None:
    """Each sub-sequence begins with its own parent's lord.

    The Venus mahadasha opens with a Venus antardasha, and so on. An
    off-by-one in the rotation would shift every sub-period's lord while
    leaving every duration property intact.
    """
    planets = [p for p, _ in VIMSHOTTARI_SEQUENCE]

    for mahadasha in build_vimshottari(moon, BIRTH):
        assert len(mahadasha.children) == 9
        assert mahadasha.children[0].planet is mahadasha.planet

        expected = (
            planets[planets.index(mahadasha.planet) :] + planets[: planets.index(mahadasha.planet)]
        )
        assert [c.planet for c in mahadasha.children] == expected

        for antardasha in mahadasha.children:
            assert antardasha.children[0].planet is antardasha.planet


def test_sub_period_lengths_are_proportional(moon_at_ashwini_start=0.0) -> None:
    """A lord's share of any parent is its years over 120.

    Venus takes 20/120 of every period it appears in, at every level.
    This is what makes the system self-similar, and an implementation
    that divided evenly into nine would pass the tiling test while being
    completely wrong.
    """
    tree = build_vimshottari(moon_at_ashwini_start, BIRTH)
    mahadasha = tree[0]

    for antardasha in mahadasha.children:
        years = next(y for p, y in VIMSHOTTARI_SEQUENCE if p is antardasha.planet)
        expected = mahadasha.duration * years / VIMSHOTTARI_TOTAL_YEARS

        assert abs(antardasha.duration - expected) < timedelta(seconds=1), (
            f"{antardasha.planet} antardasha in the {mahadasha.planet} mahadasha "
            f"runs {antardasha.duration}, expected {expected}"
        )


# ─── the balance at birth ────────────────────────────────────────────


def test_a_moon_at_the_start_of_a_nakshatra_gets_a_full_mahadasha() -> None:
    """Nothing elapsed, so the first period starts exactly at birth."""
    tree = build_vimshottari(0.0, BIRTH)  # 0° = start of Ashwini, Ketu

    assert tree[0].planet is Planet.KETU
    assert abs(tree[0].start - BIRTH) < MICROSECOND_SLACK
    assert abs(tree[0].duration - timedelta(days=float(YEAR_DAYS * 7))) < timedelta(seconds=1)


def test_a_moon_halfway_through_a_nakshatra_gets_half_a_mahadasha() -> None:
    """The balance is proportional, and the period began before birth.

    The first mahadasha notionally starts in a previous life — the
    tradition's own framing — which is why the tree is built from that
    notional start rather than clamped to the birth instant.
    """
    from app.core.constants import NAKSHATRA_ARC

    tree = build_vimshottari(NAKSHATRA_ARC / 2, BIRTH)  # halfway through Ashwini
    first = tree[0]

    assert first.planet is Planet.KETU
    assert first.start < BIRTH, "the first mahadasha should predate the birth"

    balance = first.end - BIRTH
    full = timedelta(days=float(YEAR_DAYS * 7))

    assert abs(balance - full / 2) < timedelta(seconds=1), (
        f"balance is {balance}, expected half of {full}"
    )


@settings(max_examples=100, deadline=None)
@given(moon=st.floats(min_value=0, max_value=360, exclude_max=True))
def test_the_elapsed_fraction_is_always_a_proper_fraction(moon: float) -> None:
    fraction = elapsed_fraction(moon)
    assert 0 <= fraction < 1


@settings(max_examples=60, deadline=None)
@given(moon=st.floats(min_value=0, max_value=360, exclude_max=True))
def test_birth_always_falls_inside_the_first_mahadasha(moon: float) -> None:
    """Whatever the Moon's position.

    If the birth landed outside it, the balance would be negative or the
    person would already be in their second period at birth — both
    impossible, and both the kind of off-by-one the partial first period
    invites.
    """
    first = build_vimshottari(moon, BIRTH, max_level=1)[0]
    assert first.active_at(BIRTH), f"birth outside the first mahadasha for moon={moon}"


# ─── querying ────────────────────────────────────────────────────────


def test_find_active_returns_one_period_per_level() -> None:
    tree = build_vimshottari(123.456, BIRTH)
    chain = find_active(tree, BIRTH + timedelta(days=4000))

    assert len(chain) == MAX_LEVEL
    assert [p.level for p in chain] == [1, 2, 3]

    # Each is nested inside the one before it.
    for outer, inner in pairwise(chain):
        assert outer.start <= inner.start
        assert inner.end <= outer.end


def test_exactly_one_period_is_active_at_any_instant() -> None:
    """Half-open intervals, checked at the boundaries themselves.

    One period's end is the next one's start. With closed intervals both
    would claim the boundary instant, and "which dasha am I in" would
    have two answers on exactly the days people ask about.
    """
    tree = build_vimshottari(123.456, BIRTH)

    for mahadasha in tree[:3]:
        for boundary in (mahadasha.start, mahadasha.end - timedelta(microseconds=1)):
            active = [m for m in tree if m.active_at(boundary)]
            assert len(active) == 1, f"{len(active)} mahadashas active at {boundary}"


def test_an_instant_past_the_cycle_has_no_active_period() -> None:
    """A real answer, not an error.

    Someone living past 120 has run out of tree. Returning an empty chain
    says so; raising would make an unremarkable edge case look like a
    bug.
    """
    tree = build_vimshottari(0.0, BIRTH)
    assert find_active(tree, BIRTH + timedelta(days=365.25 * 121)) == []


# ─── the guards ──────────────────────────────────────────────────────


def test_a_naive_birth_datetime_is_refused() -> None:
    with pytest.raises(ValueError, match="aware datetime"):
        build_vimshottari(0.0, datetime(1994, 8, 17, 9, 5))


def test_an_out_of_range_level_is_refused() -> None:
    for level in (-1, 4, 99):
        with pytest.raises(ValueError, match="max_level"):
            build_vimshottari(0.0, BIRTH, max_level=level)


def test_the_tree_is_not_built_deeper_than_asked() -> None:
    """Each level multiplies the node count by nine.

    A fourth level would put 6,561 rows behind a single chart, for a
    precision no reading uses.
    """
    assert all(not m.children for m in build_vimshottari(0.0, BIRTH, max_level=1))

    two = build_vimshottari(0.0, BIRTH, max_level=2)
    assert all(c.children == () for m in two for c in m.children)

    three = build_vimshottari(0.0, BIRTH, max_level=3)
    assert sum(1 for m in three for a in m.children for _ in a.children) == 9 * 9 * 9


def test_the_cycle_still_fits_float64_exactly() -> None:
    """The margin that justifies Decimal, pinned as a number.

    Float64 represents integers exactly only up to 2**53. The whole
    120-year cycle at microsecond resolution is 3.79e15 microseconds,
    which fits with a factor of about 2.4 to spare.

    That margin is the real reason this module uses Decimal — not a
    measured float bug, because at this resolution there isn't one (see
    the module docstring, which records the measurement).

    This test exists so the margin is checked rather than assumed. If
    someone moves to nanoseconds, lengthens the cycle, or changes
    YEAR_DAYS to something that is not a clean multiple, they find out
    here instead of discovering drifted dasha dates months later.
    """
    cycle_microseconds = float(YEAR_DAYS) * VIMSHOTTARI_TOTAL_YEARS * 86_400 * 1e6

    assert cycle_microseconds < 2**53, (
        f"the cycle is {cycle_microseconds:.3e} microseconds, past float64's "
        f"exact-integer limit of {2**53:.3e}. Decimal is now load-bearing "
        "rather than precautionary — and any float path in this module is a bug."
    )

    headroom = 2**53 / cycle_microseconds
    assert headroom > 2, f"only {headroom:.1f}x headroom left; the resolution is too fine"
