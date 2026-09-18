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

    assert mine["started_at"] == single["started_at"], (
        f"the windows endpoint says {mine['started_at']} and the transits endpoint "
        f"says {single['started_at']} for the same sign at the same instant"
    )
    assert mine["ends_at"] == single["ends_at"]


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
