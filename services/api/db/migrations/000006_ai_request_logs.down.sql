-- Reverses 000006_ai_request_logs.up.sql.
--
-- A real down, tested by migrations_test.go, which rolls every migration
-- back and then forward again — see .claude/rules/database.md.
--
-- Dropping the table drops its indexes with it, so they are not listed.
-- The foreign key to users points OUT of this table rather than into
-- it, so nothing else blocks the drop.
--
-- Worth stating what this destroys: every recorded AI cost. Phase 7
-- bills from this table, so a rollback in production after billing has
-- started is a data-loss event, not a schema change. Take a dump first.

DROP TABLE IF EXISTS ai_request_logs;
