-- Reverses 000001_bootstrap.up.sql.
--
-- Every migration in this project has a real down. "Revert the commit"
-- is not a rollback plan.
--
-- The extensions are deliberately NOT dropped: other objects may depend
-- on them, and dropping an extension cascades. Removing them is a
-- manual, considered operation, not something a rollback should do
-- silently.

DROP TABLE IF EXISTS schema_meta;

ALTER DEFAULT PRIVILEGES IN SCHEMA public
    REVOKE SELECT ON TABLES FROM astro_ro;
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM astro_ro;
REVOKE USAGE ON SCHEMA public FROM astro_ro;

-- The role itself is left in place: it may own grants in other
-- databases, and DROP ROLE fails if it does. Removing it is a
-- deliberate operator action.
