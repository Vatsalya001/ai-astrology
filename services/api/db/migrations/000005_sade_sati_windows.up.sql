-- Phase 3 — when a Sade Sati started and when it ends.
--
-- "When does this end" is the question people actually ask about Sade
-- Sati. Until now the product could say only whether it was running and
-- which of the three phases — which is the half that causes anxiety
-- without the half that relieves it.

-- ─── sade_sati_windows ───────────────────────────────────────────────
--
-- Twelve rows. One per natal Moon SIGN, because that is all a window
-- depends on: Saturn's passage through the 12th, 1st and 2nd from the
-- Moon is a function of Saturn's motion and the sign, and nothing else
-- about the person.
--
-- ── Why stored rather than asked for ──
--
-- api-service could call astro-service per request. It deliberately does
-- not, for the same reason the transits table exists: this is a question
-- users ask constantly, and the answer must survive astro being
-- unreachable. Reading it from Postgres makes an outage invisible here.
--
-- Twelve rows refreshed every six hours is also cheap in the other
-- direction — the alternative is a per-user ephemeris scan on a read
-- path, and the scan is seconds, not milliseconds.
--
-- ── Why both dates are nullable ──
--
-- Saturn returns every ~29.5 years, so at any instant most Moon signs
-- are nowhere near their stretch and the search window contains no
-- entry. That is reported as NULL, never as a guessed date: somebody
-- plans around these.
--
-- The CHECK enforces the pair. A row with an end and no start would be a
-- window with one edge, which no UI can render honestly.
CREATE TABLE sade_sati_windows (
    -- 0 = Aries, matching SignIndex across all three services.
    moon_sign_index  SMALLINT PRIMARY KEY
        CHECK (moon_sign_index BETWEEN 0 AND 11),

    -- The sign's name, denormalised on purpose. It is what the API
    -- returns and what a person reading the table in psql needs; the
    -- alternative is a twelve-row lookup table whose only column is a
    -- name that has not changed in two thousand years.
    moon_sign        TEXT NOT NULL,

    started_at       TIMESTAMPTZ,
    ends_at          TIMESTAMPTZ,

    CONSTRAINT sade_sati_window_is_whole
        CHECK ((started_at IS NULL) = (ends_at IS NULL)),

    -- An end before its start is a boundary search that took a
    -- retrograde dip for the real exit. Refusing it here means that bug
    -- surfaces as a failed refresh rather than as a countdown running
    -- backwards on somebody's screen.
    CONSTRAINT sade_sati_window_runs_forward
        CHECK (started_at IS NULL OR ends_at > started_at),

    -- The instant the window was computed FOR, not when the row was
    -- written. A window is a function of `at`, and reproducing a row
    -- means asking astro for this instant again.
    computed_for     TIMESTAMPTZ NOT NULL,
    computed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- No index beyond the primary key, and that is deliberate rather than an
-- omission: the table has twelve rows and is read by primary key. An
-- index on anything else would cost writes and never be chosen.
