-- Phase 1 — accounts, identities, sessions and audit.
--
-- Birth details are deliberately NOT here. They arrive in Phase 2 in
-- their own table: birth date + time + place is, in combination, close
-- to a unique identifier, and keeping it out of `users` means the row
-- read on every authenticated request carries no astrological PII.

CREATE TYPE user_role AS ENUM ('user', 'astrologer', 'admin', 'super_admin');
CREATE TYPE auth_provider AS ENUM ('phone', 'email', 'google', 'apple');

CREATE TABLE users (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email          TEXT UNIQUE,
    email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    phone          TEXT UNIQUE,                      -- E.164
    phone_verified BOOLEAN NOT NULL DEFAULT FALSE,
    name           TEXT,
    gender         TEXT,
    role           user_role NOT NULL DEFAULT 'user',
    status         TEXT NOT NULL DEFAULT 'active',   -- active | suspended | deleted
    last_login_at  TIMESTAMPTZ,
    deletion_requested_at TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- `email` and `phone` already carry UNIQUE, which creates an index. These
-- are the lookup paths for the single combined auth flow, and Postgres
-- will use the unique index for them — declared here only because the
-- spec lists them, and dropping the redundancy is a Phase 2 concern once
-- there is real query data.
CREATE INDEX users_phone_idx ON users (phone);
CREATE INDEX users_email_idx ON users (email);

-- Partial: the hard-delete worker scans only rows awaiting deletion, and
-- that is a tiny fraction of the table. A full index would be mostly
-- NULLs and mostly wasted.
CREATE INDEX users_deletion_idx ON users (deletion_requested_at)
    WHERE deletion_requested_at IS NOT NULL;

CREATE TABLE user_preferences (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                   UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    preferred_language        TEXT NOT NULL DEFAULT 'en',
    astrology_system          TEXT NOT NULL DEFAULT 'vedic',
    chart_style               TEXT NOT NULL DEFAULT 'north',   -- Phase 3 reads this
    theme                     TEXT NOT NULL DEFAULT 'dark',
    notification_preferences  JSONB NOT NULL DEFAULT '{}',
    communication_preferences JSONB NOT NULL DEFAULT '{}'
);

CREATE TABLE auth_identities (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider         auth_provider NOT NULL,
    provider_user_id TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- One account per (provider, subject). This is what makes signing in
    -- with Google twice link to the same user instead of creating a
    -- duplicate.
    UNIQUE (provider, provider_user_id)
);
CREATE INDEX auth_identities_user_idx ON auth_identities (user_id);

CREATE TABLE sessions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Rotation lineage. Every refresh issues a new row sharing the
    -- family_id of the token it replaced, so detecting one leaked token
    -- lets us revoke every descendant of it in a single UPDATE.
    family_id    UUID NOT NULL,

    -- sha256 of the opaque token. NEVER the token itself: a database
    -- dump must not be a list of live credentials.
    refresh_hash BYTEA NOT NULL,

    user_agent   TEXT,

    -- Hashed with IP_HASH_SALT. An IP address is PII and is never
    -- persisted raw.
    ip_hash      BYTEA,

    expires_at   TIMESTAMPTZ NOT NULL,

    -- Set when the token is exchanged. A token presented after this is
    -- set has leaked — see RotateRefreshToken.
    used_at      TIMESTAMPTZ,

    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_idx   ON sessions (user_id);
CREATE INDEX sessions_family_idx ON sessions (family_id);

-- UNIQUE, not just an index. This is load-bearing for rotation safety:
-- it guarantees `UPDATE ... WHERE refresh_hash = $1` touches at most one
-- row, which is what lets the check and the update be a single atomic
-- statement instead of a read-then-write race.
CREATE UNIQUE INDEX sessions_hash_idx ON sessions (refresh_hash);

CREATE TABLE audit_logs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Deliberately NOT a foreign key. An audit trail has to outlive the
    -- account it describes: ON DELETE CASCADE would erase the record of
    -- a deletion at the moment the deletion happened, and RESTRICT would
    -- make real deletion impossible. The trade is that user_id can point
    -- at a row that no longer exists, which is correct here.
    user_id    UUID,

    action     TEXT NOT NULL,

    -- IDs, enums and buckets only. NEVER PII. The column cannot enforce
    -- that; the emitter's allowlist and its tests do.
    metadata   JSONB NOT NULL DEFAULT '{}',

    ip_hash    BYTEA,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_user_idx   ON audit_logs (user_id);
CREATE INDEX audit_logs_action_idx ON audit_logs (action, created_at DESC);

-- ─── Single-writer rule ──────────────────────────────────────────────
--
-- api-service is the only writer. ai-service connects as astro_ro and
-- gets SELECT and nothing else, enforced by Postgres rather than by
-- convention.
--
-- Granted per-table here AND covered by ALTER DEFAULT PRIVILEGES in
-- migration 000001, because default privileges apply only to tables
-- created after they were set — and only for the role that creates them.
-- Being explicit means a grant cannot be missed if that assumption ever
-- breaks.
GRANT SELECT ON users, user_preferences, auth_identities, sessions, audit_logs TO astro_ro;
