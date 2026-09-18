-- Reverses 000005_sade_sati_windows.up.sql.
--
-- A real down, tested by migrations_test.go, which rolls every migration
-- back and then forward again — see .claude/rules/database.md.
--
-- Nothing references this table, so there is no ordering to get right.
-- Stated rather than left to inference: a later phase pointing a foreign
-- key at it would need its drop to come first here, or the rollback
-- blocks on the constraint.

DROP TABLE IF EXISTS sade_sati_windows;
