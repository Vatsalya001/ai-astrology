package knowledge

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden chunk files")

// ── The acceptance criterion ──
//
// PHASE-05 §10 task 5.2: "Same input → same chunks, byte-identical."
//
// Two tests, because the phrase covers two different promises and only one
// of them is about this process:
//
//	TestChunkingIsByteIdenticalAcrossRuns — the same binary, repeated.
//	                                        Catches map iteration, clocks,
//	                                        pointer-order dependence.
//	TestChunkingMatchesGolden             — the same INPUT across code
//	                                        changes. Catches a refactor that
//	                                        quietly moves a boundary.
//
// The second is the one that matters in six months. A chunk boundary that
// moves invalidates the embedding on both sides of it, and since nothing
// errors, the symptom is retrieval that is subtly worse with no commit to
// blame. Making the boundaries a reviewable artefact is the only way that
// change is ever noticed.

func TestChunkingIsByteIdenticalAcrossRuns(t *testing.T) {
	for _, path := range corpusFiles(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			doc := mustParse(t, path)

			first := mustChunk(t, doc, DefaultChunkConfig())

			// Twenty, not two. Map iteration order in Go is randomised per
			// range statement, so a single repeat has a real chance of
			// agreeing by luck.
			for run := 0; run < 20; run++ {
				again := mustChunk(t, mustParse(t, path), DefaultChunkConfig())
				if diff := describeDiff(first, again); diff != "" {
					t.Fatalf("run %d differs: %s", run, diff)
				}
			}
		})
	}
}

func TestChunkingMatchesGolden(t *testing.T) {
	for _, path := range corpusFiles(t) {
		name := strings.TrimSuffix(filepath.Base(path), ".md")

		t.Run(name, func(t *testing.T) {
			doc := mustParse(t, path)
			chunks := mustChunk(t, doc, DefaultChunkConfig())

			encoded, err := json.MarshalIndent(goldenView(doc, chunks), "", "  ")
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			encoded = append(encoded, '\n')

			goldenPath := filepath.Join("testdata", "golden", name+".json")

			if *update {
				if err := os.WriteFile(goldenPath, encoded, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				t.Logf("rewrote %s", goldenPath)
				return
			}

			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden (run `go test ./internal/knowledge -update`): %v", err)
			}

			if string(encoded) != string(want) {
				t.Errorf("chunking changed for %s.\n"+
					"If this is intended, re-run with -update AND understand that every "+
					"affected chunk has to be re-embedded: the stored vectors describe the "+
					"old boundaries.\n\ngot:\n%s", name, encoded)
			}
		})
	}
}

// ── The window contract ──

func TestNoChunkExceedsTheConfiguredWindow(t *testing.T) {
	// Tried at several sizes because the interesting failures are at the
	// edges: a window barely larger than one sentence, and one large
	// enough to swallow a whole section.
	for _, size := range []int{40, 80, 350, 2000} {
		cfg := ChunkConfig{SizeTokens: size, OverlapTokens: size / 8}

		for _, path := range corpusFiles(t) {
			doc := mustParse(t, path)
			chunks := mustChunk(t, doc, cfg)

			for _, chunk := range chunks {
				// The title prefix is charged against the budget, so the
				// whole chunk — prefix included — has to fit.
				if chunk.TokenCount > size {
					t.Errorf("%s chunk %d is %d tokens, window is %d:\n%s",
						filepath.Base(path), chunk.Index, chunk.TokenCount, size,
						truncate(chunk.Content))
				}
			}
		}
	}
}

// A single sentence longer than the entire window is the input that breaks
// the obvious implementation: it cannot fit, so a windower that refuses to
// emit an oversize chunk emits nothing and never advances.
func TestAnOversizedSentenceIsSplitRatherThanDroppedOrOverflowing(t *testing.T) {
	long := strings.Repeat("obstruction ", 400)
	doc := synthetic(t, "Very Long Sentence", long+".")

	cfg := ChunkConfig{SizeTokens: 60, OverlapTokens: 10}
	chunks := mustChunk(t, doc, cfg)

	if len(chunks) < 2 {
		t.Fatalf("expected the sentence to be split, got %d chunk(s)", len(chunks))
	}

	for _, chunk := range chunks {
		if chunk.TokenCount > cfg.SizeTokens {
			t.Errorf("chunk %d is %d tokens, over the %d window",
				chunk.Index, chunk.TokenCount, cfg.SizeTokens)
		}
	}

	// Nothing may be lost. The failure this guards against is a hard split
	// that drops the remainder — which would leave the database holding a
	// document whose content it does not actually contain.
	var recovered strings.Builder
	for _, chunk := range chunks {
		recovered.WriteString(body(chunk.Content))
		recovered.WriteString(" ")
	}
	if got := strings.Count(recovered.String(), "obstruction"); got < 400 {
		t.Errorf("recovered %d of 400 words — the split lost content", got)
	}
}

// TestWindowingTerminatesWhenAShortUnitPrecedesALongOne pins the one line
// that keeps the windower from looping forever.
//
// Written because a mutation survived. Widening the step-back floor from
// `next > start+1` to `next > start` left every other test green, so the
// floor was unproven — and it is the whole termination argument. The input
// that reaches it is specific: a window whose entire content fits inside
// the overlap budget, which happens when a short unit is followed by one
// too large to join it.
//
//	budget 98, overlap 50
//	units  [3 tokens] [98 tokens] [65 tokens]
//	window one holds only the 3-token unit, because the 98-token unit
//	cannot join it. The step-back can therefore carry the whole window —
//	and with no floor, the next window starts where this one did.
//
// The capital letter on "A long clause" is load-bearing. With a lowercase
// continuation the two sentences merge into one unit, the window comes out
// at 98 tokens, the step-back has nothing it can carry, and the test
// passes against the broken code — which is exactly what the first version
// of this test did.
func TestWindowingTerminatesWhenAShortUnitPrecedesALongOne(t *testing.T) {
	doc := synthetic(t, "T",
		"Short. A"+strings.Repeat(" long clause that keeps going", 18)+".")

	// Run under a watchdog rather than relying on `go test -timeout`: a
	// hang there takes the whole package down after ten minutes and dumps
	// every goroutine, which is a far worse signal than one failing test.
	done := make(chan []Chunk, 1)
	go func() {
		chunks, err := ChunkDocument(doc, ChunkConfig{SizeTokens: 100, OverlapTokens: 50})
		if err != nil {
			close(done)
			return
		}
		done <- chunks
	}()

	select {
	case chunks, ok := <-done:
		if !ok {
			t.Fatal("ChunkDocument returned an error on valid input")
		}
		if len(chunks) < 2 {
			t.Fatalf("expected the long clause to need its own chunk, got %d; "+
				"this input no longer reaches the step-back floor and the test "+
				"has stopped testing anything", len(chunks))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ChunkDocument did not terminate: the window step-back is not " +
			"making forward progress")
	}
}

func TestOverlapRepeatsTheTailOfThePreviousChunk(t *testing.T) {
	doc := mustParse(t, filepath.Join("testdata", "corpus", "saturn-tenth-house.md"))
	cfg := ChunkConfig{SizeTokens: 90, OverlapTokens: 30}

	chunks := mustChunk(t, doc, cfg)
	if len(chunks) < 3 {
		t.Fatalf("need several chunks to test overlap, got %d", len(chunks))
	}

	overlaps := 0
	for index := 1; index < len(chunks); index++ {
		previous, current := chunks[index-1], chunks[index]
		if previous.Section != current.Section {
			// Sections are hard boundaries — asserted separately below.
			continue
		}

		tail := lastSentence(body(previous.Content))
		if tail != "" && strings.Contains(body(current.Content), tail) {
			overlaps++
		}
	}

	if overlaps == 0 {
		t.Error("no chunk repeated any of its predecessor's tail; overlap is not working")
	}
}

// The mutation that proves the test above: with overlap set to zero no
// chunk may repeat anything. Without this, an "overlap works" assertion
// would also pass against an implementation that duplicated whole chunks.
func TestZeroOverlapRepeatsNothing(t *testing.T) {
	doc := mustParse(t, filepath.Join("testdata", "corpus", "saturn-tenth-house.md"))
	chunks := mustChunk(t, doc, ChunkConfig{SizeTokens: 90, OverlapTokens: 0})

	for index := 1; index < len(chunks); index++ {
		tail := lastSentence(body(chunks[index-1].Content))
		if tail != "" && strings.Contains(body(chunks[index].Content), tail) {
			t.Errorf("chunk %d repeats chunk %d's tail with overlap disabled:\n%q",
				index, index-1, tail)
		}
	}
}

func TestSectionsAreHardBoundaries(t *testing.T) {
	doc := synthetic(t, "Two Sections",
		"## Career\n\nSaturn rewards endurance here.\n\n"+
			"## Marriage\n\nThe seventh lord is what matters.")

	chunks := mustChunk(t, doc, DefaultChunkConfig())

	// The window is 350 tokens and the whole document is far smaller, so a
	// chunker without section boundaries would emit exactly one chunk
	// containing both. Two chunks is the assertion.
	if len(chunks) != 2 {
		t.Fatalf("expected one chunk per section, got %d", len(chunks))
	}

	for _, chunk := range chunks {
		career := strings.Contains(chunk.Content, "endurance")
		marriage := strings.Contains(chunk.Content, "seventh lord")
		if career && marriage {
			t.Errorf("chunk %d spans two sections:\n%s", chunk.Index, chunk.Content)
		}
	}
}

func TestEveryChunkCarriesTheDocumentTitle(t *testing.T) {
	for _, path := range corpusFiles(t) {
		doc := mustParse(t, path)
		for _, chunk := range mustChunk(t, doc, ChunkConfig{SizeTokens: 60, OverlapTokens: 10}) {
			if !strings.Contains(chunk.Content, doc.Title) {
				t.Errorf("%s chunk %d lacks the title %q:\n%s",
					filepath.Base(path), chunk.Index, doc.Title, truncate(chunk.Content))
			}
		}
	}
}

func TestChunkIndexesAreContiguousFromZero(t *testing.T) {
	// `kc_position_unique UNIQUE (document_id, chunk_index)` tolerates
	// gaps; retrieval's "show me the chunk before this one" does not, and
	// neither does a reader trying to reconstruct a document from its
	// chunks.
	for _, path := range corpusFiles(t) {
		chunks := mustChunk(t, mustParse(t, path), DefaultChunkConfig())
		for index, chunk := range chunks {
			if chunk.Index != index {
				t.Errorf("%s: chunk at position %d reports index %d",
					filepath.Base(path), index, chunk.Index)
			}
		}
	}
}

func TestChunkTokenCountIsPositive(t *testing.T) {
	// `kc_token_count_positive CHECK (token_count > 0)`. A zero here is an
	// insert that fails mid-transaction after the embeddings have been
	// paid for.
	for _, path := range corpusFiles(t) {
		for _, chunk := range mustChunk(t, mustParse(t, path), DefaultChunkConfig()) {
			if chunk.TokenCount <= 0 {
				t.Errorf("%s chunk %d has token_count %d",
					filepath.Base(path), chunk.Index, chunk.TokenCount)
			}
			if strings.TrimSpace(chunk.Content) == "" {
				t.Errorf("%s chunk %d is blank — /v1/embed refuses these",
					filepath.Base(path), chunk.Index)
			}
		}
	}
}

// ── Configuration ──

func TestOverlapAtOrAboveSizeIsRejected(t *testing.T) {
	doc := synthetic(t, "Anything", "One sentence. Then another one.")

	for _, cfg := range []ChunkConfig{
		{SizeTokens: 100, OverlapTokens: 100},
		{SizeTokens: 100, OverlapTokens: 150},
	} {
		if _, err := ChunkDocument(doc, cfg); err == nil {
			t.Errorf("overlap %d with size %d was accepted; that configuration "+
				"restarts every window at the previous window's start",
				cfg.OverlapTokens, cfg.SizeTokens)
		}
	}
}

func TestDegenerateConfigsAreRejected(t *testing.T) {
	doc := synthetic(t, "Anything", "One sentence.")

	for name, cfg := range map[string]ChunkConfig{
		"zero size":        {SizeTokens: 0, OverlapTokens: 0},
		"negative size":    {SizeTokens: -1, OverlapTokens: 0},
		"negative overlap": {SizeTokens: 100, OverlapTokens: -1},
	} {
		if _, err := ChunkDocument(doc, cfg); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestDefaultConfigMatchesTheSpec(t *testing.T) {
	// PHASE-05 §9: KB_CHUNK_SIZE_TOKENS=350, KB_CHUNK_OVERLAP_TOKENS=50.
	cfg := DefaultChunkConfig()
	if cfg.SizeTokens != 350 || cfg.OverlapTokens != 50 {
		t.Errorf("default config is %+v, spec says 350/50", cfg)
	}
}

// ── Metadata ──

func TestChunkMetadataCarriesTheDocumentFilterSurface(t *testing.T) {
	doc := mustParse(t, filepath.Join("testdata", "corpus", "saturn-tenth-house.md"))
	chunks := mustChunk(t, doc, DefaultChunkConfig())

	for _, chunk := range chunks {
		var fields map[string]any
		if err := json.Unmarshal(chunk.Metadata, &fields); err != nil {
			t.Fatalf("chunk %d metadata is not an object: %v", chunk.Index, err)
		}

		// Retrieval filters chunks with `metadata @> '{"planet":"saturn"}'`
		// against the GIN index on knowledge_chunks. If the document's keys
		// are not copied down, that query matches nothing and the symptom
		// is a retriever that returns generic material for a specific
		// question.
		if fields["planet"] != "saturn" {
			t.Errorf("chunk %d lost planet: %v", chunk.Index, fields)
		}
		if fields["house"] != float64(10) {
			t.Errorf("chunk %d lost house: %v", chunk.Index, fields)
		}
	}
}

func TestSectionIsRecordedUnderANamespacedKey(t *testing.T) {
	doc := synthetic(t, "Namespaced", "## Career\n\nSaturn rewards endurance.")
	doc.Metadata = json.RawMessage(`{"section":"authored, not derived"}`)

	chunks := mustChunk(t, doc, DefaultChunkConfig())

	var fields map[string]any
	if err := json.Unmarshal(chunks[0].Metadata, &fields); err != nil {
		t.Fatal(err)
	}

	if fields["section"] != "authored, not derived" {
		t.Errorf("the chunker overwrote an authored `section` key: %v", fields)
	}
	if fields["_section"] != "Career" {
		t.Errorf("_section is %v, want Career", fields["_section"])
	}
}

// ── Structure preservation ──

func TestATableIsNotSplitAcrossChunks(t *testing.T) {
	doc := mustParse(t, filepath.Join("testdata", "corpus", "edge-cases.md"))
	chunks := mustChunk(t, doc, DefaultChunkConfig())

	table := findChunk(t, chunks, "| Saturn | 10 |")
	for _, row := range []string{"| Planet | House | Reading |", "| Jupiter | 9 |", "| Mars | 3 |"} {
		if !strings.Contains(table.Content, row) {
			t.Errorf("the table lost %q — a chunk with data rows and no header "+
				"has columns nothing identifies:\n%s", row, table.Content)
		}
	}
}

func TestACodeFenceIsNotSplitAndItsHashesAreNotHeadings(t *testing.T) {
	doc := mustParse(t, filepath.Join("testdata", "corpus", "edge-cases.md"))
	chunks := mustChunk(t, doc, DefaultChunkConfig())

	fenced := findChunk(t, chunks, "weight = vector")

	// Both comments must survive in the same chunk. `# this hash is not a
	// heading` inside a fence would otherwise start a new section, cutting
	// the block in half and leaving an unclosed fence that swallows the
	// rest of the rendered message.
	for _, line := range []string{"# this hash is not a heading", "# neither is this one"} {
		if !strings.Contains(fenced.Content, line) {
			t.Errorf("a `#` inside a code fence was treated as a heading; %q is "+
				"missing from:\n%s", line, fenced.Content)
		}
	}

	if opens := strings.Count(fenced.Content, "```"); opens%2 != 0 {
		t.Errorf("chunk has %d fence delimiters — unbalanced:\n%s", opens, fenced.Content)
	}
}

func TestACodeFenceKeepsItsBlankLines(t *testing.T) {
	// Found by the golden file rather than by reasoning: the first
	// implementation made each line of a fence its own unit, and a blank
	// line is not a unit, so every blank line inside a code block
	// disappeared. In code a blank line is content.
	doc := mustParse(t, filepath.Join("testdata", "corpus", "edge-cases.md"))
	fenced := findChunk(t, mustChunk(t, doc, DefaultChunkConfig()), "weight = vector")

	if !strings.Contains(fenced.Content, "0.4\n\n# neither") {
		t.Errorf("the blank line inside the fence was dropped:\n%q", fenced.Content)
	}
}

func TestAWrappedListItemKeepsItsIndentation(t *testing.T) {
	// The indentation is what makes the line a continuation of the item
	// above rather than a new paragraph. Stripping it changes the markdown
	// the chat UI renders.
	doc := mustParse(t, filepath.Join("testdata", "corpus", "edge-cases.md"))
	chunk := findChunk(t, mustChunk(t, doc, DefaultChunkConfig()), "Research requiring decades")

	if !strings.Contains(chunk.Content, "decades\n  rather than quarters") {
		t.Errorf("a wrapped list item lost its indentation:\n%q", chunk.Content)
	}
}

func TestAnOversizedTableIsCutAtRowBoundaries(t *testing.T) {
	var table strings.Builder
	table.WriteString("| Planet | House | Reading |\n|---|---|---|\n")
	for row := 0; row < 60; row++ {
		fmt.Fprintf(&table, "| Planet %d | %d | A reading of moderate length |\n", row, row%12+1)
	}

	doc := synthetic(t, "Wide Table", table.String())
	chunks := mustChunk(t, doc, ChunkConfig{SizeTokens: 120, OverlapTokens: 20})

	if len(chunks) < 2 {
		t.Fatalf("the table fits in one chunk; raise the row count. got %d", len(chunks))
	}

	// Cutting a table mid-row leaves a half-written row in both chunks.
	// Cutting between rows at least leaves both halves parseable.
	for _, chunk := range chunks {
		for _, line := range strings.Split(body(chunk.Content), "\n") {
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
				t.Errorf("chunk %d holds a partial row %q", chunk.Index, line)
			}
		}
	}
}

func TestAListKeepsOneItemPerLine(t *testing.T) {
	doc := mustParse(t, filepath.Join("testdata", "corpus", "saturn-tenth-house.md"))
	chunks := mustChunk(t, doc, DefaultChunkConfig())

	list := findChunk(t, chunks, "- Administration, law")
	if strings.Contains(list.Content, "judiciary - Engineering") {
		t.Errorf("list items were rejoined with a space, producing a run-on line:\n%s",
			list.Content)
	}
}

func TestAHeadingWithNoProseProducesNoChunk(t *testing.T) {
	doc := mustParse(t, filepath.Join("testdata", "corpus", "edge-cases.md"))
	for _, chunk := range mustChunk(t, doc, DefaultChunkConfig()) {
		if chunk.Section == "A heading with no prose under it" {
			t.Errorf("empty section produced a chunk: %q", chunk.Content)
		}
	}
}

func TestADocumentOfOnlyHeadingsIsAnError(t *testing.T) {
	doc := synthetic(t, "Headings Only", "## One\n\n## Two\n\n## Three")

	if _, err := ChunkDocument(doc, DefaultChunkConfig()); err == nil {
		t.Error("a document with no prose ingested silently; it would land in the " +
			"corpus as a document with nothing retrievable in it")
	}
}

// ── Fuzzing: the property that has to hold for input nobody has written ──

// FuzzChunkDocumentTerminates is here for one failure mode that unit tests
// structurally cannot cover: the windower is a loop whose step size depends
// on the content, and the corpus is 400–600 documents that do not exist
// yet. A seed corpus proves the cases I thought of; the fuzzer is for the
// ones I did not.
func FuzzChunkDocumentTerminates(f *testing.F) {
	f.Add("Plain prose. Two sentences.")
	f.Add("## Heading\n\n- a\n- b\n\n| x | y |\n|---|---|\n| 1 | 2 |")
	f.Add("```\n# fence\n```\n\nAfter.")
	f.Add(strings.Repeat("word ", 500))
	f.Add("one.two.three.four")
	f.Add("शनि दशम भाव में। यह कर्म का भाव है॥")
	f.Add("...!?!?...")
	f.Add("#\n##\n### \n")

	f.Fuzz(func(t *testing.T, body string) {
		doc := &Document{
			Title: "Fuzz", Category: "x", Source: "x", Language: "en",
			Version: 1, Body: body, Metadata: json.RawMessage("{}"), Path: "fuzz.md",
		}

		// A tiny window maximises the number of loop iterations per byte of
		// input, which is where a non-terminating step would show up.
		chunks, err := ChunkDocument(doc, ChunkConfig{SizeTokens: 12, OverlapTokens: 4})
		if err != nil {
			// Refusing input is allowed. Hanging or panicking is not.
			return
		}

		for index, chunk := range chunks {
			if chunk.Index != index {
				t.Fatalf("index %d at position %d", chunk.Index, index)
			}
			if strings.TrimSpace(chunk.Content) == "" {
				t.Fatalf("blank chunk at %d", index)
			}
			if chunk.TokenCount <= 0 {
				t.Fatalf("token_count %d at %d", chunk.TokenCount, index)
			}
			if !json.Valid(chunk.Metadata) {
				t.Fatalf("invalid metadata JSON at %d: %s", index, chunk.Metadata)
			}
		}
	})
}

// ── Helpers ──

type goldenChunk struct {
	Index      int             `json:"index"`
	Section    string          `json:"section"`
	TokenCount int             `json:"token_count"`
	Metadata   json.RawMessage `json:"metadata"`
	Content    string          `json:"content"`
}

type goldenDocument struct {
	Title    string        `json:"title"`
	Checksum string        `json:"checksum"`
	Chunks   []goldenChunk `json:"chunks"`
}

func goldenView(doc *Document, chunks []Chunk) goldenDocument {
	view := goldenDocument{Title: doc.Title, Checksum: doc.Checksum}
	for _, chunk := range chunks {
		view.Chunks = append(view.Chunks, goldenChunk{
			Index:      chunk.Index,
			Section:    chunk.Section,
			TokenCount: chunk.TokenCount,
			Metadata:   chunk.Metadata,
			Content:    chunk.Content,
		})
	}
	return view
}

func corpusFiles(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "corpus", "*.md"))
	if err != nil {
		t.Fatalf("glob corpus: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no corpus fixtures found")
	}
	return paths
}

func mustParse(t *testing.T, path string) *Document {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	doc, err := ParseDocument(filepath.Base(path), raw)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc
}

func mustChunk(t *testing.T, doc *Document, cfg ChunkConfig) []Chunk {
	t.Helper()
	chunks, err := ChunkDocument(doc, cfg)
	if err != nil {
		t.Fatalf("chunk %s: %v", doc.Path, err)
	}
	return chunks
}

func synthetic(t *testing.T, title, body string) *Document {
	t.Helper()
	return &Document{
		Title:           title,
		Category:        "test",
		Source:          "editorial",
		Language:        "en",
		AstrologySystem: "vedic",
		Authority:       50,
		Version:         1,
		Body:            body,
		Metadata:        json.RawMessage("{}"),
		Path:            "synthetic.md",
	}
}

func describeDiff(want, got []Chunk) string {
	if len(want) != len(got) {
		return fmt.Sprintf("chunk count %d vs %d", len(want), len(got))
	}
	for index := range want {
		switch {
		case want[index].Content != got[index].Content:
			return fmt.Sprintf("chunk %d content:\n%q\nvs\n%q",
				index, truncate(want[index].Content), truncate(got[index].Content))
		case want[index].TokenCount != got[index].TokenCount:
			return fmt.Sprintf("chunk %d token count %d vs %d",
				index, want[index].TokenCount, got[index].TokenCount)
		case string(want[index].Metadata) != string(got[index].Metadata):
			return fmt.Sprintf("chunk %d metadata %s vs %s",
				index, want[index].Metadata, got[index].Metadata)
		case want[index].Section != got[index].Section:
			return fmt.Sprintf("chunk %d section %q vs %q",
				index, want[index].Section, got[index].Section)
		}
	}
	return ""
}

// body strips the prepended title line, leaving the prose a chunk carries.
func body(content string) string {
	_, rest, found := strings.Cut(content, "\n\n")
	if !found {
		return content
	}
	return rest
}

func lastSentence(text string) string {
	sentences := splitSentences(strings.ReplaceAll(text, "\n", " "))
	if len(sentences) == 0 {
		return ""
	}
	return sentences[len(sentences)-1]
}

func findChunk(t *testing.T, chunks []Chunk, needle string) Chunk {
	t.Helper()
	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, needle) {
			return chunk
		}
	}
	t.Fatalf("no chunk contains %q", needle)
	return Chunk{}
}

func truncate(s string) string {
	const limit = 400
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}
