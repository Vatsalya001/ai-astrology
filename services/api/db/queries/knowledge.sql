-- Knowledge-base queries. PHASE-05 tasks 5.1 and 5.4.
--
-- Written by `cmd/ingest-kb` only. `ai-service` reads these tables as
-- `astro_ro` through asyncpg (task 5.6) rather than through here — the
-- single-writer rule means Go owns the writes, not that Go owns every
-- read.
--
-- ── Why the embedding is a string in Go ──
--
-- Left to itself, sqlc maps `vector(768)` to `*pgvector.Vector` and pulls
-- in github.com/pgvector/pgvector-go — a new direct dependency that also
-- has to be registered with every pgx pool before the type encodes. It is
-- overridden to `string` in sqlc.yaml, and the vector travels as
-- pgvector's own documented text form, `'[0.1,0.2,…]'`, cast here.
--
-- The reason is that no Go code in this service does arithmetic on an
-- embedding. It receives 768 floats from ai-service, hands them to
-- Postgres, and never looks at them again. Postgres does the parsing and
-- the validation — including rejecting a vector of the wrong width, which
-- is the failure that actually matters.
--
-- The `::vector` cast is NOT cosmetic. Without it the insert fails
-- outright, which is the good outcome; the bad one would be a column loose
-- enough to accept the string.

-- ─── Ingestion ───────────────────────────────────────────────────────

-- name: UpsertKnowledgeDocument :one
-- Insert or update one authored document, keyed by its identity.
--
-- ON CONFLICT against `kd_identity_idx` (title, language, version) is
-- what makes ingestion re-runnable: a second run over an unchanged corpus
-- updates rows in place rather than doubling the corpus, which would show
-- up as retrieval returning the same passage twice and read as a ranking
-- bug.
--
-- `created_at` is deliberately not touched on update. "When did this
-- document first enter the corpus" is a different question from "when was
-- it last ingested", and overwriting the first with the second loses it.
INSERT INTO knowledge_documents (
    title, category, content, language, source,
    authority, astrology_system, metadata, source_checksum
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (title, language, version) DO UPDATE SET
    category         = EXCLUDED.category,
    content          = EXCLUDED.content,
    source           = EXCLUDED.source,
    authority        = EXCLUDED.authority,
    astrology_system = EXCLUDED.astrology_system,
    metadata         = EXCLUDED.metadata,
    source_checksum  = EXCLUDED.source_checksum,
    is_active        = TRUE,
    updated_at       = now()
RETURNING *;

-- name: GetKnowledgeDocumentChecksum :one
-- What the database already holds for this identity.
--
-- Returns the checksum and the chunk count together because both have to
-- match before a re-run can skip a document. A matching checksum with
-- zero chunks is a previous run that failed between the document insert
-- and the chunk inserts — and skipping that leaves a document in the
-- corpus with nothing retrievable in it.
SELECT d.source_checksum,
       (SELECT count(*) FROM knowledge_chunks c WHERE c.document_id = d.id) AS chunk_count
FROM knowledge_documents d
WHERE d.title = $1 AND d.language = $2 AND d.version = $3;

-- name: DeleteKnowledgeChunks :execrows
-- Clear a document's chunks before re-inserting them.
--
-- Replace rather than upsert, because re-chunking can produce a DIFFERENT
-- NUMBER of chunks. An upsert keyed on (document_id, chunk_index) would
-- leave the tail of the previous run behind — orphan chunks at indexes the
-- new run never reached, still matching keyword search, still being
-- retrieved. They would be the only chunks in the corpus whose text no
-- authored file contains.
DELETE FROM knowledge_chunks WHERE document_id = $1;

-- name: InsertKnowledgeChunk :exec
-- One chunk, with its vector.
--
-- See the note at the top of this file for why the vector is a string
-- here and cast in the SQL.
INSERT INTO knowledge_chunks (
    document_id, content, chunk_index, token_count, embedding, embedding_model, metadata
) VALUES (
    sqlc.arg(document_id), sqlc.arg(content), sqlc.arg(chunk_index), sqlc.arg(token_count),
    sqlc.arg(embedding)::vector, sqlc.arg(embedding_model), sqlc.arg(metadata)
);

-- ─── Operational checks ──────────────────────────────────────────────

-- name: CountKnowledgeCorpus :one
-- The two numbers the phase gate asks for (§16: ≥400 documents, chunked,
-- embedded) plus the one that says whether it is sound.
--
-- `unembedded` reads through `kc_unembedded_idx`, a partial index on
-- `embedding IS NULL`. In a healthy corpus it is empty, which is the
-- point: an unembedded chunk is invisible to vector search while still
-- matching keyword search, and that half-presence reads as a ranking
-- mystery rather than as missing data.
SELECT
    (SELECT count(*) FROM knowledge_documents WHERE is_active) AS documents,
    (SELECT count(*) FROM knowledge_chunks)                    AS chunks,
    (SELECT count(*) FROM knowledge_chunks
      WHERE embedding IS NULL)                                 AS unembedded,
    (SELECT count(DISTINCT embedding_model) FROM knowledge_chunks) AS embedding_models;

-- name: ListKnowledgeChecksums :many
-- Every stored document's identity and checksum, for verifying the corpus
-- against what is on disk.
SELECT title, language, version, source_checksum
FROM knowledge_documents
ORDER BY title, language, version;
