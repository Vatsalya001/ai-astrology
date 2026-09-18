-- Share-link queries. See docs/specs/PHASE-03-KUNDLI-UI.md task 3.16.
--
-- Two access paths, and they are deliberately asymmetric:
--
--   The OWNER's queries are scoped by user_id in the SQL, like every
--   other read in this service. A predicate in the query is a guarantee
--   the handler cannot forget to apply.
--
--   The VIEWER's query is scoped by nothing but the token hash, because
--   a viewer has no account. The token IS the authorisation — which is
--   why liveness (not expired, not revoked) is in the WHERE clause
--   rather than checked in Go afterwards. A dead link must return no
--   row, not a row the caller is trusted to inspect.

-- ─── creating and listing, for the owner ─────────────────────────────

-- name: CreateChartShare :one
INSERT INTO chart_shares (
    user_id, birth_profile_id, token_hash, scope, expires_at
) VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListChartShares :many
-- Every link this user has created for one profile, newest first.
-- Revoked and expired rows are included on purpose: "which links did I
-- make, and which are dead" is the question this answers.
SELECT * FROM chart_shares
WHERE user_id = $1 AND birth_profile_id = $2
ORDER BY created_at DESC;

-- name: ListChartSharesForUser :many
-- Every link this user has created, across all their profiles.
--
-- For the data export: "who can currently see my chart" is a question
-- only this answers, and an export omitting it hands somebody a copy of
-- their data with the sharing removed.
SELECT * FROM chart_shares
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: CountLiveChartShares :one
-- How many links this user currently has working, across all profiles.
-- Used to cap the total: an unbounded number of live bearer credentials
-- per account is a liability the owner cannot reason about.
SELECT COUNT(*) FROM chart_shares
WHERE user_id = $1
  AND revoked_at IS NULL
  AND expires_at > NOW();

-- ─── revoking ────────────────────────────────────────────────────────

-- name: RevokeChartShare :one
-- Scoped by user, so revoking somebody else's link affects no row and
-- the handler answers 404 — never 403, which would confirm it exists.
--
-- Idempotent: revoking an already-revoked link keeps the ORIGINAL
-- timestamp, because "when did I turn this off" must not be rewritten by
-- a second click.
UPDATE chart_shares
SET revoked_at = COALESCE(revoked_at, NOW())
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: RevokeChartSharesForProfile :exec
-- Every live link for one profile, killed at once.
--
-- Called when birth details are corrected. A correction creates a new
-- profile version, and a link pointing at the old one would keep serving
-- a chart its owner has already decided was wrong.
UPDATE chart_shares
SET revoked_at = NOW()
WHERE birth_profile_id = $1
  AND user_id = $2
  AND revoked_at IS NULL;

-- ─── resolving, for the viewer ───────────────────────────────────────

-- name: ResolveChartShare :one
-- The whole viewer authorisation, in one predicate.
--
-- Liveness is in the WHERE clause rather than checked in Go afterwards,
-- so an expired or revoked link returns NO ROW. A query that returned
-- the row and left the decision to the caller would make the guarantee
-- depend on every future call site remembering to check.
--
-- NOW() is the database's clock, not the API's. Several replicas with
-- slightly different clocks would otherwise disagree about whether a
-- link is still live, and an expiry that depends on which instance you
-- reach is not an expiry.
SELECT * FROM chart_shares
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND expires_at > NOW();

-- name: TouchChartShare :exec
-- Records that a live link was opened.
--
-- A count and a timestamp, never who. A log of viewers would be a record
-- of one person's interest in another, which this product has no
-- business keeping — and which nobody consented to when they opened a
-- link someone sent them.
UPDATE chart_shares
SET view_count = view_count + 1, last_viewed_at = NOW()
WHERE id = $1;

-- ─── housekeeping ────────────────────────────────────────────────────

-- name: DeleteExpiredChartShares :execrows
-- Swept by the worker.
--
-- Rows are kept for a grace period after death rather than deleted at
-- the instant they expire, so an owner opening the share screen can
-- still see that a link existed and has lapsed. A row that vanishes at
-- expiry reads as "I never made that link".
DELETE FROM chart_shares
WHERE expires_at < $1
   OR (revoked_at IS NOT NULL AND revoked_at < $1);
