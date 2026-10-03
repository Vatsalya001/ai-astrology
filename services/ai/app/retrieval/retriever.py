"""Hybrid retrieval: vector + keyword, filtered, boosted, capped.

PHASE-05 §4. Executed via asyncpg on the read-only role.
"""

from __future__ import annotations

import json
import logging
from dataclasses import dataclass, field
from typing import Any

import asyncpg

from app.providers.base import EmbeddingProvider
from app.retrieval.query import (
    RetrievedChunk,
    build_tsquery,
    combine_scores,
    normalise_keyword_score,
    normalise_vector_score,
    order_chunks,
    placement_filters,
)
from app.settings import settings

log = logging.getLogger(__name__)

# How many candidates each half of the hybrid contributes before merging.
#
# §4's SQL uses 20 per half. Larger than `rag_top_k` on purpose: the merge
# reranks, so a chunk that is 15th by vector score and 2nd by keyword score
# has to be present in both candidate sets to be found. Taking only
# `top_k` from each half would make the reranking unable to promote
# anything.
DEFAULT_CANDIDATE_LIMIT = 20


@dataclass(frozen=True)
class RetrievalFilter:
    """What to narrow the corpus by before ranking.

    §4: "Retrieval narrows by these BEFORE ranking, so a career question
    does not compete against nakshatra material it can never use."
    """

    category: str | None = None
    language: str = "en"

    # Chart facts used for the metadata boost. Shape is documented in
    # `placement_filters`.
    facts: dict[str, Any] = field(default_factory=dict)


class KnowledgeRetriever:
    """Reads the knowledge base. Never writes to it.

    ── Why this holds a pool rather than taking a connection ──

    Retrieval runs once per chat message on the request path, and
    connecting per request costs a round trip plus TLS. The pool is
    created once at startup and closed at shutdown.

    The pool is built from `ai_database_url`, which is the `astro_ro`
    role. That is the single-writer rule (ADR-001), and it is enforced by
    Postgres grants rather than by this class being careful — a bug here
    that tried to INSERT would get a permission error, not a row.
    """

    def __init__(
        self,
        *,
        pool: asyncpg.Pool,
        embedder: EmbeddingProvider,
        candidate_limit: int = DEFAULT_CANDIDATE_LIMIT,
    ) -> None:
        self._pool = pool
        self._embedder = embedder

        # A parameter rather than the bare constant so a test can shrink
        # it — and that is not a convenience. With a fixture corpus
        # smaller than the limit, both halves return EVERY document, the
        # two candidate sets are identical, and the FULL OUTER JOIN
        # becomes indistinguishable from any other join. A mutation
        # changing it to a LEFT JOIN — which loses every keyword-only hit
        # in production — broke no test until this was injectable.
        self._candidate_limit = candidate_limit

    @classmethod
    async def connect(
        cls, *, embedder: EmbeddingProvider, dsn: str | None = None
    ) -> KnowledgeRetriever:
        pool = await asyncpg.create_pool(
            dsn or settings.ai_database_url,
            min_size=1,
            # Small. This service makes one retrieval query per chat
            # message and the queries are fast; a large pool would hold
            # connections the database could give to the writer.
            max_size=5,
            # A retrieval that cannot complete in this long has already
            # lost — the user is waiting on the first token.
            command_timeout=10.0,
        )
        return cls(pool=pool, embedder=embedder)

    async def close(self) -> None:
        await self._pool.close()

    async def retrieve(
        self,
        question: str,
        *,
        filters: RetrievalFilter | None = None,
        top_k: int | None = None,
    ) -> list[RetrievedChunk]:
        """Find the chunks most relevant to a question.

        Returns at most `top_k` chunks, each scoring above
        `RAG_MIN_SCORE`, ordered by combined score.
        """
        filters = filters or RetrievalFilter()
        limit = top_k or settings.rag_top_k

        vectors = await self._embedder.embed([question])
        if not vectors:
            # An embedder that returns nothing for one text is broken, and
            # continuing would run a keyword-only search under a name that
            # claims to be hybrid.
            raise RuntimeError("the embedding provider returned no vector for the query")
        embedding = _format_vector(vectors[0])

        tsquery = build_tsquery(question)

        rows = await self._pool.fetch(
            _HYBRID_SQL,
            embedding,
            tsquery,
            filters.category,
            filters.language,
            self._candidate_limit,
        )

        boost_filters = placement_filters(filters.facts)
        chunks = [self._score(row, boost_filters) for row in rows]

        kept = order_chunks(
            [chunk for chunk in chunks if chunk.combined_score >= settings.rag_min_score]
        )

        # Counts and scores only — never the question, never the chunk
        # text. `.claude/rules/security.md`: no message content in logs.
        log.info(
            "retrieved",
            extra={
                "candidates": len(chunks),
                "kept": len(kept[:limit]),
                "dropped_below_min_score": len(chunks) - len(kept),
                "keyword_terms": tsquery.count("|") + 1 if tsquery else 0,
                "boost_filters": len(boost_filters),
            },
        )

        return kept[:limit]

    def _score(self, row: asyncpg.Record, boost_filters: list[dict[str, Any]]) -> RetrievedChunk:
        metadata = _decode_metadata(row["metadata"])

        boosted = any(_contains(metadata, wanted) for wanted in boost_filters)
        boost = settings.rag_metadata_boost if boosted else 1.0

        # Normalised before weighting. The raw values are not on the
        # same scale — cosine sits in [0.45, 0.85] and `ts_rank` in
        # [0, 0.06] — so combining them directly makes the keyword half
        # contribute about 4% of the score and promote nothing. See the
        # note above `VECTOR_BASELINE` in query.py.
        vector_score = normalise_vector_score(float(row["vector_score"] or 0.0))
        keyword_score = normalise_keyword_score(float(row["keyword_score"] or 0.0))

        return RetrievedChunk(
            chunk_id=str(row["id"]),
            document_id=str(row["document_id"]),
            document_title=row["title"],
            category=row["category"],
            source=row["source"],
            authority=int(row["authority"]),
            content=row["content"],
            metadata=metadata,
            vector_score=vector_score,
            keyword_score=keyword_score,
            combined_score=combine_scores(
                vector_score=vector_score,
                keyword_score=keyword_score,
                vector_weight=settings.rag_vector_weight,
                keyword_weight=settings.rag_keyword_weight,
                boost=boost,
            ),
            boosted=boosted,
        )


def _contains(metadata: dict[str, Any], wanted: dict[str, Any]) -> bool:
    """Postgres `@>` containment, in Python.

    Applied here rather than in the SQL because the boost is disjunctive
    over a variable number of placements — a career question for a native
    with nine planets produces well over a dozen predicates, and building
    that into the query would mean a different query string per request
    and therefore no statement caching.

    It is also where the lowercase normalisation in `placement_filters`
    pays off: `@>` is an exact match on both sides, and so is this.
    """
    for key, value in wanted.items():
        if key not in metadata:
            return False
        held = metadata[key]
        if isinstance(held, list):
            if value not in held:
                return False
        elif held != value:
            return False
    return True


def _decode_metadata(raw: Any) -> dict[str, Any]:
    """asyncpg returns jsonb as a string unless a codec is registered.

    Decoded defensively rather than assuming: a retrieval that treated the
    JSON string as a dict would find no keys, the boost would never fire,
    and nothing would error — the metadata boost would simply be dead.
    `TestTheMetadataBoostActuallyFires` is what says it is not.
    """
    if isinstance(raw, dict):
        return raw
    if isinstance(raw, (str, bytes)):
        try:
            decoded = json.loads(raw)
        except (ValueError, TypeError):
            return {}
        return decoded if isinstance(decoded, dict) else {}
    return {}


def _format_vector(vector: list[float]) -> str:
    """pgvector's text input form, `[0.1,0.2,…]`.

    The same representation the Go ingester writes, and for the same
    reason: neither side needs a vector type in its own language, and
    Postgres does the parsing and the width validation.
    """
    return "[" + ",".join(repr(float(value)) for value in vector) + "]"


# ── The hybrid query ──
#
# §4's SQL, with three deliberate differences, each of which was a bug in
# the version that copied it verbatim:
#
# 1. `to_tsquery($2)` with an OR-joined query, not `plainto_tsquery`.
#    See app/retrieval/query.py — plainto ANDs, so the keyword half
#    matched nothing on any multi-word question.
#
# 2. The keyword half is filtered by category and language too. §4 filters
#    only the vector half, which lets a keyword hit from an unrelated
#    category into the candidate set and past the `FULL OUTER JOIN`.
#
# 3. `$2 <> ''` guards the keyword half — but NOT for the reason first
#    written here. The comment claimed an empty tsquery is a syntax
#    error. It is not: Postgres emits
#
#        NOTICE: text-search query doesn't contain lexemes: ""
#
#    and returns an empty query that matches nothing. Checked by running
#    it rather than by reading the documentation, after a mutation
#    removing the guard broke no test.
#
#    So the guard is not a correctness guard. It is kept because a
#    question made entirely of stop words is common — "what is it?" —
#    and without it every such request writes a NOTICE into the Postgres
#    log and runs a GIN scan for a query that cannot match. Neither is a
#    bug; both are waste.
#
# The join is a FULL OUTER JOIN so a chunk found by either half survives;
# scoring and the final ordering happen in Python, where the metadata
# boost lives.
_HYBRID_SQL = """
WITH filtered AS (
    SELECT c.id, c.document_id, c.content, c.metadata, c.embedding, c.tsv,
           d.title, d.category, d.source, d.authority
    FROM knowledge_chunks c
    JOIN knowledge_documents d ON d.id = c.document_id
    WHERE d.is_active
      AND d.language = $4
      AND ($3::text IS NULL OR d.category = $3)
),
vec AS (
    SELECT id, 1 - (embedding <=> $1::vector) AS score
    FROM filtered
    WHERE embedding IS NOT NULL
    ORDER BY embedding <=> $1::vector
    LIMIT $5
),
kw AS (
    SELECT id, ts_rank(tsv, to_tsquery('english', $2)) AS score
    FROM filtered
    WHERE $2 <> '' AND tsv @@ to_tsquery('english', $2)
    ORDER BY score DESC
    LIMIT $5
)
SELECT f.id, f.document_id, f.content, f.metadata,
       f.title, f.category, f.source, f.authority,
       COALESCE(vec.score, 0) AS vector_score,
       COALESCE(kw.score, 0)  AS keyword_score
FROM vec
FULL OUTER JOIN kw USING (id)
JOIN filtered f USING (id)
"""
