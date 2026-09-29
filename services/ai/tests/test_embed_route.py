"""PHASE-05 task 5.3 — `/v1/embed`, done when local nomic returns 768-dim.

The interesting tests here are not "it returns vectors". They are the
three ways this route could return vectors that are wrong in a way
nothing downstream notices:

  · a blank chunk gets a confident vector and becomes a search result
    for queries it has nothing to do with
  · the provider returns fewer vectors than texts, and the ingester zips
    them by position, attaching every embedding to the wrong passage
  · the width disagrees with the `vector(768)` column

All three produce a corpus that retrieves confidently and wrongly, and
none of them errors on its own. That is why the route checks rather than
trusts.
"""

from __future__ import annotations

import pytest
from fastapi.testclient import TestClient

from app.middleware import INTERNAL_TOKEN_HEADER
from app.settings import settings


@pytest.fixture
def client(monkeypatch: pytest.MonkeyPatch) -> TestClient:
    """The real app, with real middleware and no token attached.

    Same pattern as `test_http_surface.py`: tests that need to get
    through add the header explicitly, so the guard is visible at every
    call site that depends on it.
    """
    monkeypatch.setattr(settings, "llm_provider", "mock")
    monkeypatch.setattr(settings, "llm_provider_tier", "local")

    from app.main import app

    return TestClient(app, raise_server_exceptions=False)


def auth() -> dict[str, str]:
    return {INTERNAL_TOKEN_HEADER: settings.internal_token}


class StubEmbeddings:
    """An embedding provider with a controllable failure mode.

    Deliberately not `OllamaEmbeddingProvider` with a mocked transport:
    the properties under test are about what this ROUTE does with what a
    provider returns, and the provider's own width assertion is already
    tested in `test_ollama_embeddings.py`. Using the real one here would
    mean its guard fires before the route's ever runs.
    """

    def __init__(self, *, dimensions: int = 768, returns: int | None = None) -> None:
        self._dimensions = dimensions
        self._returns = returns
        self.calls: list[list[str]] = []

    @property
    def id(self) -> str:
        return "stub-embeddings"

    @property
    def tier(self) -> str:
        return "local"

    @property
    def dimensions(self) -> int:
        return self._dimensions

    async def embed(self, texts: list[str]) -> list[list[float]]:
        self.calls.append(list(texts))
        count = self._returns if self._returns is not None else len(texts)
        return [[0.1] * self._dimensions for _ in range(count)]


def use(monkeypatch: pytest.MonkeyPatch, provider: object) -> None:
    monkeypatch.setattr("app.api.embed.embedding_from_settings", lambda *a, **k: provider)


class TestTheHappyPath:
    def test_it_returns_one_vector_per_text(
        self, client: TestClient, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        stub = StubEmbeddings()
        use(monkeypatch, stub)

        response = client.post(
            "/v1/embed",
            json={"texts": ["Saturn in the tenth house", "Mars in the seventh"]},
            headers=auth(),
        )

        assert response.status_code == 200, response.text
        body = response.json()
        assert len(body["embeddings"]) == 2
        assert all(len(v) == 768 for v in body["embeddings"])

    def test_the_whole_batch_is_one_provider_call(
        self, client: TestClient, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """Not a loop of single-text requests.

        Every provider rate-limits per request, so a 600-document corpus
        at one call each is both slow and the fastest way to get
        throttled.
        """
        stub = StubEmbeddings()
        use(monkeypatch, stub)

        client.post("/v1/embed", json={"texts": ["a", "b", "c", "d"]}, headers=auth())

        assert len(stub.calls) == 1, f"made {len(stub.calls)} provider calls, expected 1"
        assert stub.calls[0] == ["a", "b", "c", "d"]

    def test_it_reports_what_produced_the_vectors(
        self, client: TestClient, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """The ingester writes `embedding_model` into every chunk row.

        Migration 000007 explains why: a corpus embedded across a model
        change is unfixable without it, because nothing in the data says
        which rows need redoing.
        """
        use(monkeypatch, StubEmbeddings())

        body = client.post("/v1/embed", json={"texts": ["x"]}, headers=auth()).json()

        assert body["dimensions"] == 768
        assert body["provider_id"] == "stub-embeddings"
        assert body["model"] == settings.embedding_model
        assert body["latency_ms"] >= 0


class TestTheWaysAVectorCanBeQuietlyWrong:
    @pytest.mark.parametrize("blank", ["", "   ", "\n\t "])
    def test_a_blank_text_is_refused_not_embedded(
        self, client: TestClient, monkeypatch: pytest.MonkeyPatch, blank: str
    ) -> None:
        """The one that is invisible in the corpus.

        A blank chunk gets a vector that is arbitrary but confidently
        close to *something*, so it becomes a result for queries it has
        nothing to do with — and the row looks perfectly fine. Nothing
        downstream can distinguish it from a real passage.
        """
        stub = StubEmbeddings()
        use(monkeypatch, stub)

        response = client.post("/v1/embed", json={"texts": ["real text", blank]}, headers=auth())

        assert response.status_code == 422
        assert "texts[1]" in response.text
        assert stub.calls == [], "a blank text reached the provider"

    def test_a_short_vector_list_is_refused(
        self, client: TestClient, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """The dangerous one, and the reason the count is checked.

        The ingester zips vectors against chunks BY POSITION. A provider
        that silently truncates therefore attaches every embedding to the
        wrong passage, and the corpus retrieves confidently and wrongly —
        a failure with no error and no symptom except bad answers.
        """
        use(monkeypatch, StubEmbeddings(returns=2))

        response = client.post("/v1/embed", json={"texts": ["a", "b", "c"]}, headers=auth())

        assert response.status_code == 503
        assert "2 vectors for 3 texts" in response.text

    def test_an_unexpected_width_is_visible_to_the_caller(
        self, client: TestClient, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """The route reports the width rather than asserting it.

        Deliberate: the provider asserts its own output width at
        construction, and the `vector(768)` column rejects a mismatch on
        insert. What this route owes the ingester is the number, so the
        ingester can refuse BEFORE writing a corpus it would have to
        re-embed.
        """
        use(monkeypatch, StubEmbeddings(dimensions=1024))

        body = client.post("/v1/embed", json={"texts": ["x"]}, headers=auth()).json()

        assert body["dimensions"] == 1024
        assert len(body["embeddings"][0]) == 1024


class TestTheBoundary:
    def test_it_requires_the_internal_token(self, client: TestClient) -> None:
        """§14: ai-service is not publicly reachable."""
        response = client.post("/v1/embed", json={"texts": ["x"]})
        assert response.status_code == 401

    def test_an_empty_batch_is_refused(self, client: TestClient) -> None:
        response = client.post("/v1/embed", json={"texts": []}, headers=auth())
        assert response.status_code == 422

    def test_an_oversized_batch_is_refused(self, client: TestClient) -> None:
        """Bounded because anything holding the internal token can call it.

        An unbounded batch is a way to hold the embedding model for an
        arbitrary time — the same reasoning as the Phase 3 PDF limit.
        """
        response = client.post("/v1/embed", json={"texts": ["x"] * 257}, headers=auth())
        assert response.status_code == 422

    def test_the_documented_ingest_batch_size_fits(
        self, client: TestClient, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """§9 sets `KB_EMBED_BATCH_SIZE=64`, so 64 must not trip the cap.

        Without this the cap and the documented batch size could drift
        apart and the ingester would fail on its own default.
        """
        use(monkeypatch, StubEmbeddings())

        response = client.post("/v1/embed", json={"texts": ["chunk"] * 64}, headers=auth())
        assert response.status_code == 200

    def test_an_unknown_field_is_refused(self, client: TestClient) -> None:
        """`extra="forbid"`, so a typo'd field name is not silently dropped."""
        response = client.post(
            "/v1/embed", json={"texts": ["x"], "modell": "nomic"}, headers=auth()
        )
        assert response.status_code == 422

    def test_a_provider_failure_is_a_retryable_503(
        self, client: TestClient, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """The caller is an ingestion job that can retry.

        PHASE-04 §2's contract: an unavailable provider is a 503, not an
        internal error.
        """
        from app.providers import ProviderError

        class Dead(StubEmbeddings):
            async def embed(self, texts: list[str]) -> list[list[float]]:
                raise ProviderError("embeddings unreachable", provider_id="stub", retryable=True)

        use(monkeypatch, Dead())

        response = client.post("/v1/embed", json={"texts": ["x"]}, headers=auth())
        assert response.status_code == 503


class TestTheTextNeverReachesALog:
    def test_the_log_line_carries_counts_not_content(
        self, client: TestClient, monkeypatch: pytest.MonkeyPatch, caplog: pytest.LogCaptureFixture
    ) -> None:
        """`.claude/rules/security.md`: log IDs, not objects.

        Corpus text is not user data, but this route cannot know that its
        caller only ever sends corpus text — and a route that logs its
        input is one refactor away from logging a user's question.
        """
        use(monkeypatch, StubEmbeddings())
        secret = "Saturn in the tenth house delays recognition"

        with caplog.at_level("INFO"):
            client.post("/v1/embed", json={"texts": [secret]}, headers=auth())

        assert secret not in caplog.text
        assert "embedded" in caplog.text
