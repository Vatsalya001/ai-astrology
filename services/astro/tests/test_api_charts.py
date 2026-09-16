"""The HTTP surface.

`app/api/` is an adapter, so these tests check adaptation — validation,
shape, status codes — not astrology. The maths has its own tests in
`core`, and duplicating them here would mean two places to update and two
chances to disagree.

The exceptions are the end-to-end assertions: that a real request
produces a complete, self-consistent chart, and that the unknown-time
contract survives serialisation. Those cross the seam and nothing else
covers them.
"""

from __future__ import annotations

import pytest
from fastapi.testclient import TestClient

from app.main import app

JAIPUR = {
    "utc_instant": "1994-08-17T09:05:00Z",
    "latitude": 26.9124,
    "longitude": 75.7873,
}


@pytest.fixture(scope="module")
def client() -> TestClient:
    """A client carrying the internal token.

    Every endpoint on this service requires `X-Internal-Token` — it is
    internal-only and must never be reachable from the internet. The
    first version of this file omitted the header and got 401 on all
    seventeen requests, which is the guard working.
    """
    from app.middleware import INTERNAL_TOKEN_HEADER
    from app.settings import settings

    return TestClient(app, headers={INTERNAL_TOKEN_HEADER: settings.internal_token})


def post_chart(client: TestClient, **birth_overrides):
    body = {"birth": {**JAIPUR, **birth_overrides}}
    response = client.post("/v1/charts/compute", json=body)
    assert response.status_code == 200, response.text
    return response.json()


# ─── the chart endpoint ──────────────────────────────────────────────


def test_a_complete_chart_comes_back(client: TestClient) -> None:
    chart = post_chart(client)

    assert len(chart["planets"]) == 9
    assert len(chart["houses"]) == 12
    assert chart["ascendant"] is not None
    assert chart["navamsa"] is not None
    assert chart["dashas"] is not None
    assert len(chart["dashas"]) == 9


def test_the_response_is_internally_consistent(client: TestClient) -> None:
    """The summary must agree with the detail.

    `summary` exists so callers do not walk the whole structure for four
    facts. That is only safe if it cannot drift from the planets it
    summarises — and a summary built from a separate computation could.
    """
    chart = post_chart(client)

    moon = next(p for p in chart["planets"] if p["planet"] == "Moon")
    sun = next(p for p in chart["planets"] if p["planet"] == "Sun")

    assert chart["summary"]["moon_sign"] == moon["sign"]
    assert chart["summary"]["sun_sign"] == sun["sign"]
    assert chart["summary"]["moon_nakshatra"] == moon["nakshatra"]
    assert chart["summary"]["moon_nakshatra_pada"] == moon["pada"]
    assert chart["summary"]["ascendant_sign"] == chart["ascendant"]["sign"]


def test_houses_and_planets_agree_about_placement(client: TestClient) -> None:
    """Both directions, because the two lists are built separately."""
    chart = post_chart(client)

    for house in chart["houses"]:
        for planet_name in house["planets"]:
            placed = next(p for p in chart["planets"] if p["planet"] == planet_name)
            assert placed["house"] == house["house"]


def test_meta_records_what_actually_ran(client: TestClient) -> None:
    """Provenance, so "why did it say that" is answerable years later.

    `ayanamsa_value` is the offset actually applied, not just its name —
    two releases can both say "lahiri" and differ by arcseconds.
    """
    chart = post_chart(client)
    meta = chart["meta"]

    assert meta["schema_version"] == 1
    assert meta["ayanamsa"] == "lahiri"
    assert 23.0 < meta["ayanamsa_value"] < 25.0
    assert meta["engine_version"].startswith("skyfield-")
    assert "de421" in meta["engine_version"]
    assert meta["time_accuracy"] == "exact"


def test_the_engine_version_pins_the_schema(client: TestClient) -> None:
    """A schema bump must change the engine version.

    Otherwise a chart stored under schema 1 and one stored under schema 2
    are indistinguishable in the database, and the recompute query that
    finds "charts built by an older engine" silently misses them.
    """
    from app.api.charts import engine_version
    from app.schemas.chart import CHART_SCHEMA_VERSION

    assert f"schema{CHART_SCHEMA_VERSION}" in engine_version()


# ─── the unknown-birth-time contract, across the wire ────────────────


def test_unknown_time_returns_nulls_not_guesses(client: TestClient) -> None:
    """The contract that matters most, asserted on the JSON itself.

    `core` omits the ascendant, houses and dashas when the time is
    unknown. This checks the omission survives serialisation as `null`
    rather than becoming `{}`, `0`, or a default-constructed object that
    a consumer would read as a real rising sign.
    """
    chart = post_chart(client, time_accuracy="unknown")

    assert chart["ascendant"] is None
    assert chart["houses"] is None
    assert chart["dashas"] is None
    assert chart["summary"]["ascendant_sign"] is None

    # Planets are still computed and still meaningful.
    assert len(chart["planets"]) == 9
    assert all(p["house"] == 0 for p in chart["planets"])
    assert all(p["aspects"] == [] for p in chart["planets"])

    # And the Moon's sign is still reported, because it survives a
    # missing birth time.
    assert chart["summary"]["moon_sign"]


def test_yogas_are_empty_without_a_birth_time(client: TestClient) -> None:
    """Every yoga depends on houses, and houses need an ascendant."""
    assert post_chart(client, time_accuracy="unknown")["yogas"] == []


def test_approximate_time_still_computes_everything(client: TestClient) -> None:
    """`approximate` is not `unknown`.

    The caveat belongs in the UI, not in missing data — the user gave a
    time and it is probably close, so withholding the chart would be
    unhelpful rather than honest.
    """
    chart = post_chart(client, time_accuracy="approximate")

    assert chart["ascendant"] is not None
    assert chart["dashas"] is not None
    assert chart["meta"]["time_accuracy"] == "approximate"


# ─── validation at the boundary ──────────────────────────────────────


def test_a_naive_timestamp_is_refused(client: TestClient) -> None:
    """This service never assumes a timezone.

    For an Indian birth a silent UTC assumption is five and a half hours
    of error. The ascendant moves a degree every four minutes, so the
    chart would be wrong by more than a whole sign while looking entirely
    normal.
    """
    response = client.post(
        "/v1/charts/compute",
        json={"birth": {**JAIPUR, "utc_instant": "1994-08-17T09:05:00"}},
    )

    assert response.status_code == 422
    assert "timezone-aware" in response.text


def test_impossible_coordinates_are_refused(client: TestClient) -> None:
    for field, value in (
        ("latitude", 95.0),
        ("latitude", -91.0),
        ("longitude", 181.0),
        ("longitude", -200.0),
    ):
        response = client.post("/v1/charts/compute", json={"birth": {**JAIPUR, field: value}})
        assert response.status_code == 422, f"{field}={value} was accepted"


def test_an_unknown_field_is_refused(client: TestClient) -> None:
    """`extra="forbid"`, and this is why.

    A typo'd field name would otherwise be silently ignored, leaving the
    service computing a chart for a different birth time than the caller
    believes they asked for — which is undetectable from the response.
    """
    response = client.post(
        "/v1/charts/compute",
        json={"birth": {**JAIPUR, "birth_time": "14:35"}},
    )
    assert response.status_code == 422
    assert "birth_time" in response.text


def test_an_unsupported_house_system_is_refused(client: TestClient) -> None:
    """Only whole sign in Phase 2.

    Accepting "placidus" and quietly computing whole sign would be worse
    than refusing — the caller would get a chart that is correct for a
    system they did not ask for.
    """
    response = client.post(
        "/v1/charts/compute", json={"birth": {**JAIPUR, "house_system": "placidus"}}
    )
    assert response.status_code == 422


def test_an_unknown_ayanamsa_is_refused(client: TestClient) -> None:
    response = client.post(
        "/v1/charts/compute", json={"birth": {**JAIPUR, "ayanamsa": "fagan_bradley"}}
    )
    assert response.status_code == 422


# ─── the other endpoints ─────────────────────────────────────────────


def test_dashas_can_be_computed_alone(client: TestClient) -> None:
    """Separate from the chart, so a tree can be recomputed cheaply.

    After a birth-time correction `api-service` needs a new tree but not
    nine new planetary positions.
    """
    response = client.post(
        "/v1/dashas/compute",
        json={
            "moon_longitude": 251.31,
            "birth_instant": "1994-08-17T09:05:00Z",
            "max_level": 3,
        },
    )
    assert response.status_code == 200, response.text

    body = response.json()
    assert len(body["periods"]) == 9
    assert len(body["periods"][0]["children"]) == 9
    assert len(body["periods"][0]["children"][0]["children"]) == 9
    assert body["balance_at_birth_days"] > 0


def test_the_dasha_level_is_bounded(client: TestClient) -> None:
    """A fourth level is 6,561 nodes for a precision no reading uses."""
    for level in (0, 4, 99):
        response = client.post(
            "/v1/dashas/compute",
            json={
                "moon_longitude": 251.31,
                "birth_instant": "1994-08-17T09:05:00Z",
                "max_level": level,
            },
        )
        assert response.status_code == 422, f"max_level={level} was accepted"


def test_transits_and_sade_sati_come_back(client: TestClient) -> None:
    response = client.post(
        "/v1/transits/compute",
        json={"at": "2026-01-01T00:00:00Z", "natal_moon_sign": 0, "natal_ascendant_sign": 3},
    )
    assert response.status_code == 200, response.text

    body = response.json()
    assert len(body["transits"]) == 9
    assert isinstance(body["sade_sati"]["is_active"], bool)

    for transit in body["transits"]:
        assert 1 <= transit["house_from_moon"] <= 12
        assert 1 <= transit["house_from_ascendant"] <= 12


def test_transits_without_a_natal_ascendant(client: TestClient) -> None:
    """The unknown-birth-time case, again across the wire."""
    response = client.post(
        "/v1/transits/compute",
        json={"at": "2026-01-01T00:00:00Z", "natal_moon_sign": 0},
    )
    assert response.status_code == 200

    for transit in response.json()["transits"]:
        assert transit["house_from_ascendant"] is None
        assert 1 <= transit["house_from_moon"] <= 12


# ─── the contract itself ─────────────────────────────────────────────


def test_the_openapi_document_describes_every_endpoint() -> None:
    """This document generates the Go client.

    If an endpoint is missing here it does not exist as far as
    `api-service` is concerned, however well it works over HTTP.
    """
    paths = app.openapi()["paths"]

    for path in ("/v1/charts/compute", "/v1/dashas/compute", "/v1/transits/compute"):
        assert path in paths, f"{path} is absent from the contract"
        assert "post" in paths[path]


def test_the_contract_carries_the_schema_version() -> None:
    """A consumer must be able to tell which shape it is reading."""
    schemas = app.openapi()["components"]["schemas"]
    assert "schema_version" in schemas["ChartMeta"]["properties"]


def test_nullable_fields_are_nullable_in_the_contract() -> None:
    """The unknown-time contract has to survive into the generated client.

    If `ascendant` were non-nullable in the OpenAPI document, the Go
    client would generate a value type, and "no ascendant" would arrive
    as a zero-valued struct — 0° Aries — which reads as a real rising
    sign. The whole honesty property would be lost at the language
    boundary.
    """
    response_schema = app.openapi()["components"]["schemas"]["ChartResponse"]
    ascendant = response_schema["properties"]["ascendant"]

    # Pydantic renders `X | None` as anyOf with a null branch.
    assert "anyOf" in ascendant, f"ascendant is not nullable: {ascendant}"
    assert any(option.get("type") == "null" for option in ascendant["anyOf"])

    for field in ("houses", "dashas", "navamsa"):
        rendered = response_schema["properties"][field]
        assert "anyOf" in rendered, f"{field} is not nullable in the contract"


def test_the_endpoints_refuse_an_unauthenticated_caller() -> None:
    """astro-service is internal only.

    A client without the token gets 401, which is what stops this service
    being usable even if the network topology ever leaks it. Asserted
    directly, because the `client` fixture carries the header and every
    other test in this file would pass with the guard removed.
    """
    anonymous = TestClient(app)

    for path, body in (
        ("/v1/charts/compute", {"birth": JAIPUR}),
        ("/v1/dashas/compute", {"moon_longitude": 0.0, "birth_instant": "1994-08-17T09:05:00Z"}),
        ("/v1/transits/compute", {"at": "2026-01-01T00:00:00Z", "natal_moon_sign": 0}),
    ):
        response = anonymous.post(path, json=body)
        assert response.status_code == 401, f"{path} answered {response.status_code}"


def test_a_wrong_token_is_refused() -> None:
    from app.middleware import INTERNAL_TOKEN_HEADER

    wrong = TestClient(app, headers={INTERNAL_TOKEN_HEADER: "not-the-token"})
    response = wrong.post("/v1/charts/compute", json={"birth": JAIPUR})

    assert response.status_code == 401
