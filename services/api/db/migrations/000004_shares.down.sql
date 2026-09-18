-- Reverses 000004_shares.up.sql.
--
-- A real down, tested, per .claude/rules/database.md — "revert the
-- commit" is not a rollback plan.
--
-- Dropping the table takes its three indexes with it, so they are not
-- named individually: an explicit DROP INDEX for an index that
-- PostgreSQL has already removed would fail the rollback at the very
-- moment it is being relied on.
--
-- Nothing references chart_shares, so there is no ordering to get right
-- here. That is worth stating rather than leaving to inference: if a
-- later phase adds a table that points at this one, this file has to
-- grow a drop for it FIRST, or the rollback will block on the foreign
-- key.

DROP TABLE IF EXISTS chart_shares;
