-- Reverses 000008_knowledge_checksum.up.sql.
--
-- Dropping the column loses the link between stored rows and the files
-- they came from, so the next `ingest-kb` run re-embeds the whole corpus.
-- That costs a full pass through the embedding model — minutes at the
-- Phase 5 corpus size — and no data: the corpus itself lives in
-- `packages/content/knowledge/` and is in git.
--
-- The index goes with the column; naming it anyway so the order is
-- readable rather than implied.

DROP INDEX IF EXISTS kd_checksum_idx;

ALTER TABLE knowledge_documents
    DROP COLUMN IF EXISTS source_checksum;
