-- Phase 2 — birth profiles, charts, dashas, transits and places.
--
-- api-service owns all of this. astro-service has no database access at
-- all and never will: it receives birth data, computes, returns, and
-- forgets. That topology is what makes "the engine is deterministic"
-- checkable rather than aspirational.

-- ─── birth_profiles ──────────────────────────────────────────────────
--
-- Never overwritten. Correcting a birth time creates version 2 and marks
-- version 1 superseded, because a past reading has to stay explicable:
-- "which chart was this based on?" must always have an answer.
--
-- This is the most sensitive table in the product. Birth date + time +
-- place is, in combination, close to a unique identifier — treat it
-- exactly like an email address. It is never logged and never enters an
-- analytics payload.
CREATE TABLE birth_profiles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- self | partner | child | friend. Free text rather than an enum:
    -- the set will grow and a migration per label is not worth it.
    label           TEXT NOT NULL DEFAULT 'self',

    -- The LOCAL calendar date and clock time, as the person would say
    -- them. utc_instant below is what the maths actually uses.
    birth_date      DATE NOT NULL,
    birth_time      TIME,

    -- exact | approximate | unknown.
    --
    -- `unknown` is a first-class state, not a missing value: a large
    -- fraction of Indian users genuinely do not know their birth time,
    -- and the honest response is to omit the ascendant, the houses and
    -- the dashas rather than invent a time and compute confidently
    -- wrong answers.
    time_accuracy   TEXT NOT NULL DEFAULT 'exact'
        CHECK (time_accuracy IN ('exact', 'approximate', 'unknown')),

    birth_place     TEXT NOT NULL,
    latitude        DOUBLE PRECISION NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude       DOUBLE PRECISION NOT NULL CHECK (longitude BETWEEN -180 AND 180),

    -- IANA name, e.g. "Asia/Kolkata". Not an offset: an offset is a fact
    -- about one instant, and the whole point is that India's offset has
    -- changed (LMT before 1906, +05:30 after, +06:30 in parts of
    -- 1942-45).
    timezone        TEXT NOT NULL,

    -- The offset actually in force at this birth instant, resolved from
    -- tzdata and stored so it can be shown and audited.
    utc_offset_min  INTEGER NOT NULL,

    -- The source of truth for every calculation. Derived once, in Go,
    -- where the stdlib carries full historical tzdata.
    utc_instant     TIMESTAMPTZ NOT NULL,

    source          TEXT NOT NULL DEFAULT 'user',
    verified        BOOLEAN NOT NULL DEFAULT FALSE,

    version         INTEGER NOT NULL DEFAULT 1,
    superseded_by   UUID REFERENCES birth_profiles (id),
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- A time is required unless the user said they do not know it.
    -- Without this, an application bug that drops birth_time silently
    -- produces a chart with no ascendant and no explanation.
    CONSTRAINT birth_time_present_unless_unknown
        CHECK (time_accuracy = 'unknown' OR birth_time IS NOT NULL)
);

CREATE INDEX birth_profiles_user_idx ON birth_profiles (user_id, is_active);

-- ─── charts ──────────────────────────────────────────────────────────
--
-- Keyed by every input that affects the output. Changing the ayanamsa
-- produces a NEW row rather than mutating one, which is the same rule
-- the cache keys follow and for the same reason: a chart is a pure
-- function of its inputs, so anything that changes the output belongs in
-- the identity.
CREATE TABLE charts (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    birth_profile_id   UUID NOT NULL REFERENCES birth_profiles (id) ON DELETE CASCADE,

    chart_type         TEXT NOT NULL CHECK (chart_type IN ('D1', 'D9', 'D10')),
    calculation_system TEXT NOT NULL DEFAULT 'vedic',
    ayanamsa           TEXT NOT NULL DEFAULT 'lahiri',
    house_system       TEXT NOT NULL DEFAULT 'whole_sign',

    -- e.g. "skyfield-1.49+de421+schema1". Lets a library upgrade be
    -- detected and the affected charts recomputed, rather than leaving
    -- two subtly different generations of chart in one table with no way
    -- to tell them apart.
    engine_version     TEXT NOT NULL,

    chart_data         JSONB NOT NULL,
    computed_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (birth_profile_id, chart_type, calculation_system, ayanamsa, house_system)
);

CREATE INDEX charts_profile_idx ON charts (birth_profile_id);

-- ─── dashas ──────────────────────────────────────────────────────────
--
-- The Vimshottari tree, flattened. parent_id gives the hierarchy:
-- level 1 Maha, level 2 Antar, level 3 Pratyantar.
--
-- Stored rather than recomputed on read because "which dasha was running
-- on this date" is a range query, and Postgres does range queries better
-- than a tree walk in application code.
CREATE TABLE dashas (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chart_id   UUID NOT NULL REFERENCES charts (id) ON DELETE CASCADE,

    system     TEXT NOT NULL DEFAULT 'vimshottari',
    planet     TEXT NOT NULL,

    start_date TIMESTAMPTZ NOT NULL,
    end_date   TIMESTAMPTZ NOT NULL,

    level      SMALLINT NOT NULL CHECK (level IN (1, 2, 3)),
    parent_id  UUID REFERENCES dashas (id) ON DELETE CASCADE,

    metadata   JSONB NOT NULL DEFAULT '{}',

    -- A period that ends before it starts is a sign the proportional
    -- arithmetic has gone wrong — which is exactly the failure mode
    -- float accumulation produces, and exactly the one that looks
    -- plausible in a UI.
    CONSTRAINT dasha_ends_after_it_starts CHECK (end_date > start_date),

    -- Level 1 has no parent; levels 2 and 3 must have one. Without this
    -- an orphaned Antardasha is queryable as though it were a Mahadasha.
    CONSTRAINT dasha_parent_matches_level
        CHECK ((level = 1 AND parent_id IS NULL) OR (level > 1 AND parent_id IS NOT NULL))
);

CREATE INDEX dashas_chart_level_idx ON dashas (chart_id, level);
CREATE INDEX dashas_chart_range_idx ON dashas (chart_id, start_date, end_date);

-- ─── transits ────────────────────────────────────────────────────────
--
-- Global, shared by every user, and containing no personal data at all —
-- which is what makes them safe to cache aggressively. The per-user
-- interpretation of a transit is personal; the position of Saturn is not.
CREATE TABLE transits (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    planet             TEXT NOT NULL,
    sign               TEXT NOT NULL,
    degree             DOUBLE PRECISION NOT NULL CHECK (degree >= 0 AND degree < 30),
    is_retrograde      BOOLEAN NOT NULL DEFAULT FALSE,

    timestamp          TIMESTAMPTZ NOT NULL,
    calculation_system TEXT NOT NULL DEFAULT 'vedic',
    ayanamsa           TEXT NOT NULL DEFAULT 'lahiri',
    metadata           JSONB NOT NULL DEFAULT '{}',

    UNIQUE (planet, timestamp, calculation_system, ayanamsa)
);

CREATE INDEX transits_ts_idx ON transits (timestamp DESC);

-- ─── places ──────────────────────────────────────────────────────────
--
-- Self-hosted GeoNames. Free, offline, no rate limit, and faster than
-- any API call — place lookup happens at the highest drop-off point in
-- the product, so a network round trip there is a conversion cost.
--
-- The id is the GeoNames id rather than a generated UUID, so re-running
-- the importer updates rows instead of duplicating them.
CREATE TABLE places (
    id           INTEGER PRIMARY KEY,
    name         TEXT NOT NULL,
    ascii_name   TEXT NOT NULL,
    admin1       TEXT,
    country_code TEXT NOT NULL,
    latitude     DOUBLE PRECISION NOT NULL,
    longitude    DOUBLE PRECISION NOT NULL,

    -- Pre-resolved at import, so selecting a place needs no timezone
    -- lookup at request time.
    timezone     TEXT NOT NULL,

    population   INTEGER NOT NULL DEFAULT 0
);

-- Trigram index for prefix and fuzzy matching on partially-typed names.
CREATE INDEX places_name_trgm ON places USING gin (ascii_name gin_trgm_ops);

-- Population ranking is what makes "jaip" return Jaipur, Rajasthan
-- before a village of 600 people. A small detail with a large effect on
-- the onboarding funnel.
CREATE INDEX places_country_pop_idx ON places (country_code, population DESC);
