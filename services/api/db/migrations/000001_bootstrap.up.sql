-- Bootstrap: extensions and the read-only role.
--
-- These also exist in infrastructure/docker/init/01-init.sql, which is
-- what makes a fresh LOCAL container work. That script runs only on
-- first container start, so it cannot be relied on for staging or
-- production — a managed Postgres has no docker-entrypoint-initdb.d.
--
-- This migration is the authoritative path. Every statement is
-- idempotent so applying it to a database already bootstrapped by the
-- init script is a no-op.

-- pgvector: embeddings for RAG (Phase 5) and memory (Phase 6).
CREATE EXTENSION IF NOT EXISTS vector;

-- pg_trgm: fuzzy place-name search (Phase 2).
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- ────────────────────────────────────────────────────────────────
--  THE SINGLE-WRITER RULE (ADR-001)
--
--  Only api-service writes. ai-service reads. astro-service has no
--  database access at all.
--
--  Enforced by Postgres grants rather than by code review, because a
--  grant cannot be forgotten during a refactor.
--
--  Proven by services/api/internal/platform/db/singlewriter_test.go,
--  which runs this exact file against a real container.
-- ────────────────────────────────────────────────────────────────
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'astro_ro') THEN
        -- Password is overridden per environment. The literal here is
        -- only ever used by the local development container.
        CREATE ROLE astro_ro LOGIN PASSWORD 'astro_ro';
    END IF;
END
$$;

GRANT USAGE ON SCHEMA public TO astro_ro;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO astro_ro;

-- Tables created later by the migration user inherit SELECT.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT ON TABLES TO astro_ro;

-- Withhold write capability explicitly. The default privileges above
-- already omit these; stating it makes the intent unmistakable to the
-- next person reading this file.
REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON ALL TABLES IN SCHEMA public FROM astro_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON TABLES FROM astro_ro;

-- ────────────────────────────────────────────────────────────────
--  schema_meta: one row, recording what this database is.
--
--  Exists so there is something real for sqlc to generate against in
--  Phase 0, and so an operator can tell at a glance which application
--  and schema version a given database belongs to. Phase 1 adds the
--  first domain tables.
-- ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS schema_meta (
    id           SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    application  TEXT        NOT NULL,
    phase        TEXT        NOT NULL,
    bootstrapped_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO schema_meta (id, application, phase)
VALUES (1, 'ayana', '0 — Foundation')
ON CONFLICT (id) DO NOTHING;

GRANT SELECT ON schema_meta TO astro_ro;
