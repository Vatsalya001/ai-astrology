-- Auth queries. See docs/specs/PHASE-01-AUTH-AND-USERS.md §3.

-- name: FindUserByEmail :one
SELECT * FROM users WHERE email = $1 AND status <> 'deleted';

-- name: FindUserByPhone :one
SELECT * FROM users WHERE phone = $1 AND status <> 'deleted';

-- name: FindUserByID :one
SELECT * FROM users WHERE id = $1 AND status <> 'deleted';

-- name: CreateUserWithEmail :one
INSERT INTO users (email, email_verified) VALUES ($1, TRUE) RETURNING *;

-- name: CreateUserWithPhone :one
INSERT INTO users (phone, phone_verified) VALUES ($1, TRUE) RETURNING *;

-- name: TouchLastLogin :exec
UPDATE users SET last_login_at = now(), updated_at = now() WHERE id = $1;

-- name: UpdateUserProfile :one
-- COALESCE so a PATCH omitting a field leaves it alone rather than
-- nulling it. Only name and gender are writable here; email and phone
-- change through a verification flow, never a profile edit.
UPDATE users
SET name       = COALESCE(sqlc.narg('name'), name),
    gender     = COALESCE(sqlc.narg('gender'), gender),
    updated_at = now()
WHERE id = sqlc.arg('id') AND status <> 'deleted'
RETURNING *;

-- ─── Identities ──────────────────────────────────────────────────────

-- name: FindIdentity :one
SELECT * FROM auth_identities WHERE provider = $1 AND provider_user_id = $2;

-- name: LinkIdentity :one
-- ON CONFLICT DO UPDATE rather than DO NOTHING: DO NOTHING returns no
-- row, so the caller cannot tell "already linked" from "insert failed"
-- without a second query.
INSERT INTO auth_identities (user_id, provider, provider_user_id)
VALUES ($1, $2, $3)
ON CONFLICT (provider, provider_user_id)
DO UPDATE SET user_id = auth_identities.user_id
RETURNING *;

-- ─── Preferences ─────────────────────────────────────────────────────

-- name: CreateDefaultPreferences :one
INSERT INTO user_preferences (user_id) VALUES ($1)
ON CONFLICT (user_id) DO UPDATE SET user_id = user_preferences.user_id
RETURNING *;

-- name: GetPreferences :one
SELECT * FROM user_preferences WHERE user_id = $1;

-- name: UpdatePreferences :one
UPDATE user_preferences
SET preferred_language        = COALESCE(sqlc.narg('preferred_language'), preferred_language),
    astrology_system          = COALESCE(sqlc.narg('astrology_system'), astrology_system),
    chart_style               = COALESCE(sqlc.narg('chart_style'), chart_style),
    theme                     = COALESCE(sqlc.narg('theme'), theme),
    notification_preferences  = COALESCE(sqlc.narg('notification_preferences'), notification_preferences),
    communication_preferences = COALESCE(sqlc.narg('communication_preferences'), communication_preferences)
WHERE user_id = sqlc.arg('user_id')
RETURNING *;

-- ─── Sessions ────────────────────────────────────────────────────────

-- name: CreateSession :one
INSERT INTO sessions (user_id, family_id, refresh_hash, user_agent, ip_hash, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: RotateRefreshToken :one
-- The single most important statement in this phase.
--
-- Check and update in ONE statement. Two concurrent refreshes presenting
-- the same token: Postgres serialises the UPDATE, so exactly one matches
-- `used_at IS NULL` and gets a row back. The other gets no rows, which
-- the caller treats as reuse and revokes the family.
--
-- A read-then-write in Go would let both pass the check before either
-- wrote — the classic TOCTOU that makes a leaked token usable twice. No
-- application lock is needed, and a mocked database cannot test this.
UPDATE sessions
SET used_at = now()
WHERE refresh_hash = $1
  AND used_at IS NULL
  AND revoked_at IS NULL
  AND expires_at > now()
RETURNING *;

-- name: FindSessionByHash :one
-- Used only on the reuse path, to find which family to revoke. Deliberately
-- ignores used_at/revoked_at: the whole point is to locate a token that
-- has already been spent.
SELECT * FROM sessions WHERE refresh_hash = $1;

-- name: RevokeTokenFamily :exec
-- One leaked token invalidates every descendant of it. Cheap to run and
-- it turns a silent compromise into a forced re-authentication.
UPDATE sessions SET revoked_at = now()
WHERE family_id = $1 AND revoked_at IS NULL;

-- name: RevokeSession :execrows
-- :execrows, not :exec. An :exec cannot distinguish "revoked it" from
-- "matched nothing", so revoking someone else's session — which the
-- user_id predicate correctly refuses — returned 204 and told the caller
-- it had worked. The row count is what lets the handler answer 404.
UPDATE sessions SET revoked_at = now()
WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL;

-- name: RevokeAllUserSessions :exec
UPDATE sessions SET revoked_at = now()
WHERE user_id = $1 AND revoked_at IS NULL;

-- name: ListActiveSessions :many
SELECT * FROM sessions
WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
ORDER BY created_at DESC;

-- name: DeleteExpiredSessions :exec
-- Housekeeping for the worker. Expired rows prove nothing and grow
-- forever.
DELETE FROM sessions WHERE expires_at < now() - INTERVAL '30 days';

-- ─── Deletion ────────────────────────────────────────────────────────

-- name: RequestUserDeletion :one
UPDATE users
SET deletion_requested_at = now(), status = 'deleted', updated_at = now()
WHERE id = $1 AND deletion_requested_at IS NULL
RETURNING *;

-- name: CancelUserDeletion :one
UPDATE users
SET deletion_requested_at = NULL, status = 'active', updated_at = now()
WHERE id = $1 AND deletion_requested_at IS NOT NULL
RETURNING *;

-- name: ListUsersPastDeletionGrace :many
SELECT * FROM users
WHERE deletion_requested_at IS NOT NULL
  AND deletion_requested_at < $1;

-- name: HardDeleteUser :exec
-- Real deletion, not a flag. Every user-owned table declares ON DELETE
-- CASCADE, so this removes the lot — which is what the Phase 1 gate
-- means by "no row anywhere references the user".
--
-- audit_logs is the deliberate exception: its user_id is not a foreign
-- key, so the record that a deletion happened survives the deletion. It
-- holds IDs and enums only, never PII.
DELETE FROM users WHERE id = $1;

-- ─── Audit ───────────────────────────────────────────────────────────

-- name: WriteAuditLog :exec
INSERT INTO audit_logs (user_id, action, metadata, ip_hash)
VALUES ($1, $2, $3, $4);

-- name: ListAuditLogsForUser :many
SELECT * FROM audit_logs WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2;

-- name: ListIdentitiesForUser :many
-- Needed by the data export. Without it the export declares an
-- auth_identities field and always returns [], which is worse than
-- omitting it: it tells the user there are none.
SELECT * FROM auth_identities WHERE user_id = $1 ORDER BY created_at;
