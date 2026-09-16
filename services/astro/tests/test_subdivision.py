"""The arc-division primitive, and two bugs it was hiding.

`subdivision_index` is one line of arithmetic under every nakshatra,
every pada, every sign and every varga in the product. It shipped with a
docstring explaining a correctness fix and **no test at all**.

Writing this file found three things, in order:

1. Reintroducing the old form left the entire golden suite green. A
   golden fixture is a planet at a real instant, and a real planet is
   never within one ulp of a boundary — only enumerating boundaries
   finds this class of bug.

2. The docstring's justification was wrong in both halves. It claimed
   nine of twenty-seven nakshatra boundaries failed under the old form;
   the real number is three. And measured against exact arithmetic, the
   "fixed" multiply-first ordering is wrong MORE often than the form it
   replaced, not less. `test_neither_float_ordering_is_exact` is that
   measurement.

3. `normalise_longitude` could return exactly 360.0 — for a tiny
   negative input the true answer is a hair under 360 and the nearest
   float is 360.0 — which produced sign index 12. That is the "house 13"
   its own docstring promises to prevent, in the one line meant to
   prevent it. `test_a_tiny_negative_angle_folds_to_zero` covers it.

None of this is reachable by a real chart. The reason to fix it anyway
is that the alternative was three comments asserting properties nothing
checked.
"""

from __future__ import annotations

import math
import random
from fractions import Fraction

import pytest

from app.core.constants import (
    FULL_CIRCLE,
    NAKSHATRA_COUNT,
    PADA_COUNT,
    SIGN_COUNT,
    normalise_longitude,
    subdivision_index,
)


def exact_index(longitude: float, divisions: int) -> int:
    """The answer, with no floating-point arithmetic anywhere.

    A float is a rational number, and `Fraction` holds its exact value,
    so this is the truth every other form is compared against rather than
    another approximation with a different rounding story.
    """
    return math.floor(Fraction(normalise_longitude(longitude)) * divisions / 360)


def multiply_first(longitude: float, divisions: int) -> int:
    """The form this module used to use."""
    return int(normalise_longitude(longitude) * divisions / FULL_CIRCLE)


def divide_first(longitude: float, divisions: int) -> int:
    """The form it used before that."""
    return int(normalise_longitude(longitude) / (FULL_CIRCLE / divisions))


DIVISIONS = [
    pytest.param(SIGN_COUNT, id="signs"),
    pytest.param(NAKSHATRA_COUNT, id="nakshatras"),
    pytest.param(PADA_COUNT, id="padas"),
    pytest.param(9, id="navamsa"),
    pytest.param(30, id="trimsamsa"),
    pytest.param(60, id="shashtiamsa"),
]


def boundary_neighbourhood(divisions: int) -> list[float]:
    """Every arc boundary, plus the float on each side of it.

    Three values per boundary. The boundary itself is usually not
    representable, so the two neighbours are where a real disagreement
    shows up — and where every form other than exact arithmetic has been
    observed to get it wrong.
    """
    values: list[float] = []
    for k in range(divisions):
        boundary = k * FULL_CIRCLE / divisions
        values.append(math.nextafter(boundary, -math.inf))
        values.append(boundary)
        values.append(math.nextafter(boundary, math.inf))
    return values


@pytest.mark.parametrize("divisions", DIVISIONS)
def test_it_is_exact_at_every_boundary(divisions: int) -> None:
    """The property, stated as the thing it actually has to satisfy.

    Not "the k-th boundary gives k" — that is false, because `k * 360/d`
    is usually a float slightly BELOW the true boundary and therefore
    genuinely belongs to arc k-1. The property is that the answer matches
    exact arithmetic on the float that was actually passed in.
    """
    wrong = [
        (value, subdivision_index(value, divisions), exact_index(value, divisions))
        for value in boundary_neighbourhood(divisions)
        if subdivision_index(value, divisions) != exact_index(value, divisions)
    ]

    assert not wrong, (
        f"{len(wrong)} of {divisions * 3} boundary-adjacent longitudes disagree with "
        f"exact arithmetic: {wrong[:4]}"
    )


@pytest.mark.parametrize("divisions", DIVISIONS)
def test_the_result_is_always_a_valid_arc(divisions: int) -> None:
    """Never `divisions`, which would be sign 12 — house 13."""
    values = boundary_neighbourhood(divisions)
    values += [-0.0, 0.0, 360.0, -1e-18, -5e-324, 720.0, -360.0, -1e-9]

    for value in values:
        index = subdivision_index(value, divisions)
        assert 0 <= index < divisions, (
            f"longitude {value!r} gave arc {index}, outside 0..{divisions - 1}. "
            "For signs that is the thirteenth sign, and SIGNS[12] raises."
        )


def test_a_tiny_negative_angle_folds_to_zero() -> None:
    """The bug this file found, pinned.

    `(-1e-18) % 360.0` is 360.0, because the true answer is a hair below
    360 and that is the nearest float. A value like -1e-18 is exactly
    what subtracting two nearly-equal angles produces, and this codebase
    subtracts the ayanamsa from a tropical longitude on every planet of
    every chart.
    """
    # The range property holds for every negative, however small.
    for tiny in (-5e-324, -1e-18, -1e-12, -1e-9, -0.0):
        folded = normalise_longitude(tiny)
        assert 0.0 <= folded < FULL_CIRCLE, (
            f"normalise_longitude({tiny!r}) returned {folded!r}, which is not in "
            "[0, 360). The next step is SIGNS[12], and there are twelve signs."
        )

    # Landing on zero is a narrower claim, and only true for inputs so
    # small that 360 - x is not representable below 360. At -1e-12 the
    # subtraction IS representable, and 359.999999999999° is the last
    # picodegree of Pisces — sign 11, correctly. Asserting 0 there was an
    # over-specified test, not a bug in the fold.
    for vanishing in (-5e-324, -1e-18, -0.0):
        assert subdivision_index(vanishing, SIGN_COUNT) == 0
        assert subdivision_index(vanishing, NAKSHATRA_COUNT) == 0

    assert subdivision_index(-1e-12, SIGN_COUNT) == SIGN_COUNT - 1, (
        "a longitude one picodegree before 0° Aries is the end of Pisces, not the start of Aries"
    )


def test_neither_float_ordering_is_exact() -> None:
    """The measurement that corrected the docstring.

    Both float forms are wrong at some boundary-adjacent values, and the
    ordering the module used to call a correctness fix is wrong more
    often than the one it replaced. Kept as a test rather than a comment
    so the claim is re-run rather than remembered — the previous version
    of that claim was remembered, and was wrong.
    """
    report = {}
    for divisions in (SIGN_COUNT, NAKSHATRA_COUNT, PADA_COUNT):
        values = boundary_neighbourhood(divisions)
        report[divisions] = (
            sum(1 for v in values if multiply_first(v, divisions) != exact_index(v, divisions)),
            sum(1 for v in values if divide_first(v, divisions) != exact_index(v, divisions)),
            sum(1 for v in values if subdivision_index(v, divisions) != exact_index(v, divisions)),
        )

    # Signs are exact in binary — 360/12 = 30 — so no form fails there.
    # This is precisely why eyeballing sign placements, the obvious check,
    # would never have found any of it.
    assert report[SIGN_COUNT] == (0, 0, 0), (
        f"signs now disagree somewhere: {report[SIGN_COUNT]}. 360/12 is exact, "
        "so this means something other than rounding has changed."
    )

    multiply_wrong, divide_wrong, exact_wrong = report[NAKSHATRA_COUNT]
    assert exact_wrong == 0
    assert multiply_wrong > divide_wrong, (
        f"nakshatras: multiply-first wrong {multiply_wrong}, divide-first "
        f"{divide_wrong}. The docstring records that multiply-first is the WORSE "
        "of the two; if that has stopped being true, correct the docstring."
    )
    assert (multiply_wrong, divide_wrong) == (4, 1), (
        f"the measured counts are now {(multiply_wrong, divide_wrong)}; the "
        "docstring records 4 and 1"
    )


def test_all_three_forms_agree_on_longitudes_a_planet_can_actually_have() -> None:
    """Why none of this was ever user-visible.

    The forms diverge only within one ulp of a boundary. Over a large
    random sweep they agree exactly — which is the honest scope of the
    fix, and the reason it is described as a correctness-of-reasoning fix
    rather than a bug users hit.
    """
    generator = random.Random(20260916)
    disagreements = 0

    for _ in range(200_000):
        longitude = generator.uniform(-720.0, 1080.0)
        truth = exact_index(longitude, PADA_COUNT)
        if subdivision_index(longitude, PADA_COUNT) != truth:
            disagreements += 1
        if multiply_first(longitude, PADA_COUNT) != truth:
            disagreements += 1

    assert disagreements == 0, (
        f"{disagreements} disagreements over 200,000 random longitudes. The forms "
        "are supposed to differ only within one ulp of a boundary, so this means "
        "the divergence is far wider than the docstring claims."
    )
