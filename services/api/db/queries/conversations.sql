-- Conversation, message and context queries. PHASE-05 task 5.10.
--
-- ── Every read is scoped by user_id in the SQL ──
--
-- Not in the handler. `shares.sql` put it this way and it is worth
-- repeating because this file is where it matters most: "a predicate in
-- the query is a guarantee the handler cannot forget to apply."
--
-- `.claude/rules/security.md`: "Cross-user access returns 404, not 403 —
-- a 403 confirms the resource exists." A query that returned the row and
-- left the ownership decision to Go would make that guarantee depend on
-- every future call site remembering to check — including the ones added
-- by tasks 5.12, 5.16 and 5.17, by someone who has not read this
-- comment. Scoped in the SQL, a non-owner's request produces NO ROW,
-- `pgx.ErrNoRows`, and a 404 that cannot accidentally become a 403.
--
-- The writes are scoped the same way, which matters more: a rename or an
-- archive that affected somebody else's row would be a silent
-- cross-account mutation, not merely an information leak.
--
-- ── Why the message queries name their columns ──
--
-- `messages.search_tsv` is a GENERATED tsvector — index payload, not
-- data. `SELECT *` would carry it over the wire on every message read,
-- into a Go field no code in this service can use (sqlc types tsvector
-- as `interface{}`). The column lists below are therefore explicit and
-- identical, in schema order:
--
--   id, conversation_id, role, content, intent, model, provider_id,
--   prompt_version, input_tokens, output_tokens, latency_ms, is_partial,
--   safety_flags, is_reported, created_at
--
-- `conversations` and `message_contexts` have no generated column, so
-- they use `*`.


-- ─── conversations: create ───────────────────────────────────────────

-- name: CreateConversation :one
-- Start a thread against one of the caller's own birth profiles.
--
-- INSERT ... SELECT rather than INSERT ... VALUES, and that is the whole
-- point of this query. `birth_profile_id` arrives from the client, so
-- VALUES would happily open a conversation bound to another user's
-- chart — §12's first checklist item ("chart context comes from the
-- chart Go loaded after an ownership check — never from an ID in the
-- message body") lost at the moment the thread is created rather than
-- when it is read.
--
-- Selecting the row makes the profile's ownership the precondition for
-- the insert: a foreign profile matches nothing, no row is inserted, and
-- the caller gets `pgx.ErrNoRows` → 404.
--
-- `user_id` is taken from the profile row, not from the parameter, so
-- the two cannot disagree. The parameter only narrows.
--
-- `is_active` is required too. Correcting a birth time supersedes the
-- old profile (000003), and a NEW thread started on a superseded
-- version would be read against a chart its owner has already said was
-- wrong. Existing threads keep pointing at it — that is the audit trail
-- — but nothing new attaches to it.
INSERT INTO conversations (user_id, birth_profile_id, title, category, persona)
SELECT p.user_id,
       p.id,
       sqlc.narg(title),
       sqlc.narg(category),
       sqlc.arg(persona)
FROM birth_profiles p
WHERE p.id = sqlc.arg(birth_profile_id)
  AND p.user_id = sqlc.arg(user_id)
  AND p.is_active
RETURNING *;


-- ─── conversations: read ─────────────────────────────────────────────

-- name: GetConversation :one
-- One thread, for its owner.
--
-- Archived threads are included: §7's history screen can open one, and
-- "archived" means "not in the default list", not "gone".
SELECT * FROM conversations
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: ListConversations :many
-- The history screen's default list: this user's live threads, most
-- recently active first.
--
-- `is_archived = FALSE` is spelled out rather than parameterised. A
-- single query with `is_archived = $2` would read as tidier and would
-- defeat `conversations_user_active_idx`, which is partial precisely
-- because this is the query that runs on every history load.
SELECT * FROM conversations
WHERE user_id = sqlc.arg(user_id) AND is_archived = FALSE
ORDER BY updated_at DESC
LIMIT sqlc.arg('limit');

-- name: ListArchivedConversations :many
-- The archive, as its own list rather than a flag on the one above.
SELECT * FROM conversations
WHERE user_id = sqlc.arg(user_id) AND is_archived = TRUE
ORDER BY updated_at DESC
LIMIT sqlc.arg('limit');

-- name: ListAllConversationsForUser :many
-- Every thread, archived or not, uncapped — for the data export.
--
-- Uncapped on purpose, like `ListChartSharesForUser`. A portability
-- export that silently drops the tail is the failure
-- `internal/users/export.go` exists to prevent: the user reads an absent
-- row as "I never had that conversation".
SELECT * FROM conversations
WHERE user_id = sqlc.arg(user_id)
ORDER BY created_at;


-- ─── conversations: update and delete ────────────────────────────────

-- name: RenameConversation :one
-- Scoped by user, so renaming somebody else's thread affects no row and
-- the handler answers 404 — never 403, which would confirm it exists.
--
-- `updated_at` is deliberately NOT touched. It is the history sort key
-- and it means "when did this conversation last have something said in
-- it"; a rename would otherwise shuffle a year-old thread to the top of
-- the list for a cosmetic edit.
UPDATE conversations
SET title = sqlc.arg(title)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)
RETURNING *;

-- name: SetConversationArchived :one
-- Archive or restore. One query for both directions, because a toggle
-- implemented as two endpoints drifts.
--
-- `updated_at` untouched, same reasoning as the rename.
UPDATE conversations
SET is_archived = sqlc.arg(is_archived)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)
RETURNING *;

-- name: DeleteConversation :execrows
-- Scoped by user. `:execrows` rather than `:exec` so the handler can
-- tell "deleted" from "not yours or not there" and return 404 for both
-- — the two cases must be indistinguishable to the client.
--
-- The messages and their contexts go with it, by `ON DELETE CASCADE` in
-- migration 000009 rather than by two more statements here. §16 gates on
-- it ("Deleting a conversation cascades to messages and contexts") and
-- the database is the only place that cannot forget.
DELETE FROM conversations
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);


-- ─── messages: append ────────────────────────────────────────────────

-- name: AppendMessage :one
-- One turn, and the conversation's counter, in a single statement.
--
-- ── Why the UPDATE is the CTE and the INSERT is the main statement ──
--
-- The UPDATE is the authorisation. It matches nothing for a non-owner,
-- so `owned` is empty, so the INSERT's SELECT produces no row and no
-- message is written — the same "no row rather than a row the caller is
-- trusted to inspect" shape as the rest of this file, applied to a
-- write. Appending to somebody else's conversation is not merely
-- refused; it is unrepresentable.
--
-- It is also what keeps `message_count` honest. One statement means the
-- counter and the row land together or not at all: a CHECK violation on
-- the message rolls the increment back with it, and the increment cannot
-- happen without a message because the INSERT is what consumes `owned`.
--
-- Concurrency: `message_count + 1` is re-evaluated after the row lock
-- the UPDATE takes, so two simultaneous appends serialise and neither
-- increment is lost. No explicit `FOR UPDATE` is needed — that is for
-- read-then-write sequences (balances, Phase 7), not for a counter
-- incremented in terms of itself.
--
-- ── Why not a trigger ──
--
-- A trigger on `messages` is the reflex and it is a trap here. An
-- AFTER DELETE trigger decrementing the counter would fire for every
-- message during a conversation's `ON DELETE CASCADE` and try to UPDATE
-- the `conversations` row that is itself being deleted. That either
-- errors or silently does nothing depending on trigger timing, and the
-- failure appears at account deletion — the one path that must not have
-- surprises. The cascade needs no decrement, because the counter is
-- deleted along with the row that holds it.
-- ── Two pieces of sqlc pedantry, both load-bearing ──
--
-- The predicate columns are table-qualified and the CTE's output column
-- is aliased. Postgres accepts the unqualified, unaliased version
-- without complaint; sqlc's analyser resolves names across the whole
-- statement, sees `id` on both `conversations` and `messages`, and fails
-- the build with `column reference "id" is ambiguous` — reported at a
-- line several statements further down, which is not a hint worth
-- rediscovering.
WITH owned AS (
    UPDATE conversations
    SET message_count = message_count + 1,
        updated_at = now()
    WHERE conversations.id = sqlc.arg(conversation_id)
      AND conversations.user_id = sqlc.arg(user_id)
    RETURNING id AS owned_conversation_id
)
INSERT INTO messages (
    conversation_id, role, content, intent, model, provider_id,
    prompt_version, input_tokens, output_tokens, latency_ms,
    is_partial, safety_flags
)
SELECT owned.owned_conversation_id,
       sqlc.arg(role),
       sqlc.arg(content),
       sqlc.narg(intent),
       sqlc.narg(model),
       sqlc.narg(provider_id),
       sqlc.narg(prompt_version),
       sqlc.narg(input_tokens),
       sqlc.narg(output_tokens),
       sqlc.narg(latency_ms),
       sqlc.arg(is_partial),
       -- COALESCE because naming the column in the INSERT list above
       -- disables its DEFAULT. Without it, a caller that has no flags to
       -- record passes a nil slice, which reaches Postgres as NULL and
       -- fails the column's NOT NULL — a failure in the ordinary path,
       -- not the exceptional one. '[]' is the same value the column
       -- defaults to: "none fired", which is a different claim from
       -- "we hold nothing".
       COALESCE(sqlc.narg(safety_flags)::jsonb, '[]'::jsonb)
FROM owned
RETURNING messages.id, messages.conversation_id, messages.role,
          messages.content, messages.intent, messages.model,
          messages.provider_id, messages.prompt_version,
          messages.input_tokens, messages.output_tokens, messages.latency_ms,
          messages.is_partial, messages.safety_flags, messages.is_reported,
          messages.created_at;


-- ─── messages: read ──────────────────────────────────────────────────

-- name: ListMessages :many
-- One conversation's turns, oldest first, for its owner.
--
-- The join is the ownership check. `conversation_id` alone would return
-- the whole of somebody else's thread to anyone who learned its id,
-- which is the single worst read in this product: it is the complete
-- text of what a person asked about their health and their marriage.
--
-- `id` is the tiebreaker on `created_at`. `now()` is the transaction's
-- start time, so two messages written in one transaction share a
-- timestamp exactly; a random UUID is not a meaningful order but it is a
-- STABLE one, which is what keeps a paginated list from repeating or
-- skipping a row. In normal operation the user's turn and the
-- assistant's are seconds apart (§6 persists the user message before the
-- model is called), so the tiebreaker is a guard, not the usual path.
SELECT m.id, m.conversation_id, m.role, m.content, m.intent, m.model,
       m.provider_id, m.prompt_version, m.input_tokens, m.output_tokens,
       m.latency_ms, m.is_partial, m.safety_flags, m.is_reported, m.created_at
FROM messages m
JOIN conversations c ON c.id = m.conversation_id
WHERE m.conversation_id = sqlc.arg(conversation_id)
  AND c.user_id = sqlc.arg(user_id)
ORDER BY m.created_at, m.id;

-- name: ListRecentMessages :many
-- The last N turns, for §5's context window (`CHAT_RECENT_MESSAGE_WINDOW`).
--
-- Newest first, because "the last 6" has to be taken from the recent end
-- — the caller reverses them before building the prompt. Ordering
-- ascending with a LIMIT would return the OLDEST six, which is the
-- version of this query that looks right and produces a model with no
-- idea what was just said.
--
-- Partial messages are included. A disconnect mid-answer is part of the
-- thread the user can see, and omitting it from the context would make
-- the model's next turn contradict the screen.
SELECT m.id, m.conversation_id, m.role, m.content, m.intent, m.model,
       m.provider_id, m.prompt_version, m.input_tokens, m.output_tokens,
       m.latency_ms, m.is_partial, m.safety_flags, m.is_reported, m.created_at
FROM messages m
JOIN conversations c ON c.id = m.conversation_id
WHERE m.conversation_id = sqlc.arg(conversation_id)
  AND c.user_id = sqlc.arg(user_id)
ORDER BY m.created_at DESC, m.id DESC
LIMIT sqlc.arg('limit');

-- name: ListMessagesForUser :many
-- Every message this person has, across every conversation — the data
-- export.
--
-- Capped, unlike the conversation list, and the asymmetry is deliberate:
-- a long-lived account has tens of thousands of messages and a response
-- that times out is not an export. The audit log is capped for the same
-- reason in `internal/users/export.go`. Ordered oldest-first so a
-- truncated export is a truncated BEGINNING of the history rather than
-- an arbitrary slice.
SELECT m.id, m.conversation_id, m.role, m.content, m.intent, m.model,
       m.provider_id, m.prompt_version, m.input_tokens, m.output_tokens,
       m.latency_ms, m.is_partial, m.safety_flags, m.is_reported, m.created_at
FROM messages m
JOIN conversations c ON c.id = m.conversation_id
WHERE c.user_id = sqlc.arg(user_id)
ORDER BY m.created_at, m.id
LIMIT sqlc.arg('limit');

-- name: SearchMessages :many
-- Full-text search across one user's own history (§7, task 5.16).
--
-- Scoped by `c.user_id` in the same WHERE clause as the match, so a
-- search can only ever rank this person's own text. A search endpoint is
-- the easiest place in a product to leak the whole corpus of user
-- content, because the predicate that limits it looks like a filter
-- rather than like authorisation.
--
-- `websearch_to_tsquery` rather than `plainto_tsquery`: it accepts what
-- people actually type into a search box — quoted phrases, `or`, a
-- leading `-` to exclude — and, unlike `to_tsquery`, it cannot raise a
-- syntax error on user input. A search that 500s on an apostrophe is a
-- search nobody uses twice.
--
-- The query text is matched against `search_tsv`, the generated column,
-- so `messages_search_idx` serves it. `ts_rank` recomputes over the
-- matched rows only, which is a user-sized set, not a corpus-sized one.
--
-- `conversation_title` comes along because a search hit is useless
-- without the thread it belongs to — the UI needs somewhere to send the
-- tap.
SELECT m.id, m.conversation_id, m.role, m.content, m.intent, m.model,
       m.provider_id, m.prompt_version, m.input_tokens, m.output_tokens,
       m.latency_ms, m.is_partial, m.safety_flags, m.is_reported, m.created_at,
       c.title AS conversation_title,
       ts_rank(m.search_tsv, websearch_to_tsquery('english', sqlc.arg(query))) AS rank
FROM messages m
JOIN conversations c ON c.id = m.conversation_id
WHERE c.user_id = sqlc.arg(user_id)
  AND m.search_tsv @@ websearch_to_tsquery('english', sqlc.arg(query))
ORDER BY rank DESC, m.created_at DESC
LIMIT sqlc.arg('limit');

-- name: MarkMessageReported :execrows
-- §7's Report button (task 5.17).
--
-- Scoped by owner through the conversation, so a user can only report an
-- answer given to them. Without the join, any message id would be
-- reportable by anyone — a way to flood the review queue with other
-- people's conversations.
--
-- Idempotent: reporting twice sets a boolean that is already true.
-- `:execrows` returns 0 for a message that is not this user's, which the
-- handler turns into 404.
UPDATE messages m
SET is_reported = TRUE
WHERE m.id = sqlc.arg(id)
  AND EXISTS (
      SELECT 1 FROM conversations c
      WHERE c.id = m.conversation_id AND c.user_id = sqlc.arg(user_id)
  );


-- ─── message_contexts ────────────────────────────────────────────────

-- name: InsertMessageContext :one
-- What was supplied to the model for one assistant message.
--
-- `prompt_version` is NOT a parameter. It is selected off the message
-- row, so the copy here and the copy on `messages` cannot disagree —
-- the duplication §5 implies becomes a derivation. A message with no
-- `prompt_version` fails the NOT NULL and the insert errors loudly,
-- which is correct: a context row for a response that does not record
-- which prompt produced it cannot explain that response.
--
-- INSERT ... SELECT also makes a context for a non-existent message
-- return no row rather than a foreign-key error, matching the shape of
-- every other write in this file.
--
-- Not scoped by user, and that is deliberate — this runs inside the
-- chat pipeline, immediately after `AppendMessage` returned the message
-- it is about, in the same request. There is no client-supplied id to
-- validate. The scoping lives on the READ, below, which is where a
-- client-supplied id does arrive.
INSERT INTO message_contexts (
    message_id, astrology_context, fact_index, knowledge_chunk_ids,
    prompt_version, context_version
)
SELECT m.id,
       sqlc.arg(astrology_context),
       sqlc.arg(fact_index),
       sqlc.arg(knowledge_chunk_ids),
       m.prompt_version,
       sqlc.arg(context_version)
FROM messages m
WHERE m.id = sqlc.arg(message_id)
RETURNING *;

-- name: GetMessageContext :one
-- The "Why am I seeing this?" payload (§7), and §16's "renders the real
-- stored context".
--
-- A read, not a regeneration. Rebuilding the context from the chart
-- would use today's context builder against today's corpus and answer a
-- different question — "what would we retrieve now?" — while looking
-- like it answered the original one. Task 5.5's dimension migration
-- re-embeds the whole corpus, and re-chunking changes which chunk ids
-- exist at all, so the regenerated answer would drift without anything
-- appearing to change.
--
-- Two joins to reach `user_id`: contexts hang off messages, which hang
-- off conversations, which is where ownership lives. The alternative —
-- trusting the handler to have loaded the conversation first — is the
-- thing this file refuses to do.
SELECT mc.* FROM message_contexts mc
JOIN messages m ON m.id = mc.message_id
JOIN conversations c ON c.id = m.conversation_id
WHERE mc.message_id = sqlc.arg(message_id)
  AND c.user_id = sqlc.arg(user_id);
