# Runbook — changing the embedding dimension

**Task 5.5.** Written while the corpus is small, which is the whole point: PHASE-05 §4
says to write this migration now rather than discovering it at 100k chunks.

Read the next two paragraphs before touching anything.

---

## What you are actually doing

pgvector columns are **fixed-dimension**. `knowledge_chunks.embedding` is
`vector(768)` because `nomic-embed-text` produces 768 floats. Moving to a model with a
different output width is not a type change — it is a **full rebuild of every vector in
the product**.

There is no transformation from a 768-dimension vector to a 1024-dimension one. The
models have different training and different geometry; padding or truncating gives you
vectors of the right shape that are semantically meaningless, and *nothing will error*.
Retrieval will keep working and keep being subtly wrong. **Every chunk has to be
re-embedded.**

## What not to do

```sql
ALTER TABLE knowledge_chunks ALTER COLUMN embedding TYPE vector(1024);
```

This fails, because pgvector cannot cast between widths. That is the *good* outcome.
The bad outcome is somebody fixing that failure with a drop-and-add, which succeeds,
discards every vector in the corpus, and leaves a knowledge base that keyword-searches
fine and vector-searches not at all. `TestTheSwapNeverAltersTheColumnTypeInPlace` is
what keeps that statement out of the generated SQL.

---

## The procedure

Four phases. Run them one at a time — `rewidth-kb` refuses more than one phase per
invocation, so the destructive step cannot run in the same process as the step whose
output it depends on.

### Before you start

- [ ] `ai-service` is serving the **new** model, and `EMBEDDING_DIM` matches it
- [ ] You know the new width. Confirm it rather than trusting a model card:
      `curl -s localhost:8200/v1/embed -H "X-Internal-Token: $INTERNAL_TOKEN" \
       -H 'content-type: application/json' -d '{"texts":["probe"]}' | jq .dimensions`
- [ ] A database backup exists. Phase 4 is not reversible (see Rollback)
- [ ] You have the corpus checksum from before: `go run ./cmd/ingest-kb --check`

### Phase 1 — add the shadow column

```bash
cd services/api
go run ./cmd/rewidth-kb --to 1024 --emit    # writes the migration pair
# review both files
task migrate
```

Adds `embedding_v2 vector(1024)` and a partial index on the rows still to do. The live
column is untouched, nothing is locked for long — a nullable column needs no table
rewrite — and retrieval keeps working throughout. **Safe on a live system.**

Re-runnable: both statements are `IF NOT EXISTS`, so running it twice is a no-op rather
than an error that reads as "something is broken".

### Phase 2 — backfill

```bash
go run ./cmd/rewidth-kb --to 1024 --backfill
```

Re-embeds every chunk through `ai-service` into the shadow column, in batches of
`KB_EMBED_BATCH_SIZE`. Resumable: it selects `WHERE embedding_v2 IS NULL ORDER BY id`,
so an interrupted run continues rather than restarting.

This reads chunk text **from the database**, not from `packages/content/knowledge/`.
Deliberate: this phase must change the vectors and nothing else. Re-chunking at the
same time would move boundaries *and* change the model, and the two effects on
retrieval quality would be impossible to separate afterwards. Re-chunking is
`ingest-kb --apply --force`, which is a different operation on a different day.

Expect minutes at the Phase 5 corpus size. Hours at 100k chunks, which is §4's point.

### Phase 3 — the parity check

```bash
go run ./cmd/rewidth-kb --to 1024 --verify
```

Read-only, and the gate. It refuses the swap when:

| finding | why it blocks |
|---|---|
| any chunk has no shadow vector | the swap would drop its live vector and leave it unembedded — invisible to vector search, still matching keyword search, which reads as a ranking mystery rather than as missing data |
| a shadow vector is the wrong width | the model behind `ai-service` is not the one this plan is for |
| the corpus holds more than one `embedding_model` | cosine similarity across models is a number with no meaning, so ranking would be arbitrary and nothing would look wrong |
| every shadow vector is byte-identical to the live one | the backfill copied the column instead of re-embedding, or `ai-service` is still on the old model |
| the corpus is empty | almost always the wrong `DATABASE_URL`, and finding that out after swapping the right one is worse |

That last check exists because `UPDATE knowledge_chunks SET embedding_v2 = embedding`
is the shortcut somebody reaches for when the model is slow, and every other check
passes against it.

### Phase 4 — the swap

```bash
go run ./cmd/rewidth-kb --to 1024 --swap
```

Destructive. Re-runs the parity check first and refuses if it does not pass — the gate
is only a gate if it cannot be skipped, and "I ran verify a minute ago" is not a
property of the database.

In one transaction: drop the old HNSW index, drop the old column, rename
`embedding_v2` → `embedding`, rebuild `kc_embedding_idx` (HNSW, `vector_cosine_ops`)
and `kc_unembedded_idx`. One transaction because partway through, the table has no
usable `embedding` column at all, and stopping there gives a knowledge base that every
retrieval query errors against.

### After

- [ ] `go run ./cmd/ingest-kb --check` — the corpus still loads
- [ ] `go run ./cmd/ingest-kb --apply` — reports everything unchanged (the checksums
      still match; only the vectors moved)
- [ ] Update `EMBEDDING_DIM` in `.env` / `.env.example` and the `vector(768)` comment in
      `db/migrations/000007_knowledge_base.up.sql`
- [ ] Re-run the retrieval quality set. **Recall@8 will have changed** — a different
      model is a different ranking, and that is the whole reason for doing this.
      Compare against the stored baseline rather than against memory.
- [ ] `task test:integration` — `knowledge_schema_test.go` asserts the index *types*,
      which is what catches an index that came back as a btree

---

## Rollback

**Before the swap**: `task migrate:down` once. Drops the shadow column, costs you the
backfill, and loses no data — the live column was never touched.

**After the swap**: there is no migration that helps you. The old column is gone and
its vectors cannot be recomputed from the new ones. Two real options:

1. **Restore the database backup.** The reason it is on the pre-flight checklist.
2. **Rebuild the corpus from source**, against a model with the old width:

   ```bash
   go run ./cmd/rewidth-kb --to 768 --emit && task migrate
   go run ./cmd/rewidth-kb --to 768 --backfill --verify --swap   # one phase at a time
   ```

   Or, more simply, re-run ingestion: `go run ./cmd/ingest-kb --apply --force`.
   `packages/content/knowledge/` is the source of truth and is in git, which is why
   `ingest-kb` is idempotent and why this is a real rollback plan rather than a
   sentence in a PR description.

What you cannot recover is anything that was in the vectors and not in the corpus —
which is nothing, by design. That property is worth protecting the next time somebody
proposes a vector the ingester does not derive from a committed file.

---

## How this is tested

`cmd/rewidth-kb/rewidth_integration_test.go` runs **all four phases against a
throwaway Postgres with the real migrations and a real embedded corpus in it**, and
asserts:

- `embedding` ends up `vector(N)` under the same name, `embedding_v2` is gone
- the chunk count and an `md5` of all chunk text are **unchanged** across the swap —
  a dimension change must not cost a single chunk
- no chunk is left unembedded
- `kc_embedding_idx` is **hnsw** and `kc_unembedded_idx` is **btree** afterwards, by
  access method rather than by name: a recreated index that came back as a btree keeps
  its name and answers `ORDER BY embedding <=> $1` by scanning the whole corpus.
  Nothing fails, recall is identical, only latency changes
- vector search returns neighbours afterwards
- the swap is **refused** with one chunk unbackfilled, with wrong-width vectors, with a
  copied column, and against an empty corpus

Phase 1 is applied twice in that test, because an operator unsure whether it worked
runs it again.

The unit tests cover the part that is cheap to get wrong and impossible to notice in
review: statement **order** within the swap.
