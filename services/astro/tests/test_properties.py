"""Property-based tests.

Phase 0 has little domain logic to property-test — the ephemeris arrives
in Phase 2, where `hypothesis` does the real work (dashas summing to 120
years for any Moon longitude, houses being a permutation of 1..12, Ketu
being exactly opposite Rahu).

What exists now that genuinely benefits is the edit-distance function
behind the typo guard. It is the kind of small numeric routine where
example-based tests pass happily while an off-by-one hides in a branch
nobody thought to write an example for.

These tests also prove the harness works before Phase 2 depends on it.
"""

from __future__ import annotations

from hypothesis import given, settings
from hypothesis import strategies as st

from app.env_check import _MAX_DISTANCE, _edit_distance, find_suspected_typos
from app.settings import Settings

# Env-var-shaped names: uppercase, digits, underscores.
env_names = st.text(
    alphabet=st.sampled_from("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_"),
    min_size=1,
    max_size=24,
)

LIMIT = 100  # high enough that the early-exit path is not what is under test


class TestEditDistanceIsAMetric:
    """Levenshtein distance is a metric. If any of these fail, the
    implementation is wrong regardless of what the examples say."""

    @given(a=env_names)
    def test_identity(self, a: str) -> None:
        """d(a, a) == 0, and only then."""
        assert _edit_distance(a, a, LIMIT) == 0

    @given(a=env_names, b=env_names)
    def test_symmetry(self, a: str, b: str) -> None:
        """d(a, b) == d(b, a).

        Worth pinning: the implementation iterates `a` in the outer loop
        and `b` in the inner, so an asymmetry would be easy to introduce
        and invisible in a hand-written example.
        """
        assert _edit_distance(a, b, LIMIT) == _edit_distance(b, a, LIMIT)

    @given(a=env_names, b=env_names, c=env_names)
    @settings(max_examples=200)
    def test_triangle_inequality(self, a: str, b: str, c: str) -> None:
        """d(a, c) <= d(a, b) + d(b, c)."""
        assert (
            _edit_distance(a, c, LIMIT)
            <= _edit_distance(a, b, LIMIT) + _edit_distance(b, c, LIMIT)
        )

    @given(a=env_names, b=env_names)
    def test_bounded_by_longer_string(self, a: str, b: str) -> None:
        """Distance never exceeds the longer length: every character can
        be substituted, then the remainder inserted or deleted."""
        assert _edit_distance(a, b, LIMIT) <= max(len(a), len(b))

    @given(a=env_names)
    def test_distance_to_empty_is_length(self, a: str) -> None:
        assert _edit_distance(a, "", LIMIT) == len(a)


class TestEarlyExit:
    """The limit parameter is an optimisation. It must never change an
    answer that falls within the limit."""

    @given(a=env_names, b=env_names)
    def test_limit_does_not_alter_results_below_it(self, a: str, b: str) -> None:
        true_distance = _edit_distance(a, b, LIMIT)
        limited = _edit_distance(a, b, _MAX_DISTANCE)

        if true_distance <= _MAX_DISTANCE:
            assert limited == true_distance
        else:
            # Above the limit it may report any over-limit value, but it
            # must never claim the strings are closer than they are.
            assert limited > _MAX_DISTANCE


class TestTypoDetectionProperties:
    @given(junk=env_names)
    def test_detection_never_raises(self, junk: str) -> None:
        """Whatever is in the environment, scanning must not blow up.

        The guard runs during startup, before anything else binds. A
        crash here would be a far worse failure than the typo it exists
        to catch.
        """
        import os

        original = os.environ.get(junk)
        try:
            os.environ[junk] = "x"
            find_suspected_typos(Settings())
        finally:
            if original is None:
                os.environ.pop(junk, None)
            else:
                os.environ[junk] = original

    @given(name=env_names)
    def test_a_declared_field_is_never_flagged_as_its_own_typo(self, name: str) -> None:
        """An exactly-correct variable must never be reported."""
        import os

        declared = {f.upper() for f in Settings.model_fields}
        if name not in declared:
            return

        original = os.environ.get(name)
        try:
            os.environ[name] = "x"
            flagged = dict(find_suspected_typos(Settings()))
            assert name not in flagged
        finally:
            if original is None:
                os.environ.pop(name, None)
            else:
                os.environ[name] = original
