"""The HTTP boundary: who may reach this service, and what it logs.

`app/middleware.py` was at **0% coverage**. It holds the
`X-Internal-Token` check — §14: *"ai-service not publicly reachable;
X-Internal-Token required"* — and the trace-ID sanitiser, and neither
service tested that omitting the token is REFUSED.

`services/astro/tests/` builds its `TestClient` with the header already
attached, so every test there passes the guard and none of them observes
it fire. That is the shape `.claude/rules/testing.md` warns about: "a
guard that has never been observed to fire is a guard you cannot trust."

This service added `/v1/complete` and `/v1/routing` in Phase 4 — the two
routes that reach a model and spend money — so the guard covering them
needs to be watched doing its job.
"""

from __future__ import annotations

import pytest
from fastapi.testclient import TestClient

from app.middleware import INTERNAL_TOKEN_HEADER, PUBLIC_PATHS, TRACE_HEADER
from app.settings import settings


@pytest.fixture
def client(monkeypatch: pytest.MonkeyPatch) -> TestClient:
    """The real app, with real middleware, and NO token attached.

    Deliberately not the astro pattern of baking the header into the
    client: doing that is what left the guard unobserved in both
    services. Tests that need to get through add it explicitly, so the
    header is visible at every call site that relies on it.
    """
    monkeypatch.setattr(settings, "llm_provider", "mock")
    monkeypatch.setattr(settings, "llm_provider_tier", "local")

    from app.main import app

    # `raise_server_exceptions=False` so a handler error surfaces as a
    # 500 rather than propagating — this file is about the boundary, and
    # a route blowing up past the guard must not be mistaken for the
    # guard letting it through.
    return TestClient(app, raise_server_exceptions=False)


GUARDED = [
    ("POST", "/v1/complete", {"message": "what does my chart say"}),
    ("GET", "/v1/routing", None),
    ("PATCH", "/v1/routing", {"reset": True}),
]


# ─── the token ───────────────────────────────────────────────────────


class TestTheInternalTokenIsRequired:
    @pytest.mark.parametrize(("method", "path", "body"), GUARDED, ids=lambda v: str(v))
    def test_no_token_is_refused(
        self, client: TestClient, method: str, path: str, body: dict | None
    ) -> None:
        """The negative case neither service had.

        This service must not be reachable from the internet, and the
        token is defence in depth for the day that network boundary is
        misconfigured. A guard nothing has watched fire is a guard that
        may already have stopped working.
        """
        response = client.request(method, path, json=body)

        assert response.status_code in (401, 403), (
            f"{method} {path} answered {response.status_code} with NO internal token"
        )

    @pytest.mark.parametrize(("method", "path", "body"), GUARDED, ids=lambda v: str(v))
    def test_a_wrong_token_is_refused(
        self, client: TestClient, method: str, path: str, body: dict | None
    ) -> None:
        response = client.request(
            method, path, json=body, headers={INTERNAL_TOKEN_HEADER: "not-the-token"}
        )

        assert response.status_code in (401, 403)

    def test_a_correct_prefix_is_not_enough(self, client: TestClient) -> None:
        """Proves the comparison is not a prefix match.

        `_constant_time_equals` exists so a naive `==` cannot leak the
        secret one byte at a time to anyone who can measure latency.
        Timing is not testable here, but a PREFIX being accepted would
        be the same bug with a louder symptom — and nothing checked it.
        """
        truncated = settings.internal_token[:-1]

        response = client.post(
            "/v1/complete",
            json={"message": "x"},
            headers={INTERNAL_TOKEN_HEADER: truncated},
        )

        assert response.status_code in (401, 403)

    def test_an_overlong_token_is_refused(self, client: TestClient) -> None:
        response = client.post(
            "/v1/complete",
            json={"message": "x"},
            headers={INTERNAL_TOKEN_HEADER: settings.internal_token + "x"},
        )

        assert response.status_code in (401, 403)

    def test_the_right_token_gets_through_the_guard(self, client: TestClient) -> None:
        """The positive case, and the one that stops the rest being
        satisfied by a service that refuses everything.

        Asserted as "not 401/403" rather than 200: the mock provider has
        no fixture for this request, so the handler may well fail —
        which is fine. What is being tested is that the request got PAST
        the boundary.
        """
        response = client.get(
            "/v1/routing", headers={INTERNAL_TOKEN_HEADER: settings.internal_token}
        )

        assert response.status_code not in (401, 403)

    def test_the_error_body_does_not_echo_the_token(self, client: TestClient) -> None:
        """A refusal that quotes what was supplied is a log of near-misses.

        `.claude/rules/security.md`: a credential never reaches an error
        message. A brute-force attempt would otherwise write its own
        guesses into the response and the access log.
        """
        response = client.post(
            "/v1/complete",
            json={"message": "x"},
            headers={INTERNAL_TOKEN_HEADER: "guess-abc123"},
        )

        assert "guess-abc123" not in response.text
        assert settings.internal_token not in response.text


class TestThePublicPathsAreDeliberate:
    @pytest.mark.parametrize("path", sorted(PUBLIC_PATHS))
    def test_a_public_path_needs_no_token(self, client: TestClient, path: str) -> None:
        """Every entry on the allowlist must actually BE public.

        Otherwise the allowlist becomes a place to park a path and stop
        thinking about it, and a path that is in fact guarded sits there
        implying it is not.
        """
        response = client.get(path)

        assert response.status_code not in (401, 403), f"{path} is allowlisted but refused"

    def test_health_is_the_reason_the_allowlist_exists(self, client: TestClient) -> None:
        # api-service probes liveness without holding a credential, and
        # so do container orchestrators.
        response = client.get("/health")

        assert response.status_code == 200
        assert response.json()["service"] == "ai"

    def test_no_model_route_is_on_the_allowlist(self) -> None:
        """The assertion that stops the allowlist growing quietly.

        A route that reaches a provider must never be public: it spends
        money on demand and accepts free-text input. Checked against the
        constant rather than by walking routes, because the constant is
        what a reviewer reads.
        """
        for path in PUBLIC_PATHS:
            assert not path.startswith("/v1/"), (
                f"{path} is a versioned API route on the public allowlist"
            )


# ─── the trace ID ────────────────────────────────────────────────────


class TestTheTraceIdIsSanitised:
    def test_a_safe_inbound_id_is_adopted(self, client: TestClient) -> None:
        """Honouring it is what makes one user request correlatable
        across web -> Go -> Python."""
        response = client.get("/health", headers={TRACE_HEADER: "abc-123_XY.z"})

        assert response.headers[TRACE_HEADER] == "abc-123_XY.z"

    @pytest.mark.parametrize(
        ("label", "hostile"),
        [
            ("an email address", "someone@example.com"),
            ("a log-injection payload", "ok\nWARN forged log line"),
            ("a bearer token", "Bearer sk-not-a-real-key"),
            ("far too long", "x" * 200),
            ("a null byte", "abc\x00def"),
        ],
    )
    def test_a_hostile_id_is_replaced_not_echoed(
        self, client: TestClient, label: str, hostile: str
    ) -> None:
        """The header is attacker-controlled and is written into every
        log line for the request.

        An unconstrained value is a channel for putting arbitrary content
        — an email address, a token, a forged log line — into logs that
        are otherwise carefully PII-free.
        """
        response = client.get("/health", headers={TRACE_HEADER: hostile})

        returned = response.headers[TRACE_HEADER]
        assert returned != hostile, f"{label} was adopted verbatim"
        assert "@" not in returned
        assert "\n" not in returned
        assert len(returned) <= 64

    def test_a_missing_id_is_minted(self, client: TestClient) -> None:
        # The negative case: middleware that only ever echoed would pass
        # the adoption test and leave un-correlatable requests.
        response = client.get("/health")

        assert response.headers[TRACE_HEADER]

    def test_two_requests_get_different_minted_ids(self, client: TestClient) -> None:
        first = client.get("/health").headers[TRACE_HEADER]
        second = client.get("/health").headers[TRACE_HEADER]

        assert first != second, "a constant trace ID correlates nothing"
