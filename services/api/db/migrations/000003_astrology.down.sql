-- Reverses 000003_astrology.up.sql.
--
-- A real down, tested, per .claude/rules/database.md — "revert the
-- commit" is not a rollback plan.
--
-- Dropped child-first so the foreign keys never block the drop, even
-- though CASCADE would handle it: relying on CASCADE here would also
-- silently succeed if the order were wrong, which is not the property we
-- want from a rollback.
--
-- pg_trgm and vector are NOT dropped. They arrived in 000001 and are
-- used by other phases; removing an extension because one migration
-- happened to need it is how a rollback takes out an unrelated feature.

DROP TABLE IF EXISTS places;
DROP TABLE IF EXISTS transits;
DROP TABLE IF EXISTS dashas;
DROP TABLE IF EXISTS charts;
DROP TABLE IF EXISTS birth_profiles;
