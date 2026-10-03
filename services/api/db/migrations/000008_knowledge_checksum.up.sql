-- Phase 5 task 5.4 — pin each stored document to the bytes it came from.
--
-- `cmd/ingest-kb --check` already computes a SHA-256 over every authored
-- file. This is where that value lands, and it buys two things that are
-- otherwise impossible:
--
-- 1. **Re-runnable ingestion that is actually cheap.** §16 requires
--    ingestion to be "deterministic and re-runnable without duplicates".
--    The unique index on (title, language, version) already prevents the
--    duplicates, but without a checksum a re-run has to re-embed all 600
--    documents to find out that none of them changed. With one, a re-run
--    over an unchanged corpus is a single SELECT per document.
--
-- 2. **A way to ask whether the stored corpus is still the authored one.**
--    This matters more here than it would in most systems. The authored
--    markdown is a tracked file, so `scripts/check-integrity.sh` covers
--    it; the embeddings are 768 floats in Postgres, covered by nothing. A
--    flipped bit in a vector produces no error — every float is a
--    plausible float — and the only symptom is retrieval that is slightly
--    and unprovably worse. This machine has recorded sixteen single-bit
--    corruption events (see docs/PROJECT_STATUS.md), so that is an
--    observed failure mode rather than a hypothetical one.
--
-- ── Why DEFAULT '' and not NOT NULL without one ──
--
-- Migration 000007 may already have rows by the time this runs. An empty
-- checksum means "ingested before this column existed", which the ingester
-- treats as "re-ingest" — the safe direction. A NULL would mean the same
-- thing while forcing every read to handle it.

ALTER TABLE knowledge_documents
    ADD COLUMN source_checksum TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN knowledge_documents.source_checksum IS
    'SHA-256 of the authored file, whole file including front matter. '
    'Empty means unknown, which the ingester treats as needing re-ingestion.';

-- The ingester's first question for every document in the corpus:
-- "do you already hold this exact file?" Answered from the index.
--
-- Part of the key rather than a separate index on checksum alone: the
-- lookup is always by identity first, and a document whose checksum
-- matches some OTHER document's is not a hit.
CREATE INDEX kd_checksum_idx ON knowledge_documents (title, language, version, source_checksum);
