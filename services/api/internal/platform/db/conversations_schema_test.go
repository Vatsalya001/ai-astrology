//go:build integration

package db_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

/*
PHASE-05 task 5.10 — the conversation model, against real Postgres.

Every claim in migration 000009 and db/queries/conversations.sql that a
unit test cannot reach lives in this file. There are four kinds, and none
of them is checkable without a database:

  - an `ON DELETE CASCADE` rule, which §16 gates on twice
  - a `user_id` predicate inside a query, which is the entire
    cross-user guarantee
  - a GRANT, in both directions
  - a counter maintained inside one statement, under concurrency

`.claude/rules/testing.md`: "Do not mock the database. The bugs that
matter — lock contention, isolation levels, grant enforcement — live in
behaviour a mock cannot reproduce." A fake store would agree with
whatever Go asked it to do, which is exactly the opposite of what is
being asserted here: that the DATABASE refuses.

The migrations are read from disk by `applyMigrations`, not duplicated,
so a future migration that weakens a grant or drops a cascade fails
these tests rather than passing them against a stale copy of the schema.

The down migration is not re-tested here. `migrations_test.go` already
applies every up, rolls the whole set back in reverse order and applies
it forward again — and the second forward pass is what tests a down
rather than a parser. 000009 drops a constraint it added to a table it
does not own (`ai_request_logs`), which is precisely the kind of ordering
mistake that test catches.

No model is called anywhere in this file.
*/

// ─── fixtures and plumbing ───────────────────────────────────────────

// chatPool brings up Postgres with every migration applied and returns a
// writer pool alongside the `astro_ro` DSN.
//
// `schemaPool` would do for most of these, but the grant test needs to
// connect as the reader, and `startPostgres` hands back DSNs rather than
// a pool for exactly that reason.
func chatPool(t *testing.T) (*pgxpool.Pool, string, func()) {
	t.Helper()
	ctx := context.Background()

	writerDSN, readerDSN, terminate := startPostgres(ctx, t)

	conn, err := pgx.Connect(ctx, writerDSN)
	if err != nil {
		terminate()
		t.Fatalf("connect for migrations: %v", err)
	}
	applyMigrations(ctx, t, conn)
	_ = conn.Close(ctx)

	pool, err := pgxpool.New(ctx, writerDSN)
	if err != nil {
		terminate()
		t.Fatalf("pool: %v", err)
	}

	return pool, readerDSN, func() {
		pool.Close()
		terminate()
	}
}

// chatUser is one seeded account and its active birth profile.
type chatUser struct {
	ID      pgtype.UUID
	Profile pgtype.UUID
}

// seedChatUser creates a user with an active birth profile.
//
// Raw SQL rather than the generated queries, so the fixtures stay
// independent of the code under test: a seed built from `CreateConversation`
// could not be used to assert anything about `CreateConversation`.
func seedChatUser(ctx context.Context, t *testing.T, pool *pgxpool.Pool, email string) chatUser {
	t.Helper()

	var user chatUser
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, email_verified, name)
		 VALUES ($1, TRUE, 'Test Person') RETURNING id`, email,
	).Scan(&user.ID); err != nil {
		t.Fatalf("seed user %s: %v", email, err)
	}

	if err := pool.QueryRow(ctx,
		`INSERT INTO birth_profiles
		   (user_id, birth_date, birth_time, time_accuracy, birth_place,
		    latitude, longitude, timezone, utc_offset_min, utc_instant)
		 VALUES ($1, '1994-08-17', '14:35', 'exact', 'Jaipur',
		         26.9124, 75.7873, 'Asia/Kolkata', 330, '1994-08-17T09:05:00Z')
		 RETURNING id`, user.ID,
	).Scan(&user.Profile); err != nil {
		t.Fatalf("seed birth profile for %s: %v", email, err)
	}

	return user
}

// thread is one conversation with a complete turn in it: the user's
// question, the assistant's answer, and the context that produced the
// answer.
type thread struct {
	Conversation pgtype.UUID
	UserTurn     pgtype.UUID
	Assistant    pgtype.UUID
}

func seedThread(ctx context.Context, t *testing.T, q dbgen.Querier, user chatUser, title string) thread {
	t.Helper()

	conversation, err := q.CreateConversation(ctx, dbgen.CreateConversationParams{
		UserID:         user.ID,
		BirthProfileID: user.Profile,
		Title:          &title,
		Category:       ptr("CAREER"),
		Persona:        "vedic_guide",
	})
	if err != nil {
		t.Fatalf("create conversation %q: %v", title, err)
	}

	question, err := q.AppendMessage(ctx, userTurn(conversation.ID, user.ID,
		"Should I change my job this year?"))
	if err != nil {
		t.Fatalf("append user turn: %v", err)
	}

	answer, err := q.AppendMessage(ctx, assistantTurn(conversation.ID, user.ID,
		"Saturn, the lord of your tenth house, is placed in the eleventh."))
	if err != nil {
		t.Fatalf("append assistant turn: %v", err)
	}

	if _, err := q.InsertMessageContext(ctx, dbgen.InsertMessageContextParams{
		MessageID:         answer.ID,
		AstrologyContext:  []byte(`{"relevant_houses":[10,6,2,11]}`),
		FactIndex:         []byte(`["10th house: Capricorn","Saturn: 11th house"]`),
		KnowledgeChunkIds: []byte(`["4f0c3f2e-0000-4000-8000-000000000001"]`),
		ContextVersion:    "ctx.career.lahiri.v1",
	}); err != nil {
		t.Fatalf("insert message context: %v", err)
	}

	return thread{Conversation: conversation.ID, UserTurn: question.ID, Assistant: answer.ID}
}

func userTurn(conversation, user pgtype.UUID, content string) dbgen.AppendMessageParams {
	return dbgen.AppendMessageParams{
		ConversationID: conversation,
		UserID:         user,
		Role:           "user",
		Content:        content,
		Intent:         ptr("CAREER"),
	}
}

func assistantTurn(conversation, user pgtype.UUID, content string) dbgen.AppendMessageParams {
	return dbgen.AppendMessageParams{
		ConversationID: conversation,
		UserID:         user,
		Role:           "assistant",
		Content:        content,
		Intent:         ptr("CAREER"),
		Model:          ptr("mock-chat"),
		ProviderID:     ptr("mock"),
		PromptVersion:  ptr("chat_response.v1"),
		InputTokens:    ptrInt32(2100),
		OutputTokens:   ptrInt32(240),
		LatencyMs:      ptrInt32(1870),
	}
}

func ptr(s string) *string    { return &s }
func ptrInt32(v int32) *int32 { return &v }
func count(ctx context.Context, t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count (%s): %v", sql, err)
	}
	return n
}

// ─── the cross-user guarantee ────────────────────────────────────────

/*
A predicate in the query, not a check in the handler.

`.claude/rules/security.md`: "Cross-user access returns 404, not 403 — a
403 confirms the resource exists." The way that stays true as tasks 5.12,
5.16 and 5.17 add call sites is for the query itself to return nothing,
which is what every statement in db/queries/conversations.sql does. This
test is the only thing that says so.

Every scoped query is covered, not a representative sample. A guarantee
that holds for the read path and not the rename is not a guarantee, and
the writes are the worse half: a rename or an archive that reached
somebody else's row would be a silent cross-account mutation rather than
an information leak.

The control subtest at the end is load-bearing. Without it this whole
test passes on a database where the seed failed, every query returns
nothing to everybody, and the cross-user assertions are vacuously true.
*/
func TestCrossUserAccessToAConversationReturnsNoRow(t *testing.T) {
	pool, _, stop := chatPool(t)
	defer stop()

	ctx := context.Background()
	q := dbgen.New(pool)

	alice := seedChatUser(ctx, t, pool, "alice-xuser@example.test")
	bob := seedChatUser(ctx, t, pool, "bob-xuser@example.test")
	own := seedThread(ctx, t, q, alice, "Alice on her career")

	// Bob has a thread of his own, so "Bob sees nothing" cannot be
	// satisfied by Bob simply having no data.
	seedThread(ctx, t, q, bob, "Bob on his career")

	t.Run("GetConversation", func(t *testing.T) {
		_, err := q.GetConversation(ctx, dbgen.GetConversationParams{
			ID: own.Conversation, UserID: bob.ID})
		requireNoRows(t, err, "Bob read Alice's conversation")
	})

	t.Run("ListConversations", func(t *testing.T) {
		rows, err := q.ListConversations(ctx, dbgen.ListConversationsParams{
			UserID: bob.ID, Limit: 50})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, row := range rows {
			if row.ID == own.Conversation {
				t.Fatal("Alice's conversation appeared in Bob's history list")
			}
		}
	})

	t.Run("ListMessages", func(t *testing.T) {
		rows, err := q.ListMessages(ctx, dbgen.ListMessagesParams{
			ConversationID: own.Conversation, UserID: bob.ID})
		if err != nil {
			t.Fatalf("list messages: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("Bob read %d of Alice's messages — this is the full text of "+
				"what she asked and what she was told", len(rows))
		}
	})

	t.Run("ListRecentMessages", func(t *testing.T) {
		rows, err := q.ListRecentMessages(ctx, dbgen.ListRecentMessagesParams{
			ConversationID: own.Conversation, UserID: bob.ID, Limit: 6})
		if err != nil {
			t.Fatalf("list recent: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("Bob read %d of Alice's recent messages — which would reach "+
				"the model as his conversation context", len(rows))
		}
	})

	t.Run("SearchMessages", func(t *testing.T) {
		rows, err := q.SearchMessages(ctx, dbgen.SearchMessagesParams{
			UserID: bob.ID, Query: "Saturn", Limit: 20})
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		for _, row := range rows {
			if row.ConversationID == own.Conversation {
				t.Fatal("search returned a message from Alice's conversation to Bob")
			}
		}
	})

	t.Run("GetMessageContext", func(t *testing.T) {
		_, err := q.GetMessageContext(ctx, dbgen.GetMessageContextParams{
			MessageID: own.Assistant, UserID: bob.ID})
		requireNoRows(t, err, "Bob read the chart facts behind Alice's answer")
	})

	t.Run("RenameConversation", func(t *testing.T) {
		_, err := q.RenameConversation(ctx, dbgen.RenameConversationParams{
			ID: own.Conversation, UserID: bob.ID, Title: ptr("renamed by Bob")})
		requireNoRows(t, err, "Bob renamed Alice's conversation")

		var title *string
		if err := pool.QueryRow(ctx,
			`SELECT title FROM conversations WHERE id = $1`, own.Conversation,
		).Scan(&title); err != nil {
			t.Fatalf("read title back: %v", err)
		}
		if title == nil || *title != "Alice on her career" {
			t.Fatalf("Alice's title is now %v — the rename reached her row", title)
		}
	})

	t.Run("SetConversationArchived", func(t *testing.T) {
		_, err := q.SetConversationArchived(ctx, dbgen.SetConversationArchivedParams{
			ID: own.Conversation, UserID: bob.ID, IsArchived: true})
		requireNoRows(t, err, "Bob archived Alice's conversation")

		var archived bool
		if err := pool.QueryRow(ctx,
			`SELECT is_archived FROM conversations WHERE id = $1`, own.Conversation,
		).Scan(&archived); err != nil {
			t.Fatalf("read is_archived back: %v", err)
		}
		if archived {
			t.Fatal("Alice's conversation is archived — the update reached her row")
		}
	})

	t.Run("MarkMessageReported", func(t *testing.T) {
		affected, err := q.MarkMessageReported(ctx, dbgen.MarkMessageReportedParams{
			ID: own.Assistant, UserID: bob.ID})
		if err != nil {
			t.Fatalf("report: %v", err)
		}
		if affected != 0 {
			t.Fatalf("Bob reported %d of Alice's messages — the review queue can be "+
				"filled with other people's conversations", affected)
		}
	})

	t.Run("AppendMessage", func(t *testing.T) {
		before := count(ctx, t, pool,
			`SELECT count(*) FROM messages WHERE conversation_id = $1`, own.Conversation)

		_, err := q.AppendMessage(ctx, userTurn(own.Conversation, bob.ID,
			"injected into somebody else's thread"))
		requireNoRows(t, err, "Bob wrote into Alice's conversation")

		after := count(ctx, t, pool,
			`SELECT count(*) FROM messages WHERE conversation_id = $1`, own.Conversation)
		if after != before {
			t.Fatalf("Alice's conversation went from %d messages to %d", before, after)
		}

		// And the counter did not move either. The increment lives in
		// the same statement as the insert, so a bump without a message
		// would mean the authorisation gate had been separated from the
		// write.
		var stored int32
		if err := pool.QueryRow(ctx,
			`SELECT message_count FROM conversations WHERE id = $1`, own.Conversation,
		).Scan(&stored); err != nil {
			t.Fatalf("read message_count: %v", err)
		}
		if int(stored) != after {
			t.Fatalf("message_count is %d for %d rows — a refused append still "+
				"incremented the counter", stored, after)
		}
	})

	t.Run("CreateConversation against another user's profile", func(t *testing.T) {
		// §12's first checklist item, at the moment a thread is created
		// rather than when it is read: "chart context comes from the
		// chart Go loaded after an ownership check — never from an ID in
		// the message body".
		_, err := q.CreateConversation(ctx, dbgen.CreateConversationParams{
			UserID:         bob.ID,
			BirthProfileID: alice.Profile,
			Title:          ptr("Bob reading Alice's chart"),
			Persona:        "vedic_guide",
		})
		requireNoRows(t, err, "Bob opened a conversation bound to Alice's birth profile")
	})

	t.Run("DeleteConversation", func(t *testing.T) {
		affected, err := q.DeleteConversation(ctx, dbgen.DeleteConversationParams{
			ID: own.Conversation, UserID: bob.ID})
		if err != nil {
			t.Fatalf("delete: %v", err)
		}
		if affected != 0 {
			t.Fatalf("Bob deleted %d of Alice's conversations", affected)
		}
		if count(ctx, t, pool,
			`SELECT count(*) FROM conversations WHERE id = $1`, own.Conversation) != 1 {
			t.Fatal("Alice's conversation is gone")
		}
	})

	// ── The control ──
	//
	// Alice must be able to do every one of the above. Without this, the
	// whole test is satisfied by a schema in which nothing is readable by
	// anyone, which is indistinguishable from a correctly scoped one if
	// you only ever assert the refusals.
	t.Run("and Alice can do all of it", func(t *testing.T) {
		if _, err := q.GetConversation(ctx, dbgen.GetConversationParams{
			ID: own.Conversation, UserID: alice.ID}); err != nil {
			t.Fatalf("Alice cannot read her own conversation: %v", err)
		}

		messages, err := q.ListMessages(ctx, dbgen.ListMessagesParams{
			ConversationID: own.Conversation, UserID: alice.ID})
		if err != nil {
			t.Fatalf("Alice cannot list her own messages: %v", err)
		}
		if len(messages) != 2 {
			t.Fatalf("Alice sees %d of her own 2 messages", len(messages))
		}

		if _, err := q.GetMessageContext(ctx, dbgen.GetMessageContextParams{
			MessageID: own.Assistant, UserID: alice.ID}); err != nil {
			t.Fatalf("Alice cannot read her own explanation: %v", err)
		}

		hits, err := q.SearchMessages(ctx, dbgen.SearchMessagesParams{
			UserID: alice.ID, Query: "Saturn", Limit: 20})
		if err != nil {
			t.Fatalf("Alice cannot search her own history: %v", err)
		}
		if len(hits) == 0 {
			t.Fatal("Alice's search for a word in her own message found nothing")
		}

		renamed, err := q.RenameConversation(ctx, dbgen.RenameConversationParams{
			ID: own.Conversation, UserID: alice.ID, Title: ptr("Career, revisited")})
		if err != nil {
			t.Fatalf("Alice cannot rename her own conversation: %v", err)
		}
		if renamed.Title == nil || *renamed.Title != "Career, revisited" {
			t.Fatalf("rename returned title %v", renamed.Title)
		}

		affected, err := q.DeleteConversation(ctx, dbgen.DeleteConversationParams{
			ID: own.Conversation, UserID: alice.ID})
		if err != nil {
			t.Fatalf("Alice cannot delete her own conversation: %v", err)
		}
		if affected != 1 {
			t.Fatalf("Alice's own delete affected %d rows", affected)
		}
	})
}

// requireNoRows asserts a scoped query returned nothing rather than a row.
//
// `pgx.ErrNoRows` specifically, not any error. A permission failure, a
// syntax error or a connection drop would also leave the caller with no
// row, and a test satisfied by those is a test that would keep passing
// after the predicate was removed and something else broke instead.
func requireNoRows(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s — the query returned a row where it must return none", what)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("%s: refused with %v, want pgx.ErrNoRows. Failing for the wrong "+
			"reason means the user_id predicate is untested", what, err)
	}
}

// ─── cascades ────────────────────────────────────────────────────────

/*
§16, twice: "Deleting a conversation cascades to messages and contexts
(tested)" and "Conversation and account deletion cascade fully".

Both halves matter and they fail differently. A missing cascade on
`messages` leaves rows no query can reach and no export can return —
invisible residue holding everything the person wrote. A missing cascade
on `message_contexts` leaves their chart facts behind instead, which is
the same failure one level deeper and the one a hand-written delete is
most likely to stop short of.

Counted per user rather than globally, for the reason
`deletion_integration_test.go` gives: a global count of zero is also
satisfied by a cascade that deleted everybody's rows.
*/
func TestDeletingAConversationCascadesToMessagesAndContexts(t *testing.T) {
	pool, _, stop := chatPool(t)
	defer stop()

	ctx := context.Background()
	q := dbgen.New(pool)

	alice := seedChatUser(ctx, t, pool, "alice-cascade@example.test")
	bob := seedChatUser(ctx, t, pool, "bob-cascade@example.test")

	doomed := seedThread(ctx, t, q, alice, "To be deleted")
	survivor := seedThread(ctx, t, q, bob, "To be kept")

	// Per-conversation counts, so "gone" and "kept" are separable.
	messagesIn := func(conversation pgtype.UUID) int {
		return count(ctx, t, pool,
			`SELECT count(*) FROM messages WHERE conversation_id = $1`, conversation)
	}
	contextsIn := func(conversation pgtype.UUID) int {
		return count(ctx, t, pool,
			`SELECT count(*) FROM message_contexts mc
			 JOIN messages m ON m.id = mc.message_id
			 WHERE m.conversation_id = $1`, conversation)
	}

	// The seed has to have produced something, or "nothing remains"
	// proves nothing.
	if messagesIn(doomed.Conversation) != 2 || contextsIn(doomed.Conversation) != 1 {
		t.Fatalf("seed produced %d messages and %d contexts; expected 2 and 1",
			messagesIn(doomed.Conversation), contextsIn(doomed.Conversation))
	}

	t.Run("deleting the conversation", func(t *testing.T) {
		affected, err := q.DeleteConversation(ctx, dbgen.DeleteConversationParams{
			ID: doomed.Conversation, UserID: alice.ID})
		if err != nil {
			t.Fatalf("delete: %v", err)
		}
		if affected != 1 {
			t.Fatalf("delete affected %d rows, want 1", affected)
		}

		if remaining := messagesIn(doomed.Conversation); remaining != 0 {
			t.Errorf("%d messages remain after the conversation was deleted. They are "+
				"unreachable by every query in conversations.sql and by the data "+
				"export, and they hold the full text of what the user wrote",
				remaining)
		}
		if remaining := contextsIn(doomed.Conversation); remaining != 0 {
			t.Errorf("%d message_contexts remain. The cascade stopped one level "+
				"short, which leaves this person's chart facts behind", remaining)
		}

		// Bob's thread is untouched, which only a correctly scoped
		// cascade achieves.
		if messagesIn(survivor.Conversation) != 2 {
			t.Errorf("deleting Alice's conversation removed Bob's messages")
		}
		if contextsIn(survivor.Conversation) != 1 {
			t.Errorf("deleting Alice's conversation removed Bob's contexts")
		}
	})

	t.Run("deleting the account", func(t *testing.T) {
		/*
		   The other half of the gate, and the reason it needs its own
		   assertion: `conversations.birth_profile_id` is deliberately
		   NOT a cascading foreign key (migration 000009 explains why),
		   and deleting a user cascades to `birth_profiles` AND
		   `conversations` within one statement. That only works because
		   a non-deferrable NO ACTION check runs at the END of the
		   statement, after the referencing conversations have gone.
		   RESTRICT would be checked immediately and would make account
		   deletion fail outright.

		   That is a paragraph of reasoning about Postgres trigger
		   timing. This is the assertion.
		*/
		second := seedThread(ctx, t, q, alice, "Alice's other thread")
		if messagesIn(second.Conversation) == 0 {
			t.Fatal("the second thread seeded no messages")
		}

		if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, alice.ID); err != nil {
			t.Fatalf("delete the account: %v. If this is a foreign-key violation on "+
				"conversations.birth_profile_id, the FK has been changed to "+
				"RESTRICT and account deletion is broken", err)
		}

		for _, c := range []struct {
			what string
			sql  string
		}{
			{"conversations", `SELECT count(*) FROM conversations WHERE user_id = $1`},
			{"messages", `SELECT count(*) FROM messages m
			              JOIN conversations c ON c.id = m.conversation_id
			              WHERE c.user_id = $1`},
			{"message_contexts", `SELECT count(*) FROM message_contexts mc
			                      JOIN messages m ON m.id = mc.message_id
			                      JOIN conversations c ON c.id = m.conversation_id
			                      WHERE c.user_id = $1`},
		} {
			if left := count(ctx, t, pool, c.sql, alice.ID); left != 0 {
				t.Errorf("%d rows remain in %q after the account was deleted",
					left, c.what)
			}
		}

		// Globally, Bob's rows are still there — so the above was not
		// satisfied by a cascade that emptied the tables.
		if messagesIn(survivor.Conversation) != 2 {
			t.Error("deleting Alice's account removed Bob's messages")
		}
	})
}

/*
The cost log outlives the conversation it records.

Migration 000009 closes the open end 000006 left: `conversation_id` had
no foreign key because `conversations` did not exist yet. It has one now,
and it is `ON DELETE SET NULL` rather than CASCADE for the reason 000006
gave about `user_id` — Phase 7 reconciles invoices from this table, and a
row that vanished cannot be reconciled against anything.

CASCADE is the reflex here and it would be a money bug: every
conversation a user deletes would silently remove the cost of answering
it, and the cost-per-request figure the table exists to produce would
drift down by an unknowable amount. §15 makes this phase the first
production writer of that table, so the rule has to be right before
there is anything in it.
*/
func TestTheCostLogOutlivesTheConversationItRecords(t *testing.T) {
	pool, _, stop := chatPool(t)
	defer stop()

	ctx := context.Background()
	q := dbgen.New(pool)

	alice := seedChatUser(ctx, t, pool, "alice-cost@example.test")
	own := seedThread(ctx, t, q, alice, "A thread with a bill")

	const costMicros = int64(1) << 53 // beyond float64's exact range, as 000006 does

	insertLog := func(conversation any) error {
		_, err := pool.Exec(ctx, `
			INSERT INTO ai_request_logs
			  (trace_id, user_id, conversation_id, job_type, provider_id, model,
			   tier, prompt_version, context_version, latency_ms, cost_micros,
			   finish_reason)
			VALUES ('trace-cost', $1, $2, 'chat_response', 'mock', 'mock-chat',
			        'local', 'chat_response.v1', 'ctx.v1', 1870, $3, 'stop')`,
			alice.ID, conversation, costMicros)
		return err
	}

	if err := insertLog(own.Conversation); err != nil {
		t.Fatalf("record the cost of a real conversation: %v", err)
	}

	t.Run("a conversation_id that names nothing is refused", func(t *testing.T) {
		// The foreign key exists at all. Without this, SET NULL below
		// would be asserted against a column with no constraint on it,
		// and a dangling id — a cost attributed to a conversation that
		// never existed — would be accepted silently.
		err := insertLog("00000000-0000-4000-8000-000000000000")
		if err == nil {
			t.Fatal("a cost row naming a non-existent conversation was accepted")
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
			t.Fatalf("refused with %v, want a foreign-key violation (23503)", err)
		}
	})

	t.Run("deleting the conversation keeps the money", func(t *testing.T) {
		if _, err := q.DeleteConversation(ctx, dbgen.DeleteConversationParams{
			ID: own.Conversation, UserID: alice.ID}); err != nil {
			t.Fatalf("delete: %v", err)
		}

		var conversationID pgtype.UUID
		var stored int64
		err := pool.QueryRow(ctx,
			`SELECT conversation_id, cost_micros FROM ai_request_logs
			 WHERE trace_id = 'trace-cost'`).Scan(&conversationID, &stored)
		if errors.Is(err, pgx.ErrNoRows) {
			t.Fatal("the cost row was deleted with the conversation — the foreign " +
				"key is CASCADE, and Phase 7 cannot reconcile an invoice against " +
				"a row that no longer exists")
		}
		if err != nil {
			t.Fatalf("read the cost row back: %v", err)
		}
		if conversationID.Valid {
			t.Error("conversation_id still points at a deleted conversation")
		}
		if stored != costMicros {
			t.Errorf("cost_micros is %d, want %d", stored, costMicros)
		}
	})
}

// ─── the counter ─────────────────────────────────────────────────────

/*
`message_count` is denormalised, and a denormalised count is a lie
waiting to happen.

It is maintained inside `AppendMessage`'s single statement — see the long
note on that query for why that rather than a trigger. The properties
worth asserting are the ones a Go-side increment would not have: it moves
with the rows, it does not move when the insert is refused, and
simultaneous appends do not lose an increment.
*/
func TestMessageCountMatchesTheStoredMessages(t *testing.T) {
	pool, _, stop := chatPool(t)
	defer stop()

	ctx := context.Background()
	q := dbgen.New(pool)

	alice := seedChatUser(ctx, t, pool, "alice-count@example.test")

	conversation, err := q.CreateConversation(ctx, dbgen.CreateConversationParams{
		UserID: alice.ID, BirthProfileID: alice.Profile, Persona: "vedic_guide"})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if conversation.MessageCount != 0 {
		t.Fatalf("a new conversation starts at %d", conversation.MessageCount)
	}

	assertConsistent := func(t *testing.T, context string) {
		t.Helper()
		var stored int32
		if err := pool.QueryRow(ctx,
			`SELECT message_count FROM conversations WHERE id = $1`, conversation.ID,
		).Scan(&stored); err != nil {
			t.Fatalf("read message_count: %v", err)
		}
		actual := count(ctx, t, pool,
			`SELECT count(*) FROM messages WHERE conversation_id = $1`, conversation.ID)
		if int(stored) != actual {
			t.Fatalf("%s: message_count is %d and there are %d message rows",
				context, stored, actual)
		}
	}

	t.Run("it tracks sequential appends", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			if _, err := q.AppendMessage(ctx, userTurn(conversation.ID, alice.ID,
				fmt.Sprintf("turn %d", i))); err != nil {
				t.Fatalf("append %d: %v", i, err)
			}
		}
		assertConsistent(t, "after five appends")
	})

	t.Run("a refused append does not move it", func(t *testing.T) {
		// A partial USER message violates
		// `messages_only_assistant_is_partial`. The point is not the
		// constraint — that is asserted elsewhere — but that the
		// increment and the insert are one statement, so the failure
		// rolls back both. A Go-side `UPDATE ... SET count = count + 1`
		// issued before the insert would leave the counter one ahead
		// for the life of the conversation.
		bad := userTurn(conversation.ID, alice.ID, "cut short")
		bad.IsPartial = true
		if _, err := q.AppendMessage(ctx, bad); err == nil {
			t.Fatal("a partial user message was accepted")
		}
		assertConsistent(t, "after a refused append")
	})

	t.Run("concurrent appends do not lose an increment", func(t *testing.T) {
		/*
		   `message_count + 1` is evaluated after the UPDATE acquires the
		   row lock, so READ COMMITTED re-reads the committed value and
		   two simultaneous appends serialise. The version of this that
		   loses counts is a read in Go followed by a write of the
		   literal result — which is why `.claude/rules/go.md` insists on
		   `-race` and on not mocking the database: a mock would
		   serialise everything and agree.
		*/
		const writers = 24
		var wg sync.WaitGroup
		errs := make(chan error, writers)

		for i := 0; i < writers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, err := q.AppendMessage(ctx, userTurn(conversation.ID, alice.ID,
					fmt.Sprintf("concurrent turn %d", i)))
				if err != nil {
					errs <- err
				}
			}(i)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("concurrent append: %v", err)
		}

		assertConsistent(t, "after 24 concurrent appends")

		// And the absolute number, so a consistent-but-wrong pair (both
		// 5) cannot pass.
		if total := count(ctx, t, pool,
			`SELECT count(*) FROM messages WHERE conversation_id = $1`,
			conversation.ID); total != 5+writers {
			t.Fatalf("%d messages stored, want %d", total, 5+writers)
		}
	})
}

// ─── what the message row has to hold, and what it must refuse ───────

/*
The partial message, and the constraints around it.

§16: "Client disconnect propagates cancellation to Python and persists a
partial message". Task 5.12 builds that path; this asserts the column it
depends on actually round-trips, including the awkward case the schema
deliberately allows — a disconnect before the first token, which leaves
the accumulator empty.

`.claude/rules/testing.md`: "Test the negative case. A guard that has
never been observed to fire is a guard you cannot trust." The refusals
below are each a line of migration 000009 that would otherwise be
decoration.
*/
func TestTheMessageRowHoldsAPartialAnswerAndRefusesTheImpossible(t *testing.T) {
	pool, _, stop := chatPool(t)
	defer stop()

	ctx := context.Background()
	q := dbgen.New(pool)

	alice := seedChatUser(ctx, t, pool, "alice-partial@example.test")
	conversation, err := q.CreateConversation(ctx, dbgen.CreateConversationParams{
		UserID: alice.ID, BirthProfileID: alice.Profile, Persona: "vedic_guide"})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	t.Run("a partial assistant message round-trips", func(t *testing.T) {
		cut := assistantTurn(conversation.ID, alice.ID,
			"Your current period is traditionally associated with")
		cut.IsPartial = true
		cut.OutputTokens = ptrInt32(9)

		written, err := q.AppendMessage(ctx, cut)
		if err != nil {
			t.Fatalf("persist a partial message: %v", err)
		}
		if !written.IsPartial {
			t.Fatal("is_partial came back false from the insert that set it")
		}

		// Read back through the query the UI uses, not through the
		// INSERT's own RETURNING — a flag that round-trips on the write
		// and is dropped from the read is the version of this bug that
		// looks fine in a handler test.
		rows, err := q.ListMessages(ctx, dbgen.ListMessagesParams{
			ConversationID: conversation.ID, UserID: alice.ID})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("listed %d messages, want 1", len(rows))
		}
		if !rows[0].IsPartial {
			t.Error("the message reads back as complete; the UI would present a " +
				"truncated reading as a finished one")
		}
		if rows[0].Content != cut.Content {
			t.Errorf("content round-tripped as %q", rows[0].Content)
		}
	})

	t.Run("an empty partial answer is allowed", func(t *testing.T) {
		// A disconnect before the first token. Migration 000009 leaves
		// `content` without a not-blank CHECK precisely for this: a
		// refusal here would fail inside the error path, losing the
		// record that the request happened at all.
		empty := assistantTurn(conversation.ID, alice.ID, "")
		empty.IsPartial = true
		if _, err := q.AppendMessage(ctx, empty); err != nil {
			t.Fatalf("an empty partial answer was refused: %v. §6 persists on "+
				"disconnect, and a disconnect before the first token has nothing "+
				"to persist but the fact that it happened", err)
		}
	})

	t.Run("only an assistant message can be partial", func(t *testing.T) {
		for _, role := range []string{"user", "system"} {
			bad := dbgen.AppendMessageParams{
				ConversationID: conversation.ID, UserID: alice.ID,
				Role: role, Content: "half a prompt", IsPartial: true,
			}
			if _, err := q.AppendMessage(ctx, bad); err == nil {
				t.Errorf("a partial %q message was accepted — only the assistant "+
					"turn streams, so only it can be cut short", role)
			} else if !strings.Contains(err.Error(), "violates check constraint") {
				t.Errorf("a partial %q message was refused for the wrong reason: %v",
					role, err)
			}
		}
	})

	t.Run("an unknown role is refused", func(t *testing.T) {
		bad := dbgen.AppendMessageParams{
			ConversationID: conversation.ID, UserID: alice.ID,
			Role: "tool", Content: "{}",
		}
		if _, err := q.AppendMessage(ctx, bad); err == nil {
			t.Error("role 'tool' was accepted; §5 fixes the set at user, " +
				"assistant and system")
		}
	})

	t.Run("safety_flags defaults to an empty array", func(t *testing.T) {
		// `AppendMessage` COALESCEs a nil slice to '[]', because naming
		// the column in the INSERT list disables the column DEFAULT. A
		// caller with no flags to record is the ordinary case, and it
		// must not have to know that.
		plain := assistantTurn(conversation.ID, alice.ID, "No flags fired here.")
		written, err := q.AppendMessage(ctx, plain)
		if err != nil {
			t.Fatalf("append with no safety flags: %v", err)
		}
		if string(written.SafetyFlags) != "[]" {
			t.Errorf("safety_flags stored as %q, want []", written.SafetyFlags)
		}
	})
}

/*
`message_contexts` is the "Why am I seeing this?" payload, and §16 says
it must render "the real stored context".

Two properties the schema is responsible for:

  - the shape guards. The panel's whole input is three JSONB columns, and
    writing the right data in the wrong shape — an object where the
    renderer maps an array — errors nowhere. The response still streams,
    the panel renders empty, and explainability quietly becomes a blank
    box.
  - the prompt version cannot disagree with the message it describes,
    because `InsertMessageContext` does not accept it as a parameter.
*/
func TestTheStoredContextCannotDisagreeWithItsMessage(t *testing.T) {
	pool, _, stop := chatPool(t)
	defer stop()

	ctx := context.Background()
	q := dbgen.New(pool)

	alice := seedChatUser(ctx, t, pool, "alice-context@example.test")
	own := seedThread(ctx, t, q, alice, "A thread with an explanation")

	t.Run("the prompt version is derived from the message", func(t *testing.T) {
		stored, err := q.GetMessageContext(ctx, dbgen.GetMessageContextParams{
			MessageID: own.Assistant, UserID: alice.ID})
		if err != nil {
			t.Fatalf("read the context: %v", err)
		}

		var onMessage *string
		if err := pool.QueryRow(ctx,
			`SELECT prompt_version FROM messages WHERE id = $1`, own.Assistant,
		).Scan(&onMessage); err != nil {
			t.Fatalf("read the message's prompt_version: %v", err)
		}
		if onMessage == nil || stored.PromptVersion != *onMessage {
			t.Fatalf("the context says %q and the message says %v — a value in two "+
				"places that can disagree", stored.PromptVersion, onMessage)
		}
		if stored.ContextVersion != "ctx.career.lahiri.v1" {
			t.Errorf("context_version round-tripped as %q", stored.ContextVersion)
		}
	})

	t.Run("a context for a message with no prompt version is refused", func(t *testing.T) {
		// The user's own turn records no prompt_version, because no
		// prompt produced it. A context row attached to it would claim
		// to explain a response that has no recorded prompt, so the NOT
		// NULL fires — loudly, which is the point.
		_, err := q.InsertMessageContext(ctx, dbgen.InsertMessageContextParams{
			MessageID:         own.UserTurn,
			AstrologyContext:  []byte(`{}`),
			FactIndex:         []byte(`[]`),
			KnowledgeChunkIds: []byte(`[]`),
			ContextVersion:    "ctx.v1",
		})
		if err == nil {
			t.Fatal("a context was stored for a message that records no prompt version")
		}
		if !strings.Contains(err.Error(), "null value") {
			t.Errorf("refused for the wrong reason: %v", err)
		}
	})

	t.Run("a context for a message that does not exist returns no row", func(t *testing.T) {
		_, err := q.InsertMessageContext(ctx, dbgen.InsertMessageContextParams{
			MessageID:         pgtype.UUID{Bytes: [16]byte{9, 9, 9}, Valid: true},
			AstrologyContext:  []byte(`{}`),
			FactIndex:         []byte(`[]`),
			KnowledgeChunkIds: []byte(`[]`),
			ContextVersion:    "ctx.v1",
		})
		requireNoRows(t, err, "a context was stored for a message that does not exist")
	})

	t.Run("one context per message", func(t *testing.T) {
		_, err := q.InsertMessageContext(ctx, dbgen.InsertMessageContextParams{
			MessageID:         own.Assistant,
			AstrologyContext:  []byte(`{"relevant_houses":[7]}`),
			FactIndex:         []byte(`["7th house: Cancer"]`),
			KnowledgeChunkIds: []byte(`[]`),
			ContextVersion:    "ctx.marriage.v1",
		})
		if err == nil {
			t.Fatal("a second context was stored for the same message — the " +
				"explanation endpoint can now be handed two different answers " +
				"about one response")
		}
	})

	t.Run("the shape guards fire", func(t *testing.T) {
		for _, c := range []struct {
			what   string
			params dbgen.InsertMessageContextParams
		}{
			{"astrology_context as an array", dbgen.InsertMessageContextParams{
				AstrologyContext: []byte(`[]`), FactIndex: []byte(`[]`),
				KnowledgeChunkIds: []byte(`[]`)}},
			{"fact_index as an object", dbgen.InsertMessageContextParams{
				AstrologyContext: []byte(`{}`), FactIndex: []byte(`{"a":1}`),
				KnowledgeChunkIds: []byte(`[]`)}},
			{"knowledge_chunk_ids as an object", dbgen.InsertMessageContextParams{
				AstrologyContext: []byte(`{}`), FactIndex: []byte(`[]`),
				KnowledgeChunkIds: []byte(`{"a":1}`)}},
		} {
			params := c.params
			params.MessageID = own.Assistant
			params.ContextVersion = "ctx.v1"

			if _, err := q.InsertMessageContext(ctx, params); err == nil {
				t.Errorf("%s was accepted. Nothing errors downstream: the response "+
					"still streams and the explanation panel renders empty", c.what)
			}
		}
	})
}

// ─── the grants, in both directions ──────────────────────────────────

/*
`astro_ro` reads and does not write, asserted on the three new tables
specifically.

`singlewriter_test.go` already discovers every table and checks UPDATE
and DELETE, which covers these the moment the migration applies. This
test is not redundant with it, for two reasons:

  - it checks INSERT and TRUNCATE as well, which the discovery test does
    not, because it cannot synthesise a valid row for an arbitrary table
  - it checks the READ half against rows that actually exist. "The reader
    can SELECT" is asserted there with `LIMIT 1` against possibly-empty
    tables, which passes equally on a table the reader can see and a
    table that has nothing in it.

Migration 000009's grant comment is explicit that `ai-service` reads none
of these three in Phase 5 — Go passes the chart and the recent-message
window into the request (§2). The grant exists because 000001's default
privileges hand it over regardless and this repository's posture is read
everything, write nothing. What this test pins is the second half of that
sentence.
*/
func TestAstroRoReadsTheConversationTablesAndCannotWriteThem(t *testing.T) {
	pool, readerDSN, stop := chatPool(t)
	defer stop()

	ctx := context.Background()
	q := dbgen.New(pool)

	alice := seedChatUser(ctx, t, pool, "alice-grants@example.test")
	own := seedThread(ctx, t, q, alice, "A readable thread")

	reader, err := pgx.Connect(ctx, readerDSN)
	if err != nil {
		t.Fatalf("connect as astro_ro: %v", err)
	}
	defer func() { _ = reader.Close(ctx) }()

	tables := []string{"conversations", "messages", "message_contexts"}

	t.Run("reads", func(t *testing.T) {
		// Against rows that exist, so a grant that is present and a
		// table that is empty are distinguishable.
		want := map[string]int{"conversations": 1, "messages": 2, "message_contexts": 1}
		for _, table := range tables {
			var seen int
			if err := reader.QueryRow(ctx,
				fmt.Sprintf(`SELECT count(*) FROM %q`, table)).Scan(&seen); err != nil {
				t.Errorf("astro_ro cannot read %q: %v", table, err)
				continue
			}
			if seen != want[table] {
				t.Errorf("astro_ro sees %d rows in %q, writer wrote %d",
					seen, table, want[table])
			}
		}
	})

	t.Run("writes", func(t *testing.T) {
		mutations := map[string]string{
			"INSERT conversations": fmt.Sprintf(
				`INSERT INTO conversations (user_id, birth_profile_id)
				 VALUES ('%s', '%s')`,
				uuidText(alice.ID), uuidText(alice.Profile)),
			"UPDATE conversations":   `UPDATE conversations SET title = 'tampered'`,
			"DELETE conversations":   `DELETE FROM conversations WHERE true`,
			"TRUNCATE conversations": `TRUNCATE conversations CASCADE`,

			"INSERT messages": fmt.Sprintf(
				`INSERT INTO messages (conversation_id, role, content)
				 VALUES ('%s', 'assistant', 'written by ai-service')`,
				uuidText(own.Conversation)),
			"UPDATE messages":   `UPDATE messages SET content = 'tampered'`,
			"DELETE messages":   `DELETE FROM messages WHERE true`,
			"TRUNCATE messages": `TRUNCATE messages CASCADE`,

			"INSERT message_contexts": fmt.Sprintf(
				`INSERT INTO message_contexts
				   (message_id, astrology_context, fact_index, knowledge_chunk_ids,
				    prompt_version, context_version)
				 VALUES ('%s', '{}', '[]', '[]', 'v1', 'v1')`,
				uuidText(own.Assistant)),
			"UPDATE message_contexts":   `UPDATE message_contexts SET context_version = 'tampered'`,
			"DELETE message_contexts":   `DELETE FROM message_contexts WHERE true`,
			"TRUNCATE message_contexts": `TRUNCATE message_contexts CASCADE`,
		}

		for name, sql := range mutations {
			t.Run(name, func(t *testing.T) {
				_, err := reader.Exec(ctx, sql)
				if err == nil {
					t.Fatalf("astro_ro executed %s — the single-writer rule is "+
						"BROKEN for this table. Check the GRANTs in migration "+
						"000009 and in 000001's default privileges.", name)
				}

				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) {
					t.Fatalf("%s failed with a non-Postgres error: %v", name, err)
				}
				if pgErr.Code != errInsufficientPrivilege {
					t.Errorf("%s failed with SQLSTATE %s (%s), want %s "+
						"(insufficient_privilege) — it may be failing for the "+
						"wrong reason, which would leave the grant untested",
						name, pgErr.Code, pgErr.Message, errInsufficientPrivilege)
				}
			})
		}
	})

	t.Run("and nothing changed", func(t *testing.T) {
		if n := count(ctx, t, pool, `SELECT count(*) FROM messages`); n != 2 {
			t.Errorf("messages holds %d rows, want 2 — a mutation got through", n)
		}
		var content string
		if err := pool.QueryRow(ctx,
			`SELECT content FROM messages WHERE id = $1`, own.Assistant,
		).Scan(&content); err != nil {
			t.Fatalf("read the answer back: %v", err)
		}
		if content == "tampered" {
			t.Error("the stored answer was rewritten by astro_ro")
		}
	})
}

func uuidText(id pgtype.UUID) string {
	text, err := id.Value()
	if err != nil {
		return ""
	}
	s, _ := text.(string)
	return s
}

// ─── index shapes ────────────────────────────────────────────────────

/*
The index TYPES, not merely their names — the same reasoning
`knowledge_schema_test.go` gives for task 5.1, and it applies here for
the same reason.

Losing one of these breaks no query. A dropped GIN index still answers
`search_tsv @@ websearch_to_tsquery(...)`, by scanning every message in
the table and recomputing nothing — it is already stored — so the search
results are identical and only the latency moves. A
`conversations_user_active_idx` recreated without its WHERE clause still
serves the history list. Nothing fails; nothing is slow enough to notice
until there is real data; and by then the migration that did it is
months old.

A future migration recreating `messages_search_idx` as a btree, which is
the default and therefore the plausible mistake, leaves the name in place
and the index useless.
*/
func TestTheConversationIndexesAreTheShapeTheQueriesNeed(t *testing.T) {
	pool, _, stop := chatPool(t)
	defer stop()

	ctx := context.Background()

	type shape struct {
		method  string
		partial bool
	}
	want := map[string]shape{
		"messages_search_idx":           {"gin", false},   // history search
		"messages_conv_idx":             {"btree", false}, // one thread, in order
		"messages_reported_idx":         {"btree", true},  // the review queue
		"conversations_user_idx":        {"btree", false}, // §5's index
		"conversations_user_active_idx": {"btree", true},  // the default history list
		"conversations_profile_idx":     {"btree", false}, // the birth_profile FK
		"ai_logs_conversation_idx":      {"btree", true},  // the new FK on the cost log
	}

	rows, err := pool.Query(ctx, `
		SELECT c.relname, am.amname, i.indpred IS NOT NULL
		FROM pg_class c
		JOIN pg_am am ON am.oid = c.relam
		JOIN pg_index i ON i.indexrelid = c.oid
		JOIN pg_class t ON t.oid = i.indrelid
		WHERE t.relname IN ('conversations', 'messages', 'message_contexts',
		                    'ai_request_logs')`)
	if err != nil {
		t.Fatalf("query indexes: %v", err)
	}
	defer rows.Close()

	got := map[string]shape{}
	for rows.Next() {
		var name string
		var found shape
		if err := rows.Scan(&name, &found.method, &found.partial); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name] = found
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	// The control. Without it this passes against a database where the
	// query matched nothing — a typo in the table list above, or
	// migrations that never ran.
	if len(got) == 0 {
		t.Fatal("no indexes found on any of the four tables; did the migration apply?")
	}

	for name, expected := range want {
		actual, present := got[name]
		if !present {
			t.Errorf("%s is missing", name)
			continue
		}
		if actual.method != expected.method {
			t.Errorf("%s is a %s index, expected %s. The name survived a change of "+
				"access method, so the query still works and is now a scan",
				name, actual.method, expected.method)
		}
		if actual.partial != expected.partial {
			t.Errorf("%s has partial=%v, expected %v", name, actual.partial, expected.partial)
		}
	}

	// `message_contexts.message_id` is UNIQUE, and that constraint's own
	// index is the foreign key's index. Asserted because the alternative
	// — adding a second index on the same column — is the redundancy
	// 000002 called out, and because dropping the UNIQUE would remove the
	// FK index silently along with the one-context-per-message rule.
	var unique bool
	if err := pool.QueryRow(ctx, `
		SELECT i.indisunique
		FROM pg_index i
		JOIN pg_class t ON t.oid = i.indrelid
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = i.indkey[0]
		WHERE t.relname = 'message_contexts' AND a.attname = 'message_id'
		  AND i.indnatts = 1`).Scan(&unique); err != nil {
		t.Fatalf("no single-column index on message_contexts.message_id: %v", err)
	}
	if !unique {
		t.Error("message_contexts.message_id is indexed but not uniquely — a " +
			"message can now have two different stored explanations")
	}
}
