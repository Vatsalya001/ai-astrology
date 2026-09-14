-- Reverse of 000002_auth.up.sql.
--
-- A real down, not a placeholder: the project's database rules say
-- "revert the commit" is not a rollback plan, and an untested down is
-- discovered to be broken at exactly the wrong moment.
--
-- Order matters. Dropping a type while a column still uses it fails, so
-- every table referencing an enum goes first. Tables are dropped in
-- reverse dependency order even though CASCADE would handle it —
-- relying on CASCADE hides which foreign keys actually exist.

DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS auth_identities;
DROP TABLE IF EXISTS user_preferences;
DROP TABLE IF EXISTS users;

-- Only now, once no column references them.
DROP TYPE IF EXISTS auth_provider;
DROP TYPE IF EXISTS user_role;
