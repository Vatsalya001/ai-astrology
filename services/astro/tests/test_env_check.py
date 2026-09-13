"""Tests for the typo'd-environment-variable guard.

The guard exists because `extra="forbid"` does NOT cover environment
variables — only `.env` file entries. These tests pin both halves of
that: the typo is caught, and ordinary unrelated variables are not.

The false-positive half matters as much as the true-positive half. A
guard that refuses to start on `PATH` or `LANG` would be ripped out
within a day, and then the real protection goes with it.
"""

from __future__ import annotations

import pytest

from app.env_check import (
    SuspectedTypoError,
    _edit_distance,
    assert_no_typos,
    find_suspected_typos,
)
from app.settings import Settings


@pytest.fixture
def settings() -> Settings:
    return Settings()


class TestEditDistance:
    @pytest.mark.parametrize(
        ("a", "b", "expected"),
        [
            ("SAME", "SAME", 0),
            ("DEFAULT_AYANAMSA", "DEFAULT_AYANMSA", 1),  # dropped letter
            ("LOG_LEVEL", "LOG_LEVLE", 2),  # transposition
            ("ENV", "END", 1),  # substitution
        ],
    )
    def test_known_distances(self, a: str, b: str, expected: int) -> None:
        assert _edit_distance(a, b, limit=5) == expected

    def test_abandons_past_limit(self) -> None:
        """Early exit must report 'over limit', never a wrong small value."""
        assert _edit_distance("SHORT", "COMPLETELY_DIFFERENT", limit=2) > 2


class TestTypoDetection:
    def test_catches_dropped_letter(self, settings: Settings, monkeypatch) -> None:
        """The motivating case: DEFAULT_AYANMSA silently selects the
        default zodiac, so every chart in the system would be wrong."""
        monkeypatch.setenv("DEFAULT_AYANMSA", "lahiri")

        suspects = dict(find_suspected_typos(settings))
        assert "DEFAULT_AYANMSA" in suspects
        assert suspects["DEFAULT_AYANMSA"] == "DEFAULT_AYANAMSA"

    def test_catches_transposition(self, settings: Settings, monkeypatch) -> None:
        monkeypatch.setenv("LOG_LEVLE", "debug")
        assert "LOG_LEVLE" in dict(find_suspected_typos(settings))

    def test_assert_raises_and_names_both_sides(self, settings: Settings, monkeypatch) -> None:
        monkeypatch.setenv("DEFAULT_AYANMSA", "lahiri")

        with pytest.raises(SuspectedTypoError) as exc:
            assert_no_typos(settings)

        message = str(exc.value)
        assert "DEFAULT_AYANMSA" in message, "must name the offending variable"
        assert "DEFAULT_AYANAMSA" in message, "must suggest the intended field"

    def test_correct_spelling_is_not_flagged(self, settings: Settings, monkeypatch) -> None:
        monkeypatch.setenv("DEFAULT_AYANAMSA", "lahiri")
        assert_no_typos(settings)


class TestNoFalsePositives:
    """A guard that cries wolf gets deleted, and takes the real
    protection with it."""

    @pytest.mark.parametrize(
        "name",
        [
            "PATH",
            "HOME",
            "LANG",
            "SHELL",
            "PWD",
            "TERM",
            "HOSTNAME",
            "PYTHONPATH",
            "VIRTUAL_ENV",
            "KUBERNETES_SERVICE_HOST",
            "AWS_REGION",
            "DATABASE_URL",  # belongs to api-service, not these two
        ],
    )
    def test_common_variables_are_ignored(self, settings: Settings, monkeypatch, name: str) -> None:
        monkeypatch.setenv(name, "whatever")

        flagged = dict(find_suspected_typos(settings))
        assert name not in flagged, f"{name} was wrongly flagged as a typo"

    def test_a_clean_environment_raises_nothing(self, settings: Settings, monkeypatch) -> None:
        for junk in ("PATH", "HOME", "LANG", "CI", "TERM"):
            monkeypatch.setenv(junk, "x")
        assert_no_typos(settings)

    def test_short_names_are_skipped(self, settings: Settings, monkeypatch) -> None:
        """Short names collide by coincidence — ENV vs END is distance 1
        but obviously unrelated."""
        monkeypatch.setenv("END", "x")
        assert "END" not in dict(find_suspected_typos(settings))
