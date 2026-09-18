-- Phase 3 — shareable chart links.
--
-- People send their kundli to family and to astrologers. The spec calls
-- this the most effective organic-sharing surface the product has, so it
-- has to be genuinely easy — and a link that is easy to send is a link
-- that ends up in a WhatsApp group, a screenshot and a search index.
-- The whole design below follows from that.

-- ─── chart_shares ────────────────────────────────────────────────────
--
-- One row per link the owner has created.
--
-- ── The token is stored HASHED ──
--
-- `token_hash`, not `token`. A share link is a bearer credential: whoever
-- holds it can read the chart. Storing the plaintext would mean a
-- database backup, a replica, or a stray `SELECT *` in a support tool
-- hands over working links to every shared chart in the product —
-- readable by anyone, with no sign-in and no audit trail.
--
-- SHA-256 rather than bcrypt/argon2, deliberately, and this is the one
-- place that reasoning differs from password storage. The token is 128
-- bits from crypto/rand, so there is no dictionary to attack and no
-- entropy to stretch; what matters instead is that resolving a link is a
-- single indexed lookup on every page load. A slow KDF here would buy
-- nothing and cost a login-speed hash on every view.
--
-- ── What the link does NOT carry ──
--
-- The URL is the token and nothing else. No birth date, no time, no
-- place, no profile id, no user id. The server resolves the token and
-- decides what to return, so a link cannot be edited into a different
-- chart and cannot be read without asking us. The security checklist
-- requires exactly this: "Share links resolve server-side against the
-- viewer's permissions — they do not embed birth details."
CREATE TABLE chart_shares (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- The owner. ON DELETE CASCADE because a deleted account must take
    -- its share links with it — a live link to a deleted user's chart is
    -- the worst possible outcome of an erasure request.
    user_id           UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- The chart shared. Also CASCADE: correcting birth details creates a
    -- NEW profile version, and the old one is what this link points at.
    -- When a profile is genuinely deleted the link must die with it.
    birth_profile_id  UUID NOT NULL REFERENCES birth_profiles (id) ON DELETE CASCADE,

    -- SHA-256 of the token, hex. 64 characters, fixed.
    token_hash        TEXT NOT NULL UNIQUE
        CHECK (char_length(token_hash) = 64),

    -- What the viewer is allowed to see. `chart` is the diagram and the
    -- planetary positions; it deliberately excludes the birth time and
    -- place, which are the identifying half of a birth record.
    --
    -- A column rather than a constant so the set can grow without a
    -- migration per option, and CHECKed so it cannot grow by accident.
    scope             TEXT NOT NULL DEFAULT 'chart'
        CHECK (scope IN ('chart')),

    -- Links expire. A share is an act with a moment attached — "look at
    -- this" — not a permanent publication, and a link that works forever
    -- is one the owner has no reason ever to revisit.
    expires_at        TIMESTAMPTZ NOT NULL,

    -- Revocation is a timestamp, not a DELETE. "When did I turn this
    -- off" is a question an owner asks, and a deleted row cannot answer
    -- it. It also keeps view_count meaningful after revocation.
    revoked_at        TIMESTAMPTZ,

    -- Observability for the owner, not analytics. Seeing that a link has
    -- been opened forty times is how somebody decides to revoke it.
    -- A count, never a log of WHO looked: that would be a record of the
    -- viewer's interest in a person, which we have no business keeping.
    view_count        BIGINT NOT NULL DEFAULT 0,
    last_viewed_at    TIMESTAMPTZ,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The resolve path: one lookup per view, by hash.
--
-- UNIQUE above already creates an index, so this is not repeated here.
-- What is needed is the OWNER's list — "which links have I created" —
-- which is a different access pattern and unindexed without this.
CREATE INDEX idx_chart_shares_user ON chart_shares (user_id, created_at DESC);

-- And the profile's, so deleting or superseding a profile can find the
-- links pointing at it without a sequential scan.
CREATE INDEX idx_chart_shares_profile ON chart_shares (birth_profile_id);

-- Expired rows are swept by the worker. Partial, because only live rows
-- are ever swept and indexing the revoked ones wastes the space.
CREATE INDEX idx_chart_shares_expiry ON chart_shares (expires_at)
    WHERE revoked_at IS NULL;
