"""The Sade Sati window, through the API.

`test_transit.py` proves `sade_sati_window` finds the right boundaries.
These prove the API hands them out correctly — which is a different
thing, and the half a user actually receives.

The dates are the point of the whole feature. "When does this end" is
the question people ask about Sade Sati, and a wrong date is worse than
no date: somebody plans around it.
"""

from __future__ import annotations

from datetime import UTC, datetime

import pytest
from fastapi.testclient import TestClient

from app.core.constants import SIGNS
from app.main import app


@pytest.fixture(scope="module")
def client() -> TestClient:
    """A client carrying the internal token.

    Every endpoint here requires `X-Internal-Token`. Omitting it gives
    401 on every request — which is the guard working, and which
    test_api_charts.py already records having learned the same way.
    """
    from app.middleware import INTERNAL_TOKEN_HEADER
    from app.settings import settings

    return TestClient(app, headers={INTERNAL_TOKEN_HEADER: settings.internal_token})


# Saturn was in Aquarius through 2023 and entered Pisces in 2023.
# For a natal Moon in PISCES, Sade Sati runs while Saturn is in
# Aquarius (12th), Pisces (1st) or Aries (2nd) — so a 2024 instant is
# squarely inside it, and both boundaries fall inside a 40-year window.
AT = datetime(2024, 6, 1, tzinfo=UTC)
PISCES = SIGNS.index("Pisces")


def test_a_running_stretch_reports_both_dates(client: TestClient) -> None:
    response = client.post(
        "/v1/transits/compute",
        json={"at": AT.isoformat(), "natal_moon_sign": PISCES},
    )
    assert response.status_code == 200, response.text

    sade_sati = response.json()["sade_sati"]
    assert sade_sati["is_active"] is True, sade_sati

    assert sade_sati["started_at"] is not None, (
        "a running Sade Sati reports no start date. 'When does this end' is the "
        "question people ask, and it cannot be answered without both ends."
    )
    assert sade_sati["ends_at"] is not None, sade_sati

    started = datetime.fromisoformat(sade_sati["started_at"])
    ends = datetime.fromisoformat(sade_sati["ends_at"])

    assert started < AT < ends, (
        f"the reported window {started} .. {ends} does not contain the instant "
        f"{AT} it was asked about, yet is_active is true"
    )


def test_the_stretch_is_about_seven_and_a_half_years(client: TestClient) -> None:
    """The name is the assertion.

    Saturn spends ~2.5 years per sign and Sade Sati is three signs, so
    the span is ~7.5 years. Retrogrades near either boundary move it by
    months, not years — so a window outside 6.5..9 years means the
    boundary search picked the wrong crossing, which is exactly the
    failure `sade_sati_window` walks forward to avoid.
    """
    response = client.post(
        "/v1/transits/compute",
        json={"at": AT.isoformat(), "natal_moon_sign": PISCES},
    )
    sade_sati = response.json()["sade_sati"]

    started = datetime.fromisoformat(sade_sati["started_at"])
    ends = datetime.fromisoformat(sade_sati["ends_at"])
    years = (ends - started).days / 365.25

    assert 6.5 <= years <= 9.0, (
        f"the reported stretch is {years:.2f} years. Sade Sati is Saturn crossing "
        f"three signs at ~2.5 years each; anything far from 7.5 means the search "
        f"took a retrograde dip for the real exit, or missed one."
    )


def test_every_reported_window_is_about_seven_and_a_half_years(client: TestClient) -> None:
    """EVERY sign, not one.

    This is the test that was missing, and its absence let two real bugs
    ship through a green suite.

    The first version checked one Moon sign — Pisces — which happened to
    be correct. Asking for all twelve revealed Gemini reporting a SIX
    MONTH Sade Sati and Cancer reporting under three years, because the
    window function then in use returned the first stretch inside a
    forty-year span rather than the one containing `at`, and could not
    tell an entry from a sign change within the stretch.

    Both failures look entirely plausible in isolation. Only the span
    gives them away, and only if you check every sign.
    """
    response = client.post(
        "/v1/transits/sade-sati/windows",
        json={"at": AT.isoformat()},
    )
    assert response.status_code == 200, response.text

    wrong: list[str] = []
    for window in response.json()["windows"]:
        if window["started_at"] is None:
            continue  # not in it at this instant; nothing to check

        started = datetime.fromisoformat(window["started_at"])
        ends = datetime.fromisoformat(window["ends_at"])
        years = (ends - started).days / 365.25

        if not 6.5 <= years <= 9.0:
            wrong.append(
                f"{window['moon_sign']}: {years:.2f} years ({started.date()} to {ends.date()})"
            )

    assert not wrong, (
        "these Moon signs report a Sade Sati that is not about 7.5 years:\n  "
        + "\n  ".join(wrong)
        + "\n\nSaturn crosses three signs at ~2.5 years each. A short window means "
        "the search found a sign change WITHIN the stretch and took it for the "
        "entry, or reported a different stretch entirely."
    )


def test_a_reported_window_contains_the_instant_it_was_asked_about(
    client: TestClient,
) -> None:
    """The window must bracket `at`.

    A window that does not contain the instant it was computed for is a
    stretch from someone else's decade — which is exactly what a wide
    scan returns when it picks the first stretch it meets rather than the
    current one. Twenty-year-old dates look completely plausible on a
    screen.
    """
    response = client.post(
        "/v1/transits/sade-sati/windows",
        json={"at": AT.isoformat()},
    )

    stale: list[str] = []
    for window in response.json()["windows"]:
        if window["started_at"] is None:
            continue

        started = datetime.fromisoformat(window["started_at"])
        ends = datetime.fromisoformat(window["ends_at"])
        if not (started <= AT <= ends):
            stale.append(f"{window['moon_sign']}: {started.date()} to {ends.date()}")

    assert not stale, (
        f"these windows do not contain {AT.date()}, the instant they were computed "
        f"for:\n  " + "\n  ".join(stale)
    )


def test_a_sign_not_currently_in_sade_sati_reports_nothing(client: TestClient) -> None:
    """Most signs, most of the time.

    Saturn returns every ~29.5 years and the stretch is 7.5, so at any
    instant roughly three of twelve signs are in it. The other nine must
    report null rather than their last one or their next one — the
    question is "when does MINE end", and a date for a stretch that is
    not running is an answer to a question nobody asked.
    """
    response = client.post(
        "/v1/transits/sade-sati/windows",
        json={"at": AT.isoformat()},
    )
    windows = response.json()["windows"]

    running = [w for w in windows if w["started_at"] is not None]

    assert 1 <= len(running) <= 5, (
        f"{len(running)} of twelve Moon signs report a running Sade Sati. Saturn "
        f"occupies three consecutive signs, so about three should — far more than "
        f"that means stale or invented windows are being reported."
    )


def test_every_moon_sign_gets_a_window(client: TestClient) -> None:
    """Twelve in, twelve out, each naming its own sign."""
    response = client.post(
        "/v1/transits/sade-sati/windows",
        json={"at": AT.isoformat()},
    )
    assert response.status_code == 200, response.text

    windows = response.json()["windows"]
    assert len(windows) == 12, f"got {len(windows)} windows, want one per Moon sign"

    for index, window in enumerate(windows):
        assert window["moon_sign_index"] == index
        assert window["moon_sign"] == SIGNS[index], (
            f"window {index} is labelled {window['moon_sign']}, want {SIGNS[index]} — "
            f"an off-by-one here gives every user their neighbour's dates"
        )


def test_the_windows_endpoint_agrees_with_the_single_chart_one(client: TestClient) -> None:
    """The two paths must not disagree.

    The transits endpoint computes one window; the windows endpoint
    computes twelve. They are separate call sites into the same function
    with separately chosen search spans, which is exactly the shape of
    thing that drifts — one of them getting a different answer for the
    same sign at the same instant would mean a user's screen and their
    stored data disagree about when their Sade Sati ends.
    """
    single = client.post(
        "/v1/transits/compute",
        json={"at": AT.isoformat(), "natal_moon_sign": PISCES},
    ).json()["sade_sati"]

    many = client.post(
        "/v1/transits/sade-sati/windows",
        json={"at": AT.isoformat()},
    ).json()["windows"]

    mine = next(w for w in many if w["moon_sign_index"] == PISCES)

    """
       Equal to within the search's own resolution, not to the second.

       The two endpoints scan different spans — the single-chart one
       brackets twelve years either side of `at`, the twelve-sign one
       takes its span from the request — so `find_saturn_ingresses`
       starts its bisection from different brackets and converges to
       within its tolerance rather than to an identical instant. The
       observed difference is about ten seconds, on a boundary the UI
       renders as a month and a year.

       Asserting exact equality made this fail for a reason that is a
       property of bisection rather than a disagreement about the
       answer. Five minutes is far beyond the observed drift and far
       below the weeks Saturn spends near a sign boundary, so a genuine
       disagreement — the two paths finding different crossings — still
       fails.
       """

    for field in ("started_at", "ends_at"):
        one = datetime.fromisoformat(single[field])
        other = datetime.fromisoformat(mine[field])
        drift = abs((one - other).total_seconds())

        assert drift <= 300, (
            f"{field}: the transits endpoint says {one} and the windows endpoint "
            f"says {other} — {drift:.0f} seconds apart. Bisection accounts for "
            f"seconds; minutes mean the two paths are finding different crossings."
        )


def test_a_sign_with_no_stretch_in_the_window_reports_none(client: TestClient) -> None:
    """None, not a guess.

    Saturn returns every ~29.5 years, so at any instant most Moon signs
    are nowhere near their Sade Sati — and a narrow search window finds
    no entry for them. That must come back as null rather than as a
    fabricated date.
    """
    response = client.post(
        "/v1/transits/sade-sati/windows",
        # The narrowest the schema permits, so most signs fall outside.
        json={"at": AT.isoformat(), "search_years": 10},
    )
    windows = response.json()["windows"]

    absent = [w for w in windows if w["started_at"] is None]
    assert absent, (
        "every Moon sign reported a window inside a ten-year search. Saturn takes "
        "~29.5 years to come round, so most signs cannot have one — dates are "
        "being invented."
    )
    for window in absent:
        assert window["ends_at"] is None, (
            f"{window['moon_sign']} has an end date with no start: {window}"
        )
