-- Reverses 000007_knowledge_base.up.sql.
--
-- A real down, tested by migrations_test.go, which rolls every migration
-- back and then forward again — see `.claude/rules/database.md`.
--
-- Chunks first, then documents. The foreign key is chunks → documents
-- with ON DELETE CASCADE, so dropping documents first would work, but
-- naming the order makes the dependency readable rather than relying on
-- a cascade to tidy up. Indexes go with their tables and are not listed.
--
-- The GRANTs are dropped with the tables they refer to; Postgres removes
-- privileges when the object goes.
--
-- ── What this destroys ──
--
-- The entire authored corpus and every embedding computed from it.
-- Re-running `cmd/ingest-kb` rebuilds both from
-- `packages/content/knowledge/`, which is the source of truth and is in
-- git — so this is recoverable, unlike the Phase 4 cost log next door.
-- It is not free: re-embedding the corpus costs a full pass through the
-- embedding model. Minutes at the Phase 5 corpus size, which is exactly
-- why §4 says to write the dimension migration now rather than at 100k
-- chunks.

DROP TABLE IF EXISTS knowledge_chunks;
DROP TABLE IF EXISTS knowledge_documents;
