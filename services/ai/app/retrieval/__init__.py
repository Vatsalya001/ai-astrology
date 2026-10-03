"""Hybrid retrieval over the astrology knowledge base.

PHASE-05 task 5.6. Reads `knowledge_documents` and `knowledge_chunks` as
`astro_ro` — SELECT only, enforced by Postgres grants (ADR-001). This is
the one place Python touches the database directly.
"""

from app.retrieval.query import (
    RetrievedChunk,
    build_tsquery,
    combine_scores,
    normalise_keyword_score,
    normalise_vector_score,
    order_chunks,
)
from app.retrieval.retriever import KnowledgeRetriever, RetrievalFilter

__all__ = [
    "KnowledgeRetriever",
    "RetrievalFilter",
    "RetrievedChunk",
    "build_tsquery",
    "combine_scores",
    "normalise_keyword_score",
    "normalise_vector_score",
    "order_chunks",
]
