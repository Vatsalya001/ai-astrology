//go:build integration

package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

/*
PHASE-05 task 5.1, done when "all three index types present".

That criterion is unusual in this repository in that it names the
IMPLEMENTATION rather than a behaviour, and for once that is right:
hybrid retrieval needs a vector index, a keyword index and a metadata
index, and losing any one of them does not break a query — it makes it
slow and quietly worse.

A dropped HNSW index still answers `ORDER BY embedding <=> $1`, by
sequential-scanning the corpus and computing every distance. A dropped
GIN index still answers `metadata @> ...`. Nothing fails; recall stays
identical; the retrieval quality set still passes. The only symptom is
latency, on a query path that runs once per chat message, and latency is
exactly what nobody notices until the corpus is large.

So the index TYPES are asserted, not merely their names. A future
migration that recreates `kc_embedding_idx` as a btree — which is a
plausible thing to do while fixing something else, since btree is the
default — leaves the name in place and the index useless. Only the access
method distinguishes them.
*/
func TestTheKnowledgeBaseHasAllThreeIndexTypes(t *testing.T) {
	ctx := context.Background()
	writerDSN, _, terminate := startPostgres(ctx, t)
	defer terminate()

	conn, err := pgx.Connect(ctx, writerDSN)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	// The real migrations, read from disk — the same helper the
	// single-writer tests use. `startPostgres` gives a bare container
	// with only `01-init.sql` applied, which is what the control at the
	// bottom of this test caught on the first run.
	applyMigrations(ctx, t, conn)

	// index name -> the access method it must use
	want := map[string]string{
		"kc_embedding_idx": "hnsw",  // vector similarity
		"kc_tsv_idx":       "gin",   // keyword, over the generated tsvector
		"kc_meta_idx":      "gin",   // metadata containment
		"kd_category_idx":  "btree", // the retrieval filter
		"kd_identity_idx":  "btree", // re-ingestion idempotency
	}

	rows, err := conn.Query(ctx, `
		SELECT c.relname, am.amname
		FROM pg_class c
		JOIN pg_am am ON am.oid = c.relam
		JOIN pg_index i ON i.indexrelid = c.oid
		JOIN pg_class t ON t.oid = i.indrelid
		WHERE t.relname IN ('knowledge_chunks', 'knowledge_documents')
	`)
	if err != nil {
		t.Fatalf("query indexes: %v", err)
	}
	defer rows.Close()

	got := map[string]string{}
	for rows.Next() {
		var name, method string
		if err := rows.Scan(&name, &method); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name] = method
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	for name, method := range want {
		actual, present := got[name]
		if !present {
			t.Errorf("%s is missing — hybrid retrieval needs it", name)
			continue
		}
		if actual != method {
			t.Errorf(
				"%s is a %s index, expected %s. The name survived a change of "+
					"access method, so the query still works and is now a scan",
				name, actual, method,
			)
		}
	}

	// The control. Without it this test passes on a database where the
	// query returned nothing at all — a typo in the table names above,
	// or a migration that never ran.
	if len(got) == 0 {
		t.Fatal("no indexes found on either table; did the migration apply?")
	}
}

/*
The generated tsvector column maintains itself, and this proves it.

PHASE-05 §4 chose GENERATED over a trigger because "a generated
`tsvector` column means the keyword index maintains itself — no trigger
to forget". A trigger that exists and does not fire looks exactly like
this column working, so the distinction is worth an assertion: insert a
row, never mention `tsv`, and require it to be populated and searchable.
*/
func TestTheTsvectorIsGeneratedWithoutATrigger(t *testing.T) {
	ctx := context.Background()
	writerDSN, _, terminate := startPostgres(ctx, t)
	defer terminate()

	conn, err := pgx.Connect(ctx, writerDSN)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	applyMigrations(ctx, t, conn)

	var docID string
	err = conn.QueryRow(ctx, `
		INSERT INTO knowledge_documents (title, category, content, source)
		VALUES ('Saturn in the tenth house', 'houses', 'placeholder', 'editorial')
		RETURNING id
	`).Scan(&docID)
	if err != nil {
		t.Fatalf("insert document: %v", err)
	}

	// `tsv` is deliberately not in the column list.
	_, err = conn.Exec(ctx, `
		INSERT INTO knowledge_chunks
			(document_id, content, chunk_index, token_count, embedding_model)
		VALUES ($1, 'Saturn in the tenth house delays recognition', 0, 7, 'nomic-embed-text')
	`, docID)
	if err != nil {
		t.Fatalf("insert chunk: %v", err)
	}

	// Searchable by a stemmed term — `delays` must match `delay`, which
	// is the whole reason this is a tsvector and not a LIKE.
	var matches int
	err = conn.QueryRow(ctx, `
		SELECT count(*) FROM knowledge_chunks
		WHERE tsv @@ to_tsquery('english', 'delay')
	`).Scan(&matches)
	if err != nil {
		t.Fatalf("keyword search: %v", err)
	}
	if matches != 1 {
		t.Fatalf("keyword search matched %d rows, expected 1 — tsv was not generated", matches)
	}

	// And a term that is not there does not match, so the assertion
	// above is not passing on a column that matches everything.
	err = conn.QueryRow(ctx, `
		SELECT count(*) FROM knowledge_chunks
		WHERE tsv @@ to_tsquery('english', 'marriage')
	`).Scan(&matches)
	if err != nil {
		t.Fatalf("negative keyword search: %v", err)
	}
	if matches != 0 {
		t.Fatalf("a term absent from the text matched %d rows", matches)
	}
}

/*
Re-ingestion must not duplicate the corpus.

`cmd/ingest-kb` is re-runnable by design, and the thing that makes that
safe is `kd_identity_idx`. Without it a second run inserts every document
again, retrieval starts returning the same passage twice, and it reads as
a ranking bug rather than as an ingestion one.

Asserted at the database level rather than in the ingester, because the
ingester is the thing most likely to be rewritten.
*/
func TestADocumentCannotBeIngestedTwice(t *testing.T) {
	ctx := context.Background()
	writerDSN, _, terminate := startPostgres(ctx, t)
	defer terminate()

	conn, err := pgx.Connect(ctx, writerDSN)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	applyMigrations(ctx, t, conn)

	insert := `
		INSERT INTO knowledge_documents (title, category, content, source, language, version)
		VALUES ('Mars in the seventh house', 'houses', 'body', 'editorial', 'en', 1)
	`
	if _, err := conn.Exec(ctx, insert); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := conn.Exec(ctx, insert); err == nil {
		t.Fatal("the same document was ingested twice — kd_identity_idx is not enforcing")
	}

	// A NEW VERSION of the same document is allowed: that is how a
	// correction ships without deleting the text it corrects.
	_, err = conn.Exec(ctx, `
		INSERT INTO knowledge_documents (title, category, content, source, language, version)
		VALUES ('Mars in the seventh house', 'houses', 'revised', 'editorial', 'en', 2)
	`)
	if err != nil {
		t.Fatalf("version 2 of the same title was refused: %v", err)
	}
}
