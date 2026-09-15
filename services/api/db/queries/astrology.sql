-- Astrology queries. See docs/specs/PHASE-02-ASTROLOGY-ENGINE.md §3.
--
-- Every read of a birth profile or a chart is scoped by user_id in the
-- SQL itself, not by a check in the handler. A chart is among the most
-- sensitive objects in the system, and a predicate in the query is a
-- guarantee the caller cannot forget to apply.

-- ─── birth profiles ──────────────────────────────────────────────────

-- name: CreateBirthProfile :one
INSERT INTO birth_profiles (
    user_id, label, birth_date, birth_time, time_accuracy,
    birth_place, latitude, longitude, timezone, utc_offset_min, utc_instant,
    source, version
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;

-- name: ListActiveBirthProfiles :many
SELECT * FROM birth_profiles
WHERE user_id = $1 AND is_active = TRUE
ORDER BY created_at;

-- name: GetBirthProfile :one
-- Scoped by user. A caller asking for someone else's profile gets no
-- row, which the handler turns into a 404 — never a 403, because a 403
-- confirms the row exists.
SELECT * FROM birth_profiles
WHERE id = $1 AND user_id = $2;

-- name: ListBirthProfileVersions :many
-- The history behind one profile, newest first.
--
-- Walks superseded_by BACKWARDS from the given id: each step finds the
-- version that this one replaced. Callers ask for the history of the
-- profile they are looking at, which is the active one, which is the
-- newest — so walking back reaches every earlier version.
--
-- Every column reference is qualified. An unqualified `id` here is
-- ambiguous between the CTE and the table, and sqlc rejects it, which is
-- the right moment to find out rather than at runtime.
WITH RECURSIVE lineage AS (
    SELECT bp.* FROM birth_profiles bp
    WHERE bp.id = $1 AND bp.user_id = $2
  UNION ALL
    SELECT prev.* FROM birth_profiles prev
    JOIN lineage l ON prev.superseded_by = l.id
)
SELECT * FROM lineage ORDER BY version DESC;

-- name: SupersedeBirthProfile :one
-- Marks a version replaced. Returns the row so the caller can tell
-- "superseded it" from "there was nothing to supersede" without a second
-- query — the same reason RevokeSession became :execrows in Phase 1.
UPDATE birth_profiles
SET superseded_by = $2, is_active = FALSE
WHERE id = $1 AND user_id = $3 AND is_active = TRUE
RETURNING *;

-- name: DeactivateBirthProfile :execrows
UPDATE birth_profiles
SET is_active = FALSE
WHERE id = $1 AND user_id = $2 AND is_active = TRUE;

-- ─── charts ──────────────────────────────────────────────────────────

-- name: UpsertChart :one
-- Keyed on every input that affects the output. Recomputing with the
-- same inputs replaces the data in place; changing the ayanamsa creates
-- a different row rather than overwriting an unrelated chart.
INSERT INTO charts (
    birth_profile_id, chart_type, calculation_system, ayanamsa,
    house_system, engine_version, chart_data
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (birth_profile_id, chart_type, calculation_system, ayanamsa, house_system)
DO UPDATE SET
    engine_version = EXCLUDED.engine_version,
    chart_data     = EXCLUDED.chart_data,
    computed_at    = now()
RETURNING *;

-- name: GetChart :one
-- Joined to birth_profiles so ownership is enforced in the same
-- statement that fetches the data. A separate ownership check is a
-- check someone can forget.
SELECT c.* FROM charts c
JOIN birth_profiles p ON p.id = c.birth_profile_id
WHERE c.birth_profile_id = $1
  AND c.chart_type = $2
  AND c.calculation_system = $3
  AND c.ayanamsa = $4
  AND c.house_system = $5
  AND p.user_id = $6;

-- name: ListChartsForProfile :many
SELECT c.* FROM charts c
JOIN birth_profiles p ON p.id = c.birth_profile_id
WHERE c.birth_profile_id = $1 AND p.user_id = $2
ORDER BY c.chart_type;

-- name: ListChartsByEngineVersion :many
-- Finds charts built by an older engine, so a library upgrade can be
-- followed by a targeted recompute instead of a guess.
SELECT * FROM charts
WHERE engine_version <> $1
ORDER BY computed_at
LIMIT $2;

-- ─── dashas ──────────────────────────────────────────────────────────

-- name: InsertDasha :one
INSERT INTO dashas (chart_id, system, planet, start_date, end_date, level, parent_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: DeleteDashasForChart :execrows
-- Recomputing replaces the whole tree. Deleting first keeps it a tree
-- rather than two overlapping generations of one.
DELETE FROM dashas WHERE chart_id = $1;

-- name: ListDashasByLevel :many
SELECT d.* FROM dashas d
JOIN charts c ON c.id = d.chart_id
JOIN birth_profiles p ON p.id = c.birth_profile_id
WHERE d.chart_id = $1 AND d.level = $2 AND p.user_id = $3
ORDER BY d.start_date;

-- name: FindDashaAt :many
-- Which periods were running at a given instant — one per level, so a
-- caller gets the Maha, Antar and Pratyantar in a single round trip.
--
-- Half-open interval: start <= t < end. A closed interval would return
-- two rows on the boundary instant, since one period's end is the next
-- one's start.
SELECT d.* FROM dashas d
JOIN charts c ON c.id = d.chart_id
JOIN birth_profiles p ON p.id = c.birth_profile_id
WHERE d.chart_id = $1
  AND p.user_id = $2
  AND d.start_date <= @at
  AND d.end_date > @at
ORDER BY d.level;

-- ─── transits ────────────────────────────────────────────────────────

-- name: UpsertTransit :one
INSERT INTO transits (planet, sign, degree, is_retrograde, timestamp, calculation_system, ayanamsa, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (planet, timestamp, calculation_system, ayanamsa)
DO UPDATE SET
    sign          = EXCLUDED.sign,
    degree        = EXCLUDED.degree,
    is_retrograde = EXCLUDED.is_retrograde,
    metadata      = EXCLUDED.metadata
RETURNING *;

-- name: ListTransitsAt :many
-- Global and free of personal data, so no user scoping — and that is
-- what makes them safe to cache across all users.
SELECT DISTINCT ON (planet) *
FROM transits
WHERE timestamp <= $1 AND calculation_system = $2 AND ayanamsa = $3
ORDER BY planet, timestamp DESC;

-- name: DeleteTransitsBefore :execrows
DELETE FROM transits WHERE timestamp < $1;

-- ─── places ──────────────────────────────────────────────────────────

-- name: SearchPlaces :many
-- Prefix match, ranked by population — which is what makes "jaip"
-- return Jaipur, Rajasthan rather than a village of 600 people. The
-- ranking matters more than the matching at the highest drop-off point
-- in the product.
SELECT * FROM places
WHERE ascii_name ILIKE $1 || '%'
ORDER BY population DESC, ascii_name
LIMIT $2;

-- name: GetPlace :one
SELECT * FROM places WHERE id = $1;

-- name: UpsertPlace :exec
-- Keyed on the GeoNames id, so re-running the importer updates rows
-- instead of duplicating them.
INSERT INTO places (id, name, ascii_name, admin1, country_code, latitude, longitude, timezone, population)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (id) DO UPDATE SET
    name         = EXCLUDED.name,
    ascii_name   = EXCLUDED.ascii_name,
    admin1       = EXCLUDED.admin1,
    country_code = EXCLUDED.country_code,
    latitude     = EXCLUDED.latitude,
    longitude    = EXCLUDED.longitude,
    timezone     = EXCLUDED.timezone,
    population   = EXCLUDED.population;

-- name: CountPlaces :one
SELECT count(*) FROM places;
