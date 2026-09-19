"""Vectors from a local Ollama, for Phase 5's retrieval.

Separate from `OpenAICompatibleProvider` despite both talking to the same
server: Ollama's embedding endpoint is `/api/embed`, not the OpenAI
`/v1/embeddings` shape, and the request and response bodies differ. One
class pretending to be both would be a branch on every method.

── Why the dimension is asserted rather than trusted ──

A width mismatch is unrecoverable and nearly silent. Vectors of the wrong
size either fail on insert — loud, fine — or land in a column that
happens to accept them, at which point every similarity score in the
product is meaningless and nothing errors. Checking at construction costs
one comparison and turns a silent corruption into a startup crash.
"""

from __future__ import annotations

import httpx

from app.providers.base import ProviderError, ProviderTier


class OllamaEmbeddingProvider:
    """768-dimension vectors from a local `nomic-embed-text`."""

    def __init__(
        self,
        *,
        base_url: str = "http://localhost:11434",
        model: str = "nomic-embed-text",
        dimensions: int = 768,
        provider_id: str = "ollama-embeddings",
        timeout_seconds: float = 30.0,
        client: httpx.AsyncClient | None = None,
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._model = model
        self._dimensions = dimensions
        self._id = provider_id
        self._client = client or httpx.AsyncClient(timeout=timeout_seconds)

    @property
    def id(self) -> str:
        return self._id

    @property
    def tier(self) -> ProviderTier:
        # Nothing leaves the machine. Still blocked in production by the
        # PII guard, on reliability grounds rather than privacy ones.
        return "local"

    @property
    def dimensions(self) -> int:
        return self._dimensions

    async def embed(self, texts: list[str]) -> list[list[float]]:
        """One request for the whole batch.

        Not a loop of single-text calls: every provider rate-limits and
        charges per request, and the difference between one round trip
        and five hundred is the difference between a usable ingest and an
        overnight one.
        """
        if not texts:
            # An empty batch is a valid thing for a caller to hold, and a
            # request for zero embeddings is not. Returning early beats
            # sending Ollama an empty array and interpreting whatever it
            # says about it.
            return []

        try:
            response = await self._client.post(
                f"{self._base_url}/api/embed",
                json={"model": self._model, "input": texts},
            )
        except httpx.HTTPError as err:
            raise ProviderError(
                f"{self._id} unreachable: {err}", provider_id=self._id, retryable=True
            ) from err

        if response.status_code >= 400:
            raise ProviderError(
                f"{self._id} returned {response.status_code}: {response.text[:200]}",
                provider_id=self._id,
                # Same split as the chat adapter: a 404 here means the
                # model was never pulled, and retrying cannot pull it.
                retryable=response.status_code >= 500 or response.status_code == 429,
                status_code=response.status_code,
            )

        embeddings = response.json().get("embeddings")
        if not isinstance(embeddings, list) or len(embeddings) != len(texts):
            raise ProviderError(
                f"{self._id} returned {len(embeddings) if isinstance(embeddings, list) else 'no'} "
                f"vectors for {len(texts)} inputs. A short batch would silently "
                f"misalign every vector with its text from that point on.",
                provider_id=self._id,
                retryable=False,
            )

        for index, vector in enumerate(embeddings):
            if len(vector) != self._dimensions:
                raise ProviderError(
                    f"{self._id} returned a {len(vector)}-dimension vector for input "
                    f"{index}, expected {self._dimensions}. Storing this would either "
                    f"fail on insert or, worse, succeed and make every similarity "
                    f"score meaningless.",
                    provider_id=self._id,
                    retryable=False,
                )

        return [[float(value) for value in vector] for vector in embeddings]

    async def health_check(self) -> bool:
        try:
            response = await self._client.get(f"{self._base_url}/api/tags")
        except httpx.HTTPError:
            return False
        return response.status_code == 200
