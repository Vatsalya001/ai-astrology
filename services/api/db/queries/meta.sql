-- Queries against schema_meta.
--
-- Small on purpose: Phase 0 has no domain tables. This exists so the
-- sqlc pipeline is wired and proven end to end before Phase 1 needs it,
-- rather than being set up under time pressure alongside auth.

-- name: GetSchemaMeta :one
SELECT * FROM schema_meta WHERE id = 1;

-- name: SetSchemaPhase :one
UPDATE schema_meta
SET phase = $1,
    updated_at = now()
WHERE id = 1
RETURNING *;
