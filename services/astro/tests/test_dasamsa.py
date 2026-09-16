"""The D10 chart, and the trick that does not work for it.

Dasamsa is the divisional chart read for career and profession. Its rule
is stated by parity rather than by element:

    odd signs   the ten dasamsas are reckoned from the SAME sign
    even signs  they are reckoned from the NINTH sign therefrom

"Odd" is the traditional 1-based numbering — Aries is the 1st sign and so
odd — which is `sign_index % 2 == 0` in code. That inversion is the first
thing to get wrong, and getting it wrong swaps every dasamsa in the chart
while still producing a complete, plausible D10.

The second thing to get wrong is assuming the navamsa's shortcut carries
over. It does not, and `test_the_continuous_count_trick_does_not_work`
measures by how much.

No external astronomical event can validate a divisional chart — a D10 is
a convention applied to a longitude, not something anybody can observe.
So what is checked here is the convention itself, enumerated in full
against an independent statement of the classical rule.
"""

from __future__ import annotations

import pytest
from hypothesis import given, settings
from hypothesis import strategies as st

from app.core.chart import dasamsa_longitude
from app.core.constants import (
    DASAMSA_COUNT,
    DASAMSAS_PER_SIGN,
    FULL_CIRCLE,
    SIGN_ARC,
    SIGN_COUNT,
    SIGNS,
    degree_in_sign,
    sign_index,
)

DASAMSA_ARC = SIGN_ARC / DASAMSAS_PER_SIGN  # 3°


def classical_target(sign: int, dasamsa: int) -> int:
    """The rule, written out independently of the implementation.

    Deliberately a separate expression of the same rule rather than a
    call into `dasamsa_longitude`. Comparing the implementation with
    itself proves nothing; this is the reference the enumeration below
    checks against.
    """
    start = sign if sign % 2 == 0 else (sign + 8) % SIGN_COUNT
    return (start + dasamsa) % SIGN_COUNT


# ─── the rule, enumerated ────────────────────────────────────────────


def test_every_one_of_the_120_dasamsas_follows_the_classical_rule() -> None:
    """All 120, not a sample.

    There are only 120, each is a fixed convention, and a spot check
    would miss exactly the sign whose parity was handled wrongly.
    """
    wrong: list[str] = []

    for sign in range(SIGN_COUNT):
        for dasamsa in range(DASAMSAS_PER_SIGN):
            # A point in the middle of the slice, so the test is about
            # the mapping and not about boundary rounding.
            longitude = sign * SIGN_ARC + dasamsa * DASAMSA_ARC + DASAMSA_ARC / 2
            actual = sign_index(dasamsa_longitude(longitude))
            expected = classical_target(sign, dasamsa)
            if actual != expected:
                wrong.append(
                    f"{SIGNS[sign]} dasamsa {dasamsa + 1} -> {SIGNS[actual]}, "
                    f"classical says {SIGNS[expected]}"
                )

    assert not wrong, f"{len(wrong)} of {DASAMSA_COUNT} dasamsas are wrong:\n" + "\n".join(
        wrong[:10]
    )


@pytest.mark.parametrize(
    ("sign_name", "sign", "expected_first"),
    [
        # An odd sign reckons from itself.
        ("Aries", 0, "Aries"),
        ("Gemini", 2, "Gemini"),
        ("Aquarius", 10, "Aquarius"),
        # An even sign reckons from the ninth therefrom.
        ("Taurus", 1, "Capricorn"),
        ("Cancer", 3, "Pisces"),
        ("Pisces", 11, "Scorpio"),
    ],
)
def test_the_first_dasamsa_of_each_parity(sign_name: str, sign: int, expected_first: str) -> None:
    """Worked examples, spelled out.

    The enumeration above would catch these too. They are here because a
    failing enumeration says "90 of 120 are wrong" and these say which
    rule was misread — parity inverted, or the ninth counted
    exclusively.
    """
    longitude = sign * SIGN_ARC + 0.5
    assert SIGNS[sign_index(dasamsa_longitude(longitude))] == expected_first, (
        f"the first dasamsa of {sign_name}"
    )


def test_the_ninth_sign_is_counted_inclusively() -> None:
    """S + 8, not S + 9.

    "The ninth therefrom" counts the sign itself as the first, which is
    the same inclusive convention as houses. Counting exclusively puts
    every even sign's dasamsas one sign late, and the result still looks
    like a chart.
    """
    # Taurus is index 1. Inclusive: Taurus(1st) ... Capricorn(9th) = 9.
    assert classical_target(1, 0) == 9
    assert SIGNS[9] == "Capricorn"
    assert SIGNS[sign_index(dasamsa_longitude(SIGN_ARC + 0.5))] == "Capricorn"


# ─── the trap ────────────────────────────────────────────────────────


def test_the_continuous_count_trick_does_not_work_for_d10() -> None:
    """Why D10 cannot be written the way D9 is.

    The navamsa collapses into one unbroken walk of the zodiac, because
    108 navamsas over 12 signs is nine whole cycles. Ten divisions is
    not a whole number of cycles, so the same expression is wrong — and
    wrong for most of the chart, not at an edge.

    Measured rather than asserted from memory, because the whole reason
    this test exists is that the trick is tempting and the failure looks
    like a working chart.
    """
    disagreements = [
        (sign, dasamsa)
        for sign in range(SIGN_COUNT)
        for dasamsa in range(DASAMSAS_PER_SIGN)
        if classical_target(sign, dasamsa) != (sign * DASAMSAS_PER_SIGN + dasamsa) % SIGN_COUNT
    ]

    assert len(disagreements) == 90, (
        f"the continuous count disagrees with the classical rule on "
        f"{len(disagreements)} of {DASAMSA_COUNT} dasamsas; the docstring in "
        "dasamsa_longitude records 90. If this number changed, one of the two "
        "expressions changed and the docstring is now wrong."
    )

    # And name one, so a reader can check it by hand.
    assert (1, 0) in disagreements, "Taurus's first dasamsa is the worked example"


# ─── properties ──────────────────────────────────────────────────────


@settings(max_examples=300, deadline=None)
@given(longitude=st.floats(min_value=0.0, max_value=360.0, exclude_max=True))
def test_the_result_is_always_a_valid_longitude(longitude: float) -> None:
    result = dasamsa_longitude(longitude)
    assert 0.0 <= result < FULL_CIRCLE
    assert 0 <= sign_index(result) < SIGN_COUNT


@settings(max_examples=300, deadline=None)
@given(longitude=st.floats(min_value=0.0, max_value=360.0, exclude_max=True))
def test_a_three_degree_slice_fills_a_whole_sign(longitude: float) -> None:
    """The expansion, as a property.

    A dasamsa is 3° of the rasi and occupies all 30° of its destination.
    Forgetting the expansion leaves every planet in the first three
    degrees of its D10 sign — which looks like a chart where everything
    is clustered at the start of a sign, and is easy to mistake for a
    real configuration.
    """
    degree = degree_in_sign(dasamsa_longitude(longitude))
    assert 0.0 <= degree < SIGN_ARC


def test_the_expansion_spans_the_full_sign() -> None:
    """The other half: the degrees must actually reach both ends.

    The property above holds for a broken implementation that always
    returns 0. This walks one dasamsa and requires the output to sweep
    nearly the whole destination sign.
    """
    start = dasamsa_longitude(0.0)
    near_end = dasamsa_longitude(DASAMSA_ARC - 1e-9)

    assert degree_in_sign(start) == pytest.approx(0.0, abs=1e-6)
    assert degree_in_sign(near_end) == pytest.approx(SIGN_ARC, abs=1e-6)


def test_each_dasamsa_maps_onto_a_distinct_destination_within_a_sign() -> None:
    """The ten dasamsas of one sign land in ten different signs.

    They are consecutive by construction, so a repeat means the rule
    stopped advancing — which would put a whole sign's worth of planets
    in one D10 sign.
    """
    for sign in range(SIGN_COUNT):
        targets = {
            sign_index(dasamsa_longitude(sign * SIGN_ARC + d * DASAMSA_ARC + 0.5))
            for d in range(DASAMSAS_PER_SIGN)
        }
        assert len(targets) == DASAMSAS_PER_SIGN, f"{SIGNS[sign]} repeats a destination"


def test_a_boundary_longitude_belongs_to_the_dasamsa_it_starts() -> None:
    """Exactly 3.0° is the start of the second dasamsa, not the end of the first.

    The same half-open convention as every other subdivision here, and
    the same float trap: dividing before multiplying puts some of these
    boundaries in the previous slice.
    """
    first = sign_index(dasamsa_longitude(0.0))
    second = sign_index(dasamsa_longitude(DASAMSA_ARC))
    assert first != second
    assert second == classical_target(0, 1)
