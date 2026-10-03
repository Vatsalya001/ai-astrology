package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/knowledge"
)

func TestLoadCorpusReturnsDocumentsInAFixedOrder(t *testing.T) {
	// Fixed order is not tidiness. It is what makes the corpus checksum
	// mean anything, and in task 5.4 it is what makes two ingestion runs
	// insert in the same order — the difference between a re-run that is a
	// no-op and two concurrent runs deadlocking against each other.
	dir := t.TempDir()
	for _, name := range []string{"zeta.md", "alpha.md", "mid.md"} {
		writeDoc(t, dir, name, name)
	}
	// A subdirectory, because the corpus is organised by category.
	if err := os.Mkdir(filepath.Join(dir, "houses"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, dir, filepath.Join("houses", "first.md"), "houses/first")

	corpus, err := loadCorpus(dir, knowledge.DefaultChunkConfig())
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, loaded := range corpus {
		got = append(got, loaded.doc.Path)
	}

	want := []string{"alpha.md", "houses/first.md", "mid.md", "zeta.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order is %v, want %v", got, want)
	}
}

func TestLoadCorpusPathsAreSlashSeparated(t *testing.T) {
	// The path is stored and compared, so it must not carry the host's
	// separator: a corpus ingested on Windows would otherwise look
	// entirely different from the same corpus ingested in CI.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "houses"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, dir, filepath.Join("houses", "tenth.md"), "tenth")

	corpus, err := loadCorpus(dir, knowledge.DefaultChunkConfig())
	if err != nil {
		t.Fatal(err)
	}

	if corpus[0].doc.Path != "houses/tenth.md" {
		t.Errorf("path is %q, want houses/tenth.md", corpus[0].doc.Path)
	}
}

func TestLoadCorpusReportsEveryBadDocumentNotJustTheFirst(t *testing.T) {
	// An author fixing a 600-document corpus one error per run is an author
	// who stops fixing it.
	dir := t.TempDir()
	writeRaw(t, dir, "one.md", "not a document at all")
	writeRaw(t, dir, "two.md", "---\n{\"title\":\"T\"}\n---\nBody.\n")
	writeRaw(t, dir, "three.md", "---\n{\"title\":\"T\",\"category\":\"c\",\"source\":\"s\"}\n---\n\n")
	writeDoc(t, dir, "good.md", "good")

	_, err := loadCorpus(dir, knowledge.DefaultChunkConfig())
	if err == nil {
		t.Fatal("accepted a corpus with three invalid documents")
	}

	for _, name := range []string{"one.md", "two.md", "three.md"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("%s is not named in the error:\n%v", name, err)
		}
	}
	if !strings.Contains(err.Error(), "3 of 4") {
		t.Errorf("error does not count the failures:\n%v", err)
	}
}

func TestLoadCorpusSkipsDocumentationAboutTheCorpus(t *testing.T) {
	// README.md is instructions for authors. Ingested, it lands in the
	// retrieval index and is eventually quoted back at a user as though it
	// were astrology.
	dir := t.TempDir()
	writeRaw(t, dir, "README.md", "# How to write documents\n\nNo front matter here.\n")
	writeRaw(t, dir, "_draft.md", "Unfinished, no front matter.\n")
	writeRaw(t, dir, "notes.txt", "Not markdown.\n")
	writeDoc(t, dir, "real.md", "real")

	corpus, err := loadCorpus(dir, knowledge.DefaultChunkConfig())
	if err != nil {
		t.Fatalf("the skipped files were parsed: %v", err)
	}
	if len(corpus) != 1 {
		t.Fatalf("got %d documents, want 1", len(corpus))
	}
}

func TestLoadCorpusRefusesAnEmptyDirectory(t *testing.T) {
	// Silence here would read as "ingested nothing successfully", and the
	// commonest cause is a --corpus pointing at the wrong place.
	if _, err := loadCorpus(t.TempDir(), knowledge.DefaultChunkConfig()); err == nil {
		t.Error("an empty corpus directory was accepted")
	}
}

func TestDuplicateIdentitiesAreRefusedBeforeTheDatabaseSeesThem(t *testing.T) {
	// `kd_identity_idx` is UNIQUE (title, language, version) and is what
	// makes re-ingestion an upsert. Two files sharing an identity is
	// therefore not a constraint violation to report from inside a
	// transaction — it is one authored document overwriting another, and
	// the loser vanishes from the corpus with no error anywhere.
	dir := t.TempDir()
	writeDoc(t, dir, "a.md", "Same Title")
	writeDoc(t, dir, "b.md", "Same Title")

	_, err := loadCorpus(dir, knowledge.DefaultChunkConfig())
	if err == nil {
		t.Fatal("two documents with the same identity were accepted")
	}
	for _, want := range []string{"Same Title", "a.md", "b.md", "kd_identity_idx"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error omits %q:\n%v", want, err)
		}
	}
}

func TestDifferentVersionsOfOneTitleAreAllowed(t *testing.T) {
	// The identity includes the version precisely so a document can be
	// revised without displacing the one in use.
	dir := t.TempDir()
	writeRaw(t, dir, "v1.md",
		"---\n{\"title\":\"T\",\"category\":\"c\",\"source\":\"s\",\"version\":1}\n---\nBody one.\n")
	writeRaw(t, dir, "v2.md",
		"---\n{\"title\":\"T\",\"category\":\"c\",\"source\":\"s\",\"version\":2}\n---\nBody two.\n")

	corpus, err := loadCorpus(dir, knowledge.DefaultChunkConfig())
	if err != nil {
		t.Fatalf("two versions of one title were refused: %v", err)
	}
	if len(corpus) != 2 {
		t.Errorf("got %d documents, want 2", len(corpus))
	}
}

func TestCorpusChecksumChangesWithContentAndNotWithWalkOrder(t *testing.T) {
	dir := t.TempDir()
	writeDoc(t, dir, "a.md", "A")
	writeDoc(t, dir, "b.md", "B")

	first, err := loadCorpus(dir, knowledge.DefaultChunkConfig())
	if err != nil {
		t.Fatal(err)
	}
	baseline := corpusChecksum(first)

	// Re-reading the same directory must give the same value. This is the
	// property task 5.4 relies on to answer "is what is in the database
	// still what we authored?"
	again, err := loadCorpus(dir, knowledge.DefaultChunkConfig())
	if err != nil {
		t.Fatal(err)
	}
	if corpusChecksum(again) != baseline {
		t.Error("checksum is not stable across runs")
	}

	// Editing one document must move it.
	writeRaw(t, dir, "a.md",
		"---\n{\"title\":\"A\",\"category\":\"c\",\"source\":\"s\"}\n---\nEdited body.\n")
	edited, err := loadCorpus(dir, knowledge.DefaultChunkConfig())
	if err != nil {
		t.Fatal(err)
	}
	if corpusChecksum(edited) == baseline {
		t.Error("editing a document did not move the corpus checksum")
	}
}

func TestEveryShippedCorpusDocumentIsValid(t *testing.T) {
	// The real corpus, through the real loader. This is the test that fails
	// when somebody commits a document with a trailing comma in its front
	// matter — which `tsc`, `go vet` and every linter in this repo are
	// entirely blind to.
	corpus, err := loadCorpus(defaultCorpusDir(), knowledge.DefaultChunkConfig())
	if err != nil {
		t.Fatalf("the shipped corpus does not load: %v", err)
	}
	if len(corpus) == 0 {
		t.Fatal("the shipped corpus is empty")
	}

	for _, loaded := range corpus {
		for _, chunk := range loaded.chunks {
			if chunk.TokenCount > knowledge.DefaultChunkConfig().SizeTokens {
				t.Errorf("%s chunk %d is %d tokens, over the window",
					loaded.doc.Path, chunk.Index, chunk.TokenCount)
			}
		}
	}

	t.Logf("%d documents, %d chunks", len(corpus), countChunks(corpus))
}

// ── Helpers ──

func countChunks(corpus []loadedDocument) int {
	total := 0
	for _, loaded := range corpus {
		total += len(loaded.chunks)
	}
	return total
}

func writeDoc(t *testing.T, dir, name, title string) {
	t.Helper()
	writeRaw(t, dir, name, fmt.Sprintf(
		"---\n{\"title\":%q,\"category\":\"c\",\"source\":\"s\"}\n---\nBody of %s.\n",
		title, title))
}

func writeRaw(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
