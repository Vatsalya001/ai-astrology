-- Reverses 000009_conversations.up.sql.
--
-- A real down, exercised by migrations_test.go, which applies every
-- migration, rolls the whole set back in reverse order and then applies
-- it forward again — see `.claude/rules/database.md`. The second forward
-- pass is what tests this file rather than the parser: a down that drops
-- nothing exits zero, and the re-apply then fails on an object that
-- still exists.
--
-- ── Order, and why it is not just tidiness ──
--
-- The `ai_request_logs` constraint comes first and MUST. It points at
-- `conversations` from a table this migration does not own and does not
-- drop, so `DROP TABLE conversations` would fail outright — Postgres
-- refuses to drop a table another table's foreign key depends on
-- without CASCADE, and reaching for CASCADE here would silently drop
-- the constraint on the cost log and leave no record that it had been
-- there.
--
-- The three tables then go leaf-first. `ON DELETE CASCADE` means
-- dropping `conversations` alone would also work, but naming the order
-- makes the dependency readable instead of relying on a cascade to tidy
-- up — the same choice 000007 made. Indexes go with their tables and
-- are not listed; the GRANTs go with them too, since Postgres removes
-- privileges when the object does.
--
-- ── What this destroys ──
--
-- Every conversation, every message, and every stored context. Unlike
-- 000007's corpus, which `cmd/ingest-kb` rebuilds from files in git,
-- this is NOT recoverable: it is what users wrote and what the model
-- answered, and there is no source of truth for it anywhere else.
-- `message_contexts` in particular is the only record of which chart
-- facts and which knowledge chunks produced a given answer, so rolling
-- this back makes every past response permanently unexplainable even if
-- the text were restored from a backup.
--
-- Run it on a database with traffic only with a dump in hand.

ALTER TABLE ai_request_logs
    DROP CONSTRAINT IF EXISTS ai_request_logs_conversation_fk;

DROP INDEX IF EXISTS ai_logs_conversation_idx;

DROP TABLE IF EXISTS message_contexts;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS conversations;
