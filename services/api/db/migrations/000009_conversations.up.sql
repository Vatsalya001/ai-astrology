-- Phase 5 task 5.10 — the conversation model.
--
-- PHASE-05 §5 gives `conversations` and `messages` verbatim. The third
-- table, `message_contexts`, is referenced there and depended on by §7
-- but never specified; it is designed below.
--
-- `api-service` owns all of this and is the only writer (ADR-001).
-- `ai-service` never reads it: §2 is explicit that Go passes the chart
-- JSON and the recent-message window INTO the request, precisely so the
-- ownership decision lives in one place. See the grants at the bottom.
--
-- ── What the three tables are for ──
--
--   conversations     the thread, and who owns it
--   messages          the turns, including a partial one from a
--                     disconnect mid-stream (§6)
--   message_contexts  what was SUPPLIED to the model for one message
--
-- The third is the one that is easy to skip and expensive to add later.
-- §5: "a response auditable six months later: you can reconstruct
-- exactly which chart facts and knowledge chunks produced it". §7's
-- "Why am I seeing this?" panel is a READ of that row — §5 again,
-- "explainability is a read of stored data, not a second LLM call". A
-- panel that regenerated the context would be answering a different
-- question (what would we retrieve now?) while looking like it answered
-- the original one, and the corpus moves underneath it: task 5.5's
-- dimension migration re-embeds everything, and re-chunking changes
-- which chunk ids exist at all.


-- ─── conversations ───────────────────────────────────────────────────

CREATE TABLE conversations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- CASCADE. §16 gates on "Conversation and account deletion cascade
    -- fully", and the Phase 1 residue test discovers every table with a
    -- `user_id` column and refuses to pass until deleting the account
    -- empties it. A conversation is a transcript of somebody asking
    -- about their marriage and their health; nothing about it may
    -- outlive the account.
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- Deliberately NOT cascading, which is §5's own choice and worth
    -- stating because CASCADE is the reflex two lines below one.
    --
    -- Birth profiles are never deleted in normal operation: correcting
    -- a birth time creates version 2 and supersedes version 1
    -- (migration 000003), because "which chart was this reading based
    -- on?" must always have an answer. A conversation pointing at a
    -- superseded profile is therefore the normal, correct state, and a
    -- CASCADE here would mean a future cleanup of old profile versions
    -- silently erased the chat history that explains them.
    --
    -- NO ACTION does not block account deletion, and the reason is
    -- subtle enough to write down: deleting a `users` row cascades to
    -- both `birth_profiles` and `conversations` within one statement,
    -- and a non-deferrable NO ACTION check runs at the END of that
    -- statement — by which time the referencing conversations are gone
    -- too. RESTRICT would be checked immediately and WOULD break it.
    -- `conversations_schema_test.go` proves the account-deletion path
    -- rather than leaving it to this paragraph.
    birth_profile_id UUID NOT NULL REFERENCES birth_profiles (id),

    -- Nullable: a conversation exists from the moment the user opens
    -- `/chat`, before there is any text to derive a title from. §7's
    -- history screen renders a placeholder for these.
    title TEXT,

    -- The detected intent of the thread, for grouping in history.
    -- TEXT rather than an enum for the same reason 000007 gave for
    -- `category`: §3 lists 21 intents today and an enum means DDL to
    -- add the twenty-second.
    category TEXT,

    -- vedic_guide | career_guide | relationship_guide | spiritual_guide.
    -- §7 persists the persona switch to the conversation. Tone only,
    -- never facts — enforced at the prompt level in Phase 4, not here.
    persona TEXT NOT NULL DEFAULT 'vedic_guide',

    -- Denormalised, and maintained by `AppendMessage` in the same
    -- statement as the insert. See the long note on that query in
    -- db/queries/conversations.sql for why that rather than a trigger:
    -- an AFTER DELETE trigger on `messages` would fire during the
    -- cascade below and try to UPDATE a `conversations` row that is
    -- itself being deleted.
    message_count INTEGER NOT NULL DEFAULT 0,

    -- Archive rather than delete, for the same reason 000007 kept
    -- `is_active`: a deletion loses the thread, a flag keeps it out of
    -- the default list and still reachable.
    is_archived BOOLEAN NOT NULL DEFAULT FALSE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Bumped on every append, because it is the history sort key. A
    -- thread answered this morning belongs above one started last year.
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- A negative count is not a stale count, it is a bug in whatever
    -- maintains it, and it would make the consistency assertion in the
    -- schema test pass while the number was nonsense.
    CONSTRAINT conversations_count_non_negative CHECK (message_count >= 0),

    -- An empty string is a title the history screen cannot distinguish
    -- from "untitled" but which suppresses the placeholder. NULL means
    -- untitled; a blank title is a rename bug.
    CONSTRAINT conversations_title_not_blank
        CHECK (title IS NULL OR length(btrim(title)) > 0),

    -- The column has a DEFAULT, which does nothing once a caller names
    -- it in an INSERT column list — and `CreateConversation` does. A
    -- zero-value Go string would store `''`, which matches no prompt
    -- module in Phase 4 and would surface as a persona that silently
    -- stops changing the tone. NOT NULL does not catch it; this does.
    CONSTRAINT conversations_persona_not_blank
        CHECK (length(btrim(persona)) > 0)
);

-- §5's index. Every list of a user's conversations is ordered by
-- recency, and this serves both the archived and unarchived variants as
-- well as the data export, which takes them all.
CREATE INDEX conversations_user_idx ON conversations (user_id, updated_at DESC);

-- The history screen's default query, which is the one that runs on
-- every load. Partial, because `is_archived = FALSE` is the large
-- majority and a partial index is both smaller and the only shape that
-- can return `ORDER BY updated_at DESC` already sorted — a composite
-- `(user_id, is_archived, updated_at DESC)` cannot, because the sort
-- column sits behind an equality it would have to skip.
--
-- Two indexes rather than one because the archived list is a different
-- query, not the same query with a parameter. A single
-- `WHERE is_archived = $2` predicate defeats both.
CREATE INDEX conversations_user_active_idx ON conversations (user_id, updated_at DESC)
    WHERE is_archived = FALSE;

-- The foreign key. `.claude/rules/database.md`: index every foreign key.
-- Without it, deleting or superseding a birth profile sequential-scans
-- conversations to run the NO ACTION check above.
CREATE INDEX conversations_profile_idx ON conversations (birth_profile_id);


-- ─── messages ────────────────────────────────────────────────────────

CREATE TABLE messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- CASCADE, and §16 gates on it: "Deleting a conversation cascades
    -- to messages and contexts (tested)". Without it, deleting a thread
    -- leaves its turns behind as rows no query can reach and no export
    -- can return — invisible residue holding everything the person
    -- wrote.
    conversation_id UUID NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,

    role TEXT NOT NULL,

    -- NOT NULL, and deliberately WITHOUT a not-blank CHECK.
    --
    -- §6 persists on disconnect: "a user who loses signal mid-answer
    -- should find the partial message in their history, not a gap". A
    -- disconnect before the first token leaves an empty accumulator, and
    -- a CHECK would turn that into an insert failure at exactly the
    -- moment the request has already gone wrong — losing the record
    -- that anything happened, inside the error path, which is the
    -- hardest place to notice it.
    content TEXT NOT NULL,

    -- Which of §3's 21 intents routed this turn. On the message rather
    -- than only the conversation because a thread drifts: a user asks
    -- about career and then about marriage, and the explanation panel
    -- has to say which one THIS answer was built for.
    intent TEXT,

    -- §"Prompts": every response records {model, provider_id,
    -- prompt_version, context_version}. Three of the four live here;
    -- `context_version` lives with the context it names, in
    -- `message_contexts`, because it is meaningless without the facts
    -- it hashes.
    --
    -- All nullable: a user's own message was produced by no model, and
    -- a crisis short-circuit returns static human-written text with no
    -- model call at all (§"Safety").
    model TEXT,
    provider_id TEXT,
    prompt_version TEXT,

    input_tokens INTEGER,
    output_tokens INTEGER,
    latency_ms INTEGER,

    -- The disconnect flag. §16: "Client disconnect propagates
    -- cancellation to Python and persists a partial message", and task
    -- 5.12 is done when "killing the connection leaves a partial
    -- message". It exists so the UI can mark the answer as incomplete
    -- and offer a retry instead of presenting a truncated reading as a
    -- finished one.
    is_partial BOOLEAN NOT NULL DEFAULT FALSE,

    -- '[]' not NULL, matching 000006. A null reads as "we hold nothing",
    -- which is a stronger claim than "none fired".
    safety_flags JSONB NOT NULL DEFAULT '[]',

    -- §7's Report button, and §10 task 5.17.
    is_reported BOOLEAN NOT NULL DEFAULT FALSE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- The history search index, generated so it maintains itself — the
    -- same reasoning 000007 gave for `knowledge_chunks.tsv`, and the
    -- same limitation: 'english' is hardcoded, so a Hindi conversation
    -- would be stemmed as though it were English. Phase 5 ships `en`
    -- only (§1) and this is the line to change when it does not.
    --
    -- Over `content` alone. A user searching their own history is
    -- looking for what was said, not for an intent label.
    search_tsv tsvector GENERATED ALWAYS AS (to_tsvector('english', content)) STORED,

    -- §5's three roles. An enum here rather than TEXT-plus-nothing
    -- because, unlike `category`, this set is fixed by the provider
    -- APIs: a fourth role is a protocol change, not new content. A
    -- CHECK rather than a Postgres ENUM type so it can be widened
    -- without a type migration.
    CONSTRAINT messages_role_known
        CHECK (role IN ('user', 'assistant', 'system')),

    -- Only an assistant message streams, so only an assistant message
    -- can be cut short. The user's text is persisted whole before the
    -- model is called (§2), and a system message is composed locally.
    -- A partial user message would mean the request pipeline wrote the
    -- prompt it was still receiving.
    CONSTRAINT messages_only_assistant_is_partial
        CHECK (is_partial = FALSE OR role = 'assistant'),

    -- Mirrors 000006's bounds on the same three quantities. Negative
    -- token counts and negative latency are upstream bugs, and they
    -- would go on to produce a negative cost.
    CONSTRAINT messages_tokens_non_negative
        CHECK ((input_tokens IS NULL OR input_tokens >= 0)
           AND (output_tokens IS NULL OR output_tokens >= 0)),
    CONSTRAINT messages_latency_non_negative
        CHECK (latency_ms IS NULL OR latency_ms >= 0),

    CONSTRAINT messages_safety_flags_is_array
        CHECK (jsonb_typeof(safety_flags) = 'array')
);

-- §5's index, and the only ordering the chat screen uses: one
-- conversation's turns, oldest first.
CREATE INDEX messages_conv_idx ON messages (conversation_id, created_at);

-- History search. GIN over the generated column.
CREATE INDEX messages_search_idx ON messages USING gin (search_tsv);

-- The reported queue, for whoever reviews flagged answers (task 5.17).
-- Partial: reports are rare, so a full index would be almost entirely
-- `FALSE` and almost entirely wasted — the same shape as
-- `users_deletion_idx` in 000002.
CREATE INDEX messages_reported_idx ON messages (created_at DESC)
    WHERE is_reported = TRUE;


-- ─── message_contexts ────────────────────────────────────────────────
--
-- Designed here. §5 names the table and §7 depends on it, but the
-- columns below are a decision rather than a transcription.
--
-- One row per model-generated assistant message. NOT one per message:
-- a user's own turn supplies no context, and a crisis short-circuit
-- bypasses astrology entirely (§"Safety"), so there is nothing to
-- record for either. That is why the relationship is UNIQUE-and-
-- optional rather than a set of columns on `messages`.

CREATE TABLE message_contexts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- UNIQUE, so "the context for this message" is a single row and the
    -- explanation endpoint cannot be handed two different answers about
    -- the same response. CASCADE for the same reason as `messages`:
    -- this row holds the person's chart facts, and it must not survive
    -- the message, the conversation or the account.
    --
    -- The UNIQUE constraint's own index is the foreign key's index, so
    -- there is no separate one — adding it would be the redundancy
    -- 000002 called out on `users_email_idx`.
    message_id UUID NOT NULL UNIQUE REFERENCES messages (id) ON DELETE CASCADE,

    -- What §3's AstrologyContextService supplied: the intent-filtered
    -- houses, planets, dasha and transits. The structure, not a
    -- rendered string, because §7's panel lists the facts individually
    -- and each one deep-links into the Phase 3 chart with
    -- `highlight={[...]}`.
    astrology_context JSONB NOT NULL,

    -- §3's flat fact list, and §4's contract with the validator: "every
    -- astrological claim in the output appears in that index". Stored
    -- rather than recomputed because recomputing it from the chart
    -- would use TODAY's context builder, and a response is validated
    -- against the facts it was actually given.
    fact_index JSONB NOT NULL,

    -- "IDs, not content" (§5). Deliberately not a foreign key, and not
    -- only because JSONB cannot carry one: the corpus is re-chunked and
    -- re-embedded over its life (§4's dimension note, task 5.5), so a
    -- chunk id from six months ago may name a row that no longer
    -- exists. This column is a record of what WAS retrieved, not a live
    -- reference, and an FK would force the audit trail to be deleted to
    -- make a re-chunk possible.
    knowledge_chunk_ids JSONB NOT NULL,

    -- The pair that makes a response explainable, and 000006 already
    -- put the reasoning well: "the prompt version says what was asked,
    -- the context version says what was shown". Either alone explains
    -- half of an answer.
    --
    -- `prompt_version` also exists on `messages`, where §5 puts it, and
    -- a value in two places is a value that can disagree. It cannot
    -- here: `InsertMessageContext` does not accept it as a parameter —
    -- it SELECTs it off the message row. A message with no
    -- `prompt_version` then fails this NOT NULL, which is the right
    -- outcome: a context row for a response that does not record which
    -- prompt produced it explains nothing.
    prompt_version TEXT NOT NULL,

    -- §3: a hash of the supplied context. Phase 5 task 5.8 notes it
    -- carries the intent and the ayanamsa, because a chart read under
    -- the wrong ayanamsa moves planets across sign boundaries and
    -- nothing looks wrong.
    context_version TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Shape guards. These three columns are the entire input to §7's
    -- panel, and the classic failure is writing the right data in the
    -- wrong shape — an object where the renderer maps an array. That
    -- does not error anywhere: the panel renders empty, the response
    -- still streams, and "Why am I seeing this?" quietly becomes a
    -- blank box. jsonb_typeof costs nothing and turns it into an insert
    -- failure in the test that writes it.
    CONSTRAINT message_contexts_astrology_is_object
        CHECK (jsonb_typeof(astrology_context) = 'object'),
    CONSTRAINT message_contexts_fact_index_is_array
        CHECK (jsonb_typeof(fact_index) = 'array'),
    CONSTRAINT message_contexts_chunk_ids_is_array
        CHECK (jsonb_typeof(knowledge_chunk_ids) = 'array'),

    CONSTRAINT message_contexts_versions_not_blank
        CHECK (length(btrim(prompt_version)) > 0
           AND length(btrim(context_version)) > 0)
);


-- ─── closing 000006's open end ───────────────────────────────────────
--
-- `ai_request_logs.conversation_id` was declared without a foreign key
-- and said so: "conversations arrive in Phase 5. Declaring one now
-- would either block this migration on a table that does not exist or
-- require a second migration to add it later". This is that migration.
--
-- SET NULL, not CASCADE, for exactly the reason 000006 gave for
-- `user_id`: Phase 7 reconciles invoices from this table and a row that
-- vanished cannot be reconciled against anything. Deleting a
-- conversation removes the link to it and leaves the money. The row
-- carries no message content (PHASE-04 §8), so nothing personal
-- survives the deletion.
ALTER TABLE ai_request_logs
    ADD CONSTRAINT ai_request_logs_conversation_fk
    FOREIGN KEY (conversation_id) REFERENCES conversations (id) ON DELETE SET NULL;

-- Index the new foreign key. Without it, every conversation deletion
-- sequential-scans the cost log to run the SET NULL — and that table is
-- append-only and grows forever.
--
-- Partial, because the column is NULL for every row written before chat
-- existed and for every admin-playground request, which deliberately
-- writes no conversation (PHASE-05 §15).
CREATE INDEX ai_logs_conversation_idx ON ai_request_logs (conversation_id)
    WHERE conversation_id IS NOT NULL;


-- ─── grants ──────────────────────────────────────────────────────────
--
-- Which of these three tables does `ai-service` actually need to read?
-- **None of them.**
--
-- §2: "Go passes the chart JSON to Python. `ai-service` does not fetch
-- the chart itself — it receives it in the request. That keeps the
-- authorisation decision in exactly one place (Go, which checked
-- ownership) and means Python never needs a query path that could
-- return the wrong user's chart." The recent-message window arrives the
-- same way, passed in by Go. `ai-service`'s only direct read of this
-- database is hybrid retrieval over the knowledge corpus (§4), granted
-- in 000007.
--
-- The GRANTs below are written anyway, and it is worth being precise
-- about what they are and are not:
--
--   * They change nothing. 000001 sets
--     `ALTER DEFAULT PRIVILEGES ... GRANT SELECT ON TABLES TO astro_ro`,
--     so `astro_ro` receives SELECT on these tables whether or not this
--     file says so. Withholding it would need a REVOKE, not an
--     omission — and `singlewriter_test.go` asserts the reader can
--     SELECT from EVERY table in the schema, which is this repository's
--     deliberate posture: read everything, write nothing.
--
--   * They are stated explicitly because 000002 and 000007 do, and
--     000007 gave the reason: a default privilege is a property of the
--     role that created the table, so a table created by any other role
--     in staging arrives with no grant at all and the failure surfaces
--     as a permission error inside a request.
--
--   * They are NOT what stops Python reading another user's
--     conversation. §12's checklist item is "`ai-service` cannot fetch
--     arbitrary user charts (it has no such query path)" — the absence
--     of the code, plus the `user_id` predicate on every query in
--     db/queries/conversations.sql. If Phase 6's cross-conversation
--     memory ever gives Python a reason to read these tables directly,
--     the scoping has to be built there; this grant will not stop it
--     and does not pretend to.
GRANT SELECT ON conversations TO astro_ro;
GRANT SELECT ON messages TO astro_ro;
GRANT SELECT ON message_contexts TO astro_ro;
