-- Phase 5 task 5.1 — the astrology knowledge base.
--
-- Two retrieval systems feed a chat answer and this is the GENERAL half:
-- the corpus of astrology rules. The other half is the user's computed
-- chart, which lives in `charts` and is exact. PHASE-05 §2 is emphatic
-- about the distinction, because the failure modes differ — a wrong
-- chart fact is a wrong claim about a real person, a wrong retrieval is
-- generic advice.
--
-- `api-service` owns this schema; `ai-service` reads it as `astro_ro`
-- (ADR-001). Retrieval is the one place Python touches Postgres
-- directly, and it is a SELECT.
--
-- ── Why two tables ──
--
-- A document is the unit a human authors and reviews — "Saturn in the
-- 10th house". A chunk is the unit retrieval returns. Keeping them apart
-- means the corpus can be re-chunked (different window, different
-- overlap) without re-authoring, and a chunk can always be traced back
-- to a document a person signed off. `ON DELETE CASCADE` so retiring a
-- document cannot leave orphaned chunks that retrieval would still
-- return.

CREATE TABLE knowledge_documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    title TEXT NOT NULL,

    -- planets | signs | houses | nakshatras | dashas | yogas | transits
    -- | aspects | remedies | career | marriage | ...
    --
    -- TEXT rather than an enum, deliberately. The category list grows as
    -- the corpus does, and an enum means a migration to add "muhurta" —
    -- which is how a corpus stops growing. The CHECK below bounds it
    -- enough to catch a typo without needing DDL for a new topic.
    category TEXT NOT NULL,

    content TEXT NOT NULL,

    language TEXT NOT NULL DEFAULT 'en',

    -- "Brihat Parashara Hora Shastra" | "editorial" | ...
    --
    -- NOT NULL and no default, on purpose. PHASE-05 §4 rules out
    -- scraped and copyrighted material, and a corpus where provenance is
    -- optional is a corpus nobody can audit for licence cleanliness
    -- later. If you cannot name where a document came from, it does not
    -- go in.
    source TEXT NOT NULL,

    -- 0–100. Classical sources outrank editorial when the retriever has
    -- to choose, which is what stops a paraphrase displacing the text it
    -- paraphrases.
    authority SMALLINT NOT NULL DEFAULT 50,

    astrology_system TEXT NOT NULL DEFAULT 'vedic',

    -- {planet, house, sign, nakshatra, topic[]} — the filter surface.
    -- Retrieval narrows by these BEFORE ranking, so a career question
    -- does not compete against nakshatra material it can never use.
    metadata JSONB NOT NULL DEFAULT '{}',

    -- Retire a document without deleting it. A deletion loses the
    -- reason it was ever included; a flag keeps the audit trail and
    -- excludes it from retrieval in one place.
    is_active BOOLEAN NOT NULL DEFAULT TRUE,

    version INTEGER NOT NULL DEFAULT 1,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Bounds that catch an ingestion bug rather than a human typo.
    CONSTRAINT kd_authority_range CHECK (authority BETWEEN 0 AND 100),
    CONSTRAINT kd_title_not_blank CHECK (length(btrim(title)) > 0),
    CONSTRAINT kd_content_not_blank CHECK (length(btrim(content)) > 0),
    CONSTRAINT kd_source_not_blank CHECK (length(btrim(source)) > 0),
    CONSTRAINT kd_language_lower CHECK (language = lower(language))
);

-- The retrieval filter, in the order the retriever uses it: narrow by
-- category and language, exclude retired documents.
CREATE INDEX kd_category_idx ON knowledge_documents (category, language, is_active);

-- Ingestion is re-runnable, and this is what makes it so: the same
-- authored file produces the same (title, language, version) and
-- upserts rather than duplicating. Without it a second `ingest-kb` run
-- doubles the corpus and retrieval starts returning the same passage
-- twice, which reads as a ranking bug.
CREATE UNIQUE INDEX kd_identity_idx ON knowledge_documents (title, language, version);


CREATE TABLE knowledge_chunks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    document_id UUID NOT NULL
        REFERENCES knowledge_documents (id) ON DELETE CASCADE,

    content TEXT NOT NULL,
    chunk_index INTEGER NOT NULL,
    token_count INTEGER NOT NULL,

    -- 768 = nomic-embed-text, which EMBEDDING_DIM must agree with.
    --
    -- pgvector columns are FIXED dimension, so changing model means a
    -- migration and not a config edit. Task 5.5 owns that script; the
    -- note in PHASE-05 §4 is worth reading before anyone swaps the
    -- model: add `embedding_v2 vector(N)`, backfill, switch reads, drop
    -- the old column. Nullable so a chunk can be inserted and embedded
    -- in the same transaction without ordering games — but see the
    -- partial index below, which is what stops an unembedded chunk
    -- silently never being retrieved.
    embedding vector(768),

    -- Which model produced the vector. Without it, a corpus embedded
    -- across a model change is unfixable: there is no way to tell which
    -- rows need re-embedding, and mixed-model vectors rank against each
    -- other meaninglessly.
    embedding_model TEXT NOT NULL,

    metadata JSONB NOT NULL DEFAULT '{}',

    -- GENERATED, so the keyword index maintains itself. A trigger is the
    -- alternative and a trigger is a thing to forget — PHASE-05 §4 says
    -- exactly this.
    --
    -- 'english' is hardcoded and that is a known limit: a Hindi corpus
    -- needs its own column or a different configuration, and this
    -- config would stem Hindi text as though it were English. Phase 5
    -- ships `en` only (§1), and the day it does not, this line is the
    -- one to change.
    tsv tsvector GENERATED ALWAYS AS (to_tsvector('english', content)) STORED,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT kc_chunk_index_non_negative CHECK (chunk_index >= 0),
    CONSTRAINT kc_token_count_positive CHECK (token_count > 0),
    CONSTRAINT kc_content_not_blank CHECK (length(btrim(content)) > 0),
    CONSTRAINT kc_model_not_blank CHECK (length(btrim(embedding_model)) > 0),

    -- One chunk per position per document. Re-ingesting a document
    -- replaces its chunks; without this, a partial failure mid-ingest
    -- leaves duplicates at the same index and retrieval returns the same
    -- text twice.
    CONSTRAINT kc_position_unique UNIQUE (document_id, chunk_index)
);

-- ── The three index types PHASE-05 §10 task 5.1 requires ──

-- 1. Vector. HNSW rather than IVFFlat: it needs no training pass, so an
--    empty table is immediately usable and ingestion order does not
--    affect recall. Cosine ops because the embeddings are normalised.
CREATE INDEX kc_embedding_idx ON knowledge_chunks
    USING hnsw (embedding vector_cosine_ops);

-- 2. Keyword, over the generated column.
CREATE INDEX kc_tsv_idx ON knowledge_chunks USING gin (tsv);

-- 3. Metadata. `jsonb_path_ops` rather than the default `jsonb_ops`:
--    smaller and faster for the containment queries the retriever
--    actually issues (`metadata @> '{"planet":"saturn"}'`). It cannot
--    serve key-existence queries (`?`), which the retriever does not
--    use — if that changes, this operator class is the reason it
--    suddenly sequential-scans.
CREATE INDEX kc_meta_idx ON knowledge_chunks USING gin (metadata jsonb_path_ops);

-- Every chunk that still needs a vector, cheaply.
--
-- Ingestion inserts chunks and embeddings together, so in a healthy
-- corpus this index is empty — which is the point. It makes "did
-- anything fail to embed?" a fast lookup rather than a full scan, and an
-- unembedded chunk is invisible to vector search while still matching
-- keyword search, which is the kind of half-presence that reads as a
-- ranking mystery.
CREATE INDEX kc_unembedded_idx ON knowledge_chunks (document_id)
    WHERE embedding IS NULL;

-- Retrieval joins chunks to their document to read authority and
-- category. Without this the join is a sequential scan over chunks.
CREATE INDEX kc_document_idx ON knowledge_chunks (document_id);


-- ai-service reads both tables and writes neither. Granted explicitly
-- rather than relying on a default: `01-init.sql` grants SELECT on
-- existing tables at container init, and these tables did not exist
-- then. A missing grant here surfaces as a permission error inside
-- retrieval at the first chat request.
GRANT SELECT ON knowledge_documents TO astro_ro;
GRANT SELECT ON knowledge_chunks TO astro_ro;
