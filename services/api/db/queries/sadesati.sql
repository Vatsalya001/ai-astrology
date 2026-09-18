-- Sade Sati windows. See docs/specs/PHASE-03-KUNDLI-UI.md §15.
--
-- Twelve rows, one per natal Moon sign, refreshed by the worker. Read on
-- the request path by primary key, so the answer survives astro-service
-- being unreachable.

-- name: UpsertSadeSatiWindow :one
-- Idempotent on the sign, so a retried refresh — or two replicas that
-- both got through — rewrites the row rather than failing or doubling
-- it. The same reasoning as UpsertTransit.
INSERT INTO sade_sati_windows (
    moon_sign_index, moon_sign, started_at, ends_at, computed_for
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (moon_sign_index) DO UPDATE SET
    moon_sign    = EXCLUDED.moon_sign,
    started_at   = EXCLUDED.started_at,
    ends_at      = EXCLUDED.ends_at,
    computed_for = EXCLUDED.computed_for,
    computed_at  = NOW()
RETURNING *;

-- name: GetSadeSatiWindow :one
SELECT * FROM sade_sati_windows WHERE moon_sign_index = $1;

-- name: ListSadeSatiWindows :many
-- For the health probe and for anyone reading the table by hand.
SELECT * FROM sade_sati_windows ORDER BY moon_sign_index;
