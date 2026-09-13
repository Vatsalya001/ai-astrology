# ADR-002 — PostgreSQL with pgvector, not a dedicated vector database

**Status:** accepted · 2026-09-13

## Decision

One PostgreSQL 16 instance with the `pgvector` extension serves both relational data
and embeddings. No Pinecone, Weaviate, Qdrant or Milvus.

## Context

Phase 5 needs vector similarity search over an astrology knowledge base (~400–600
documents, a few thousand chunks). Phase 6 adds embeddings for user memories.

## Reason

- **Scale doesn't justify it.** A dedicated vector DB starts earning its keep somewhere
  past ~1M vectors. We will have thousands.
- **One database to operate, back up and secure.** A second datastore means a second
  backup story, a second access-control model and a second thing to be down at 3am.
- **Retrieval here is hybrid, not pure vector.** Astrology queries contain exact
  entities — "Saturn", "7th house", "Rohini" — that keyword search nails and embeddings
  blur. A single SQL query can combine `pgvector` cosine distance with `tsvector`
  ranking and a JSONB metadata filter. Splitting the stores would mean joining results
  in application code.
- **Transactional consistency.** A memory and its embedding are written in one
  transaction. With a separate store they can diverge.

## Tradeoffs

- `pgvector` HNSW is slower than a purpose-built engine at very large scale
- Vector search competes with OLTP traffic for the same resources
- Index tuning (`ef_search`) is less sophisticated

Accepted, because none of them bite below roughly a million vectors.

## Revisit when

Chunk count approaches ~1M, or vector search measurably degrades OLTP latency. Phase 11
lists this as a measured scale signal rather than a guess.
