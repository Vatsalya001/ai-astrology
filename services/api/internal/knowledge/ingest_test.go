package knowledge

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
)

// ── The fakes ──
//
// A fake embedder rather than a real one, because `.claude/rules/testing.md`
// forbids CI from calling a language model. A fake STORE as well, so the
// pipeline's logic — skip, replace, batch, abort — is tested without
// Docker. The real Postgres path is covered separately by
// ingest_integration_test.go, which is where the `::vector` cast and the
// grants are checked, because those are things a fake cannot reproduce.

type fakeEmbedder struct {
	model string

	// batches records the size of every request, which is how the
	// batching assertions are made.
	batches []int

	// modelAfter, when > 0, switches the reported model name after that
	// many texts. For the mid-run model-change test.
	modelAfter int
	nextModel  string
	seen       int

	failAfter int
	err       error

	// shortBy drops this many vectors from every batch. The failure the
	// alignment checks exist for, and one the fake could not produce
	// until now — which is why the mutation that removed them survived.
	shortBy int
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, string, error) {
	f.batches = append(f.batches, len(texts))
	f.seen += len(texts)

	if f.err != nil && f.seen > f.failAfter {
		return nil, "", f.err
	}

	model := f.model
	if f.modelAfter > 0 && f.seen > f.modelAfter {
		model = f.nextModel
	}

	count := max(len(texts)-f.shortBy, 0)
	vectors := make([][]float32, count)
	for index := 0; index < count; index++ {
		vectors[index] = deterministicVector(texts[index])
	}
	return vectors, model, nil
}

// deterministicVector is a cheap hash spread over 8 dimensions.
//
// Deterministic so a test can assert that a specific chunk's text produced
// a specific stored vector — which is the only way to catch vectors being
// zipped against the wrong chunks, the failure mode that produces a corpus
// that retrieves confidently and wrongly.
func deterministicVector(text string) []float32 {
	var sum uint32
	for _, r := range text {
		sum = sum*31 + uint32(r)
	}
	vector := make([]float32, 8)
	for index := range vector {
		vector[index] = float32((sum>>(index*4))&0xff) / 255
	}
	return vector
}

type storedChunk struct {
	documentID string
	index      int
	content    string
	vector     string
	model      string
}

type fakeStore struct {
	// documents is keyed by identity, holding the assigned id.
	documents map[Identity]string
	checksums map[Identity]string
	chunks    map[string][]storedChunk

	nextID int

	// failOnChunk, when >= 0, makes InsertChunk fail at that index. For
	// the rollback test.
	failOnChunk int

	// committed counts successful transactions.
	committed int

	inTransaction bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		documents:   map[Identity]string{},
		checksums:   map[Identity]string{},
		chunks:      map[string][]storedChunk{},
		failOnChunk: -1,
	}
}

func (s *fakeStore) WithinTransaction(ctx context.Context, fn func(tx Store) error) error {
	if s.inTransaction {
		return errors.New("already inside a transaction")
	}

	// Snapshotted and restored on failure, which is the whole point of
	// modelling the transaction here at all: a fake that applied writes
	// immediately would make the rollback test pass against an
	// implementation that never opened a transaction.
	before := s.snapshot()

	s.inTransaction = true
	err := fn(s)
	s.inTransaction = false

	if err != nil {
		s.restore(before)
		return err
	}

	s.committed++
	return nil
}

func (s *fakeStore) snapshot() *fakeStore {
	copied := newFakeStore()
	copied.nextID = s.nextID
	for key, value := range s.documents {
		copied.documents[key] = value
	}
	for key, value := range s.checksums {
		copied.checksums[key] = value
	}
	for key, value := range s.chunks {
		copied.chunks[key] = append([]storedChunk(nil), value...)
	}
	return copied
}

func (s *fakeStore) restore(from *fakeStore) {
	s.documents, s.checksums, s.chunks, s.nextID =
		from.documents, from.checksums, from.chunks, from.nextID
}

func (s *fakeStore) StoredChecksum(_ context.Context, identity Identity) (string, int, bool, error) {
	id, ok := s.documents[identity]
	if !ok {
		return "", 0, false, nil
	}
	return s.checksums[identity], len(s.chunks[id]), true, nil
}

func (s *fakeStore) UpsertDocument(_ context.Context, doc *Document) (string, error) {
	identity := doc.Identity()
	id, ok := s.documents[identity]
	if !ok {
		s.nextID++
		id = fmt.Sprintf("doc-%d", s.nextID)
		s.documents[identity] = id
	}
	s.checksums[identity] = doc.Checksum
	return id, nil
}

func (s *fakeStore) DeleteChunks(_ context.Context, documentID string) error {
	delete(s.chunks, documentID)
	return nil
}

func (s *fakeStore) InsertChunk(
	_ context.Context, documentID string, chunk Chunk, vector, model string,
) error {
	if s.failOnChunk >= 0 && chunk.Index == s.failOnChunk {
		return fmt.Errorf("simulated failure at chunk %d", chunk.Index)
	}
	s.chunks[documentID] = append(s.chunks[documentID], storedChunk{
		documentID: documentID,
		index:      chunk.Index,
		content:    chunk.Content,
		vector:     vector,
		model:      model,
	})
	return nil
}

// ── The tests ──

func TestIngestStoresEveryChunkWithTheVectorForItsOwnText(t *testing.T) {
	// The assertion that matters most in this file. Vectors are zipped
	// against chunks BY POSITION, and a misalignment produces a corpus
	// that retrieves confidently and wrongly with nothing erroring
	// anywhere. Asserting counts would not catch an off-by-one; asserting
	// that each stored vector is the one derived from that chunk's own
	// text would.
	corpus := chunkedCorpus(t)
	embedder := &fakeEmbedder{model: "nomic-embed-text"}
	store := newFakeStore()

	report, err := Ingest(context.Background(), corpus, embedder, store, options())
	if err != nil {
		t.Fatal(err)
	}

	if report.Ingested != len(corpus) {
		t.Errorf("ingested %d of %d documents", report.Ingested, len(corpus))
	}

	for _, document := range corpus {
		id := store.documents[document.Document.Identity()]
		stored := store.chunks[id]

		if len(stored) != len(document.Chunks) {
			t.Fatalf("%s: stored %d chunks, chunked %d",
				document.Document.Path, len(stored), len(document.Chunks))
		}

		for index, chunk := range document.Chunks {
			want := FormatVector(deterministicVector(chunk.Content))
			if stored[index].vector != want {
				t.Errorf("%s chunk %d holds the vector for different text",
					document.Document.Path, index)
			}
			if stored[index].content != chunk.Content {
				t.Errorf("%s chunk %d content does not match", document.Document.Path, index)
			}
			if stored[index].model != "nomic-embed-text" {
				t.Errorf("%s chunk %d model is %q", document.Document.Path,
					index, stored[index].model)
			}
		}
	}
}

func TestASecondRunOverAnUnchangedCorpusEmbedsNothing(t *testing.T) {
	// §16: "Ingestion is deterministic and re-runnable without
	// duplicates". Without the checksum check a re-run has to re-embed all
	// 600 documents to discover that none of them changed.
	corpus := chunkedCorpus(t)
	store := newFakeStore()

	first := &fakeEmbedder{model: "m"}
	if _, err := Ingest(context.Background(), corpus, first, store, options()); err != nil {
		t.Fatal(err)
	}

	second := &fakeEmbedder{model: "m"}
	report, err := Ingest(context.Background(), corpus, second, store, options())
	if err != nil {
		t.Fatal(err)
	}

	if len(second.batches) != 0 {
		t.Errorf("the second run sent %d embed requests, want 0", len(second.batches))
	}
	if report.Skipped != len(corpus) || report.Ingested != 0 {
		t.Errorf("report is %+v, want everything skipped", report)
	}
}

func TestAChangedDocumentIsReIngestedAndItsOldChunksAreGone(t *testing.T) {
	// Replace, never merge. Re-chunking can produce FEWER chunks, and an
	// upsert keyed on (document_id, chunk_index) would leave the previous
	// run's tail behind — orphan chunks still matching keyword search,
	// still retrieved, and the only chunks in the corpus whose text no
	// authored file contains.
	store := newFakeStore()

	long := synthetic(t, "Changing", strings.Repeat("A sentence that is here. ", 40))
	long.Checksum = "checksum-one"
	if _, err := Ingest(context.Background(),
		chunk(t, long, ChunkConfig{SizeTokens: 60, OverlapTokens: 10}),
		&fakeEmbedder{model: "m"}, store, options()); err != nil {
		t.Fatal(err)
	}

	id := store.documents[long.Identity()]
	chunksBefore := len(store.chunks[id])
	if chunksBefore < 3 {
		t.Fatalf("need several chunks for this test, got %d", chunksBefore)
	}

	short := synthetic(t, "Changing", "One short sentence now.")
	short.Checksum = "checksum-two"
	if _, err := Ingest(context.Background(),
		chunk(t, short, ChunkConfig{SizeTokens: 60, OverlapTokens: 10}),
		&fakeEmbedder{model: "m"}, store, options()); err != nil {
		t.Fatal(err)
	}

	after := store.chunks[id]
	if len(after) != 1 {
		t.Fatalf("stored %d chunks after shrinking the document to one, want 1; "+
			"the extra ones are orphans whose text no file contains", len(after))
	}
	if !strings.Contains(after[0].content, "One short sentence now.") {
		t.Errorf("the surviving chunk is not the new one: %q", after[0].content)
	}
}

func TestForceReEmbedsAnUnchangedDocument(t *testing.T) {
	// The escape hatch for the one change a checksum cannot see: the
	// CHUNKER changed, so the same bytes now produce different chunks.
	corpus := chunkedCorpus(t)
	store := newFakeStore()

	if _, err := Ingest(context.Background(), corpus,
		&fakeEmbedder{model: "m"}, store, options()); err != nil {
		t.Fatal(err)
	}

	embedder := &fakeEmbedder{model: "m"}
	opts := options()
	opts.Force = true

	report, err := Ingest(context.Background(), corpus, embedder, store, opts)
	if err != nil {
		t.Fatal(err)
	}

	if len(embedder.batches) == 0 {
		t.Error("--force sent no embed requests")
	}
	if report.Skipped != 0 {
		t.Errorf("--force skipped %d documents", report.Skipped)
	}
}

func TestAStoredDocumentWithNoChunksIsReIngestedEvenWhenTheChecksumMatches(t *testing.T) {
	// A previous run that died between the document insert and the chunk
	// inserts. Skipping it leaves a document the §16 count reports as
	// present with nothing retrievable in it — and the checksum alone says
	// everything is fine.
	corpus := chunkedCorpus(t)
	store := newFakeStore()

	if _, err := Ingest(context.Background(), corpus,
		&fakeEmbedder{model: "m"}, store, options()); err != nil {
		t.Fatal(err)
	}

	// Chunks gone, checksum intact: exactly the half-written state.
	for _, id := range store.documents {
		delete(store.chunks, id)
	}

	embedder := &fakeEmbedder{model: "m"}
	report, err := Ingest(context.Background(), corpus, embedder, store, options())
	if err != nil {
		t.Fatal(err)
	}

	if report.Skipped != 0 {
		t.Errorf("%d documents were skipped despite holding no chunks", report.Skipped)
	}
	if len(embedder.batches) == 0 {
		t.Error("no embed requests were sent for a document with no chunks")
	}
}

func TestEmbeddingIsBatchedAtTheConfiguredSize(t *testing.T) {
	// One request per chunk is both slow and the fastest way to get
	// rate-limited by a hosted provider.
	doc := synthetic(t, "Many Chunks", strings.Repeat("A sentence here. ", 120))
	corpus := chunk(t, doc, ChunkConfig{SizeTokens: 40, OverlapTokens: 5})

	total := len(corpus[0].Chunks)
	if total < 10 {
		t.Fatalf("need many chunks for this test, got %d", total)
	}

	embedder := &fakeEmbedder{model: "m"}
	opts := options()
	opts.BatchSize = 4

	if _, err := Ingest(context.Background(), corpus, embedder, newFakeStore(), opts); err != nil {
		t.Fatal(err)
	}

	sum := 0
	for index, size := range embedder.batches {
		sum += size
		if size > 4 {
			t.Errorf("batch %d held %d texts, over the configured 4", index, size)
		}
		// Every batch but the last must be full, or the batching is not
		// actually batching.
		if index < len(embedder.batches)-1 && size != 4 {
			t.Errorf("batch %d held %d texts, want a full 4", index, size)
		}
	}
	if sum != total {
		t.Errorf("embedded %d texts for %d chunks", sum, total)
	}
}

func TestAModelChangeMidRunIsFatal(t *testing.T) {
	// Vectors from different models are not comparable — cosine similarity
	// between them is a number with no meaning — so a corpus holding both
	// ranks arbitrarily and nothing about the result looks wrong.
	doc := synthetic(t, "Long Enough", strings.Repeat("A sentence here. ", 60))
	corpus := chunk(t, doc, ChunkConfig{SizeTokens: 40, OverlapTokens: 5})

	embedder := &fakeEmbedder{
		model:      "nomic-embed-text",
		modelAfter: 2,
		nextModel:  "text-embedding-004",
	}
	opts := options()
	opts.BatchSize = 2

	_, err := Ingest(context.Background(), corpus, embedder, newFakeStore(), opts)
	if !errors.Is(err, ErrEmbeddingModelChanged) {
		t.Fatalf("error is %v, want ErrEmbeddingModelChanged", err)
	}
}

func TestAModelChangeBetweenDocumentsIsAlsoFatal(t *testing.T) {
	corpus := chunkedCorpus(t)
	if len(corpus) < 2 {
		t.Fatal("need at least two documents")
	}

	// Switch after the first document's chunks.
	embedder := &fakeEmbedder{
		model:      "nomic-embed-text",
		modelAfter: len(corpus[0].Chunks),
		nextModel:  "text-embedding-004",
	}

	_, err := Ingest(context.Background(), corpus, embedder, newFakeStore(), options())
	if !errors.Is(err, ErrEmbeddingModelChanged) {
		t.Fatalf("error is %v, want ErrEmbeddingModelChanged", err)
	}
}

func TestAFailedChunkInsertRollsBackTheWholeDocument(t *testing.T) {
	// §4 requires documents and chunks to go in together. A document
	// visible without its chunks is a document retrieval cannot reach but
	// the §16 count includes — and `kc_unembedded_idx` would not find it,
	// because the chunks are not there at all.
	doc := synthetic(t, "Will Fail", strings.Repeat("A sentence here. ", 40))
	corpus := chunk(t, doc, ChunkConfig{SizeTokens: 40, OverlapTokens: 5})
	if len(corpus[0].Chunks) < 3 {
		t.Fatalf("need several chunks, got %d", len(corpus[0].Chunks))
	}

	store := newFakeStore()
	store.failOnChunk = 2

	_, err := Ingest(context.Background(), corpus, &fakeEmbedder{model: "m"}, store, options())
	if err == nil {
		t.Fatal("a failing chunk insert did not fail the run")
	}

	if store.committed != 0 {
		t.Errorf("%d transactions committed", store.committed)
	}
	if len(store.documents) != 0 {
		t.Errorf("the document survived the rollback: %v", store.documents)
	}
	for id, chunks := range store.chunks {
		if len(chunks) > 0 {
			t.Errorf("%s kept %d chunks after rollback", id, len(chunks))
		}
	}
}

func TestEarlierDocumentsSurviveALaterFailure(t *testing.T) {
	// The other side of per-document transactions, and the reason for
	// them: a run that dies at document 412 keeps the 411 that were
	// correct, and re-running skips them.
	corpus := chunkedCorpus(t)
	if len(corpus) < 2 {
		t.Fatal("need at least two documents")
	}

	embedder := &fakeEmbedder{
		model:     "m",
		failAfter: len(corpus[0].Chunks),
		err:       errors.New("provider died"),
	}

	store := newFakeStore()
	report, err := Ingest(context.Background(), corpus, embedder, store, options())
	if err == nil {
		t.Fatal("expected the run to fail")
	}

	if report.Ingested != 1 {
		t.Errorf("report says %d ingested, want 1", report.Ingested)
	}
	if store.committed != 1 {
		t.Errorf("%d transactions committed, want 1", store.committed)
	}
	// The error must name the file that failed, not just the operation.
	if !strings.Contains(err.Error(), corpus[1].Document.Path) {
		t.Errorf("error does not name the failing document: %v", err)
	}
}

func TestAShortBatchFromTheEmbedderIsRefused(t *testing.T) {
	// The silent-misalignment failure, from the pipeline's side. Vectors
	// are zipped against chunks BY POSITION, so one missing vector
	// attaches every embedding after it to the wrong passage — and the
	// corpus then retrieves confidently and wrongly with nothing erroring.
	//
	// Written because a mutation survived: removing the alignment check in
	// embedBatched left every test green, since the fake embedder always
	// returned exactly as many vectors as it was asked for.
	doc := synthetic(t, "Short Batch", strings.Repeat("A sentence here. ", 40))
	corpus := chunk(t, doc, ChunkConfig{SizeTokens: 40, OverlapTokens: 5})

	store := newFakeStore()
	_, err := Ingest(context.Background(), corpus,
		&fakeEmbedder{model: "m", shortBy: 1}, store, options())
	if err == nil {
		t.Fatal("a short batch was accepted")
	}
	if !strings.Contains(err.Error(), "vectors") {
		t.Errorf("error does not name the mismatch: %v", err)
	}

	// Nothing may have been written. A partial document is worse than none.
	if len(store.documents) != 0 {
		t.Errorf("a document was stored despite the misalignment: %v", store.documents)
	}
}

func TestADegenerateBatchSizeIsRefused(t *testing.T) {
	for _, size := range []int{0, -1} {
		_, err := Ingest(context.Background(), chunkedCorpus(t),
			&fakeEmbedder{model: "m"}, newFakeStore(),
			IngestOptions{BatchSize: size})
		if err == nil {
			t.Errorf("batch size %d was accepted; the batching loop would not advance", size)
		}
	}
}

// ── FormatVector ──

func TestFormatVectorProducesPgvectorTextForm(t *testing.T) {
	got := FormatVector([]float32{0.5, -0.25, 0})
	if got != "[0.5,-0.25,0]" {
		t.Errorf("got %q", got)
	}

	// pgvector rejects a bare `[]`, so an empty vector must be visibly
	// wrong rather than quietly accepted. Nothing in the pipeline produces
	// one — the route checks the width — but asserting the shape means a
	// future caller gets a Postgres error rather than a null embedding.
	if FormatVector(nil) != "[]" {
		t.Errorf("empty vector is %q", FormatVector(nil))
	}
}

func TestFormatVectorRoundTripsEveryFloat32(t *testing.T) {
	// 'g' with -1 precision is the shortest decimal that round-trips.
	// Getting this wrong loses low bits on every value in the corpus, and
	// the symptom is retrieval that is very slightly worse than it should
	// be — which is unobservable without a test like this one.
	values := []float32{
		0, 1, -1, 0.1, 1.0 / 3.0, math.MaxFloat32, math.SmallestNonzeroFloat32,
		0.012345678, -0.98765432, 1e-20, 1e20,
	}

	rendered := FormatVector(values)
	parts := strings.Split(strings.Trim(rendered, "[]"), ",")
	if len(parts) != len(values) {
		t.Fatalf("rendered %d parts for %d values: %s", len(parts), len(values), rendered)
	}

	for index, part := range parts {
		parsed, err := strconv.ParseFloat(part, 32)
		if err != nil {
			t.Fatalf("value %d rendered as %q, which does not parse: %v", index, part, err)
		}
		if float32(parsed) != values[index] {
			t.Errorf("value %d: %v rendered as %q which parses back to %v",
				index, values[index], part, float32(parsed))
		}
	}
}

func TestFormatVectorHandlesARealWidthVector(t *testing.T) {
	// 768 is what `nomic-embed-text` returns and what `vector(768)`
	// accepts. Mostly a smoke test on the builder's sizing.
	vector := make([]float32, 768)
	for index := range vector {
		vector[index] = float32(index) / 768
	}

	rendered := FormatVector(vector)
	if commas := strings.Count(rendered, ","); commas != 767 {
		t.Errorf("768 values rendered with %d commas", commas)
	}
	if !strings.HasPrefix(rendered, "[") || !strings.HasSuffix(rendered, "]") {
		t.Error("not bracketed")
	}
}

// ── Helpers ──

func options() IngestOptions {
	return IngestOptions{BatchSize: 64}
}

func chunkedCorpus(t *testing.T) []ChunkedDocument {
	t.Helper()

	var corpus []ChunkedDocument
	for _, path := range corpusFiles(t) {
		doc := mustParse(t, path)
		corpus = append(corpus, ChunkedDocument{
			Document: doc,
			Chunks:   mustChunk(t, doc, DefaultChunkConfig()),
		})
	}
	return corpus
}

func chunk(t *testing.T, doc *Document, cfg ChunkConfig) []ChunkedDocument {
	t.Helper()
	return []ChunkedDocument{{Document: doc, Chunks: mustChunk(t, doc, cfg)}}
}
