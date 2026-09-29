"""Batch embeddings, for Go's knowledge-base ingester.

PHASE-05 task 5.3. The single-writer rule (ADR-001) puts ingestion in Go
— it inserts documents and chunks in one transaction — but embedding
needs a model, and models live here. So Go chunks deterministically,
posts the batch, and writes what comes back.

── Why this is a route and not a library ──

Because the alternative is a Python embedding library imported into Go,
which does not exist, or a subprocess, which is a worse HTTP call. §4 is
explicit: "Go orchestrates, Python embeds."

── Why the dimension is echoed in the response ──

The caller is about to insert into a `vector(768)` column. A width
mismatch either fails on insert — loud and fine — or lands in a column
that happens to accept it, after which every similarity score in the
product is meaningless and nothing errors. Returning the width lets the
ingester check before it writes, rather than trusting two config files to
agree.
"""

from __future__ import annotations

import logging
import time

from fastapi import APIRouter
from pydantic import BaseModel, Field

from app.providers import ProviderError
from app.providers.factory import embedding_from_settings
from app.settings import settings

router = APIRouter(tags=["embeddings"], prefix="/v1")

log = logging.getLogger(__name__)

# One request's worth of text.
#
# Bounded because this endpoint is reachable by anything holding the
# internal token, and an unbounded batch is a way to hold the embedding
# model for an arbitrary time — the same reasoning as the PDF render
# limit in Phase 3. 256 is comfortably above `KB_EMBED_BATCH_SIZE=64`
# from §9, so the ingester's configured batch cannot trip it by
# accident.
MAX_BATCH = 256

# Per text. A knowledge chunk is ~350 tokens by §4, so this is generous
# by an order of magnitude; it exists to stop a single pathological input
# rather than to shape normal use.
MAX_CHARS = 8000


class EmbedRequest(BaseModel):
    texts: list[str] = Field(min_length=1, max_length=MAX_BATCH)

    model_config = {"extra": "forbid"}


class EmbedResponse(BaseModel):
    """Vectors, plus what produced them.

    `model` and `dimensions` are not decoration. The ingester writes
    `embedding_model` into every chunk row, and migration 000007's
    comment explains why: a corpus embedded across a model change is
    unfixable without it, because nothing in the data says which rows
    need redoing.
    """

    embeddings: list[list[float]]
    model: str
    dimensions: int
    provider_id: str
    latency_ms: int


@router.post("/embed", response_model=EmbedResponse)
async def embed(body: EmbedRequest) -> EmbedResponse:
    """Embed a batch in one call.

    Not a loop of single-text requests: every provider rate-limits per
    request, and a 600-document corpus at one call each is both slow and
    the fastest way to get throttled.
    """
    for index, text in enumerate(body.texts):
        if not text.strip():
            # Refused rather than embedded. A blank chunk gets a vector
            # that is arbitrary but confidently close to something, so it
            # becomes a result for queries it has nothing to do with —
            # and it is invisible in the corpus, because the row looks
            # fine. The chunker should never produce one; this is the
            # assertion that says so.
            raise _bad_request(f"texts[{index}] is empty or whitespace only")
        if len(text) > MAX_CHARS:
            raise _bad_request(
                f"texts[{index}] is {len(text)} characters, over the {MAX_CHARS} limit"
            )

    provider = embedding_from_settings()
    started = time.monotonic()

    try:
        vectors = await provider.embed(body.texts)
    except ProviderError as err:
        # 503 with the provider named, not 500. The caller is an
        # ingestion job that can retry, and PHASE-04 §2's contract is
        # that an unavailable provider is a retryable 503 rather than an
        # internal error.
        raise _unavailable(str(err)) from err

    latency_ms = int((time.monotonic() - started) * 1000)

    if len(vectors) != len(body.texts):
        # Provider-side truncation. Silent misalignment is the dangerous
        # failure: the ingester would zip vectors against chunks by
        # position and attach every embedding to the wrong text, which
        # produces a corpus that retrieves confidently and wrongly.
        raise _unavailable(f"provider returned {len(vectors)} vectors for {len(body.texts)} texts")

    # Counts and widths only — never the text. `.claude/rules/security.md`:
    # log IDs, not objects. A knowledge chunk is not user data, but this
    # route has no way to know its caller only ever sends corpus text.
    log.info(
        "embedded",
        extra={
            "count": len(body.texts),
            "dimensions": provider.dimensions,
            "provider": provider.id,
            "latency_ms": latency_ms,
        },
    )

    return EmbedResponse(
        embeddings=vectors,
        model=settings.embedding_model,
        dimensions=provider.dimensions,
        provider_id=provider.id,
        latency_ms=latency_ms,
    )


def _bad_request(detail: str) -> Exception:
    from fastapi import HTTPException

    return HTTPException(status_code=422, detail=detail)


def _unavailable(detail: str) -> Exception:
    from fastapi import HTTPException

    return HTTPException(status_code=503, detail=detail)
