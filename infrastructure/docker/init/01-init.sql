-- ════════════════════════════════════════════════════════════════
--  Database bootstrap. Runs once, on first container start.
--
--  Two things happen here, and both are load-bearing:
--    1. Extensions the application depends on.
--    2. The read-only role for ai-service.
--
--  See ADR-001 (service topology) and ADR-002 (postgres + pgvector).
-- ════════════════════════════════════════════════════════════════

-- pgvector: embeddings for RAG (Phase 5) and memory (Phase 6).
CREATE EXTENSION IF NOT EXISTS vector;

-- pg_trgm: fuzzy place-name search (Phase 2).
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- ────────────────────────────────────────────────────────────────
--  THE SINGLE-WRITER RULE
--
--  Only api-service (Go) writes to this database. ai-service reads.
--  astro-service gets no database access at all.
--
--  This is enforced by Postgres grants rather than by code review,
--  because a grant cannot be forgotten during a refactor.
-- ────────────────────────────────────────────────────────────────
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'astro_ro') THEN
        CREATE ROLE astro_ro LOGIN PASSWORD 'astro_ro';
    END IF;
END
$$;

GRANT CONNECT ON DATABASE astro_dev TO astro_ro;
GRANT USAGE ON SCHEMA public TO astro_ro;

-- Existing tables (none yet at bootstrap, but harmless and correct).
GRANT SELECT ON ALL TABLES IN SCHEMA public TO astro_ro;

-- Future tables created by the migration user get SELECT automatically.
ALTER DEFAULT PRIVILEGES FOR ROLE astro IN SCHEMA public
    GRANT SELECT ON TABLES TO astro_ro;

-- Explicitly withhold write capability. Belt and braces: the default
-- privileges above already omit these, but stating it makes the intent
-- unmistakable to the next person reading this file.
REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON ALL TABLES IN SCHEMA public FROM astro_ro;
ALTER DEFAULT PRIVILEGES FOR ROLE astro IN SCHEMA public
    REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON TABLES FROM astro_ro;
