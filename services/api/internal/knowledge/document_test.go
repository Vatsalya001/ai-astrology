package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDocumentReadsTheHeaderAndBody(t *testing.T) {
	doc := mustParse(t, filepath.Join("testdata", "corpus", "saturn-tenth-house.md"))

	if doc.Title != "Saturn in the Tenth House" {
		t.Errorf("title is %q", doc.Title)
	}
	if doc.Category != "houses" {
		t.Errorf("category is %q", doc.Category)
	}
	if doc.Authority != 70 {
		t.Errorf("authority is %d", doc.Authority)
	}
	if !strings.HasPrefix(doc.Body, "Saturn occupying") {
		t.Errorf("body starts %q", truncate(doc.Body))
	}
	if strings.Contains(doc.Body, frontMatterFence) {
		t.Error("the closing fence leaked into the body")
	}
}

func TestDefaultsMatchTheSchemaDefaults(t *testing.T) {
	// Migration 000007: language 'en', astrology_system 'vedic', authority
	// 50, version 1. An authored file should only have to name what
	// differs — and the two sets of defaults must not drift, because a
	// document written without `language` would otherwise be filed under
	// one value and queried under another.
	doc, err := ParseDocument("minimal.md", []byte(
		"---\n{\"title\":\"T\",\"category\":\"c\",\"source\":\"editorial\"}\n---\nBody.\n"))
	if err != nil {
		t.Fatal(err)
	}

	if doc.Language != "en" {
		t.Errorf("language default is %q, schema says en", doc.Language)
	}
	if doc.AstrologySystem != "vedic" {
		t.Errorf("astrology_system default is %q, schema says vedic", doc.AstrologySystem)
	}
	if doc.Authority != 50 {
		t.Errorf("authority default is %d, schema says 50", doc.Authority)
	}
	if doc.Version != 1 {
		t.Errorf("version default is %d, schema says 1", doc.Version)
	}
	if string(doc.Metadata) != "{}" {
		t.Errorf("metadata default is %s, schema says {}", doc.Metadata)
	}
}

func TestParseDocumentRefusesMalformedFiles(t *testing.T) {
	cases := map[string]string{
		"no opening fence": "{\"title\":\"T\"}\n---\nBody.\n",

		"unclosed front matter": "---\n{\"title\":\"T\",\"category\":\"c\",\"source\":\"s\"}\nBody.\n",

		"invalid json": "---\n{\"title\": \"T\",}\n---\nBody.\n",

		// The one that matters most in a 600-document corpus: a typo'd key
		// is a filter field that silently does not exist, so the document
		// never matches the query it was written for.
		"unknown field": "---\n{\"title\":\"T\",\"category\":\"c\",\"source\":\"s\",\"planet\":\"saturn\"}\n---\nBody.\n",

		"blank title":    "---\n{\"title\":\"  \",\"category\":\"c\",\"source\":\"s\"}\n---\nBody.\n",
		"missing source": "---\n{\"title\":\"T\",\"category\":\"c\"}\n---\nBody.\n",
		"blank body":     "---\n{\"title\":\"T\",\"category\":\"c\",\"source\":\"s\"}\n---\n\n   \n",

		"authority above 100": "---\n{\"title\":\"T\",\"category\":\"c\",\"source\":\"s\",\"authority\":101}\n---\nB.\n",
		"authority negative":  "---\n{\"title\":\"T\",\"category\":\"c\",\"source\":\"s\",\"authority\":-1}\n---\nB.\n",

		// kd_language_lower. The retriever filters on an exact match, so
		// "EN" is a document no English query reaches.
		"uppercase language": "---\n{\"title\":\"T\",\"category\":\"c\",\"source\":\"s\",\"language\":\"EN\"}\n---\nB.\n",

		"version zero": "---\n{\"title\":\"T\",\"category\":\"c\",\"source\":\"s\",\"version\":0}\n---\nB.\n",

		// A bare array in `metadata` makes every `@>` containment query
		// against it fail to match, without erroring anywhere.
		"metadata is an array": "---\n{\"title\":\"T\",\"category\":\"c\",\"source\":\"s\",\"metadata\":[1,2]}\n---\nB.\n",
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseDocument(name+".md", []byte(content)); err == nil {
				t.Error("accepted")
			}
		})
	}
}

func TestErrorsNameTheFile(t *testing.T) {
	// A constraint violation surfacing from inside a 600-document
	// transaction says only that something, somewhere, was blank. Every
	// error from this package carries the path.
	_, err := ParseDocument("nakshatras/rohini.md", []byte("not a document"))
	if err == nil {
		t.Fatal("accepted")
	}
	if !strings.Contains(err.Error(), "nakshatras/rohini.md") {
		t.Errorf("error does not name the file: %v", err)
	}
}

// ── Line endings and invisible bytes ──
//
// Both of these produce a file that looks identical in every editor and
// chunks differently, which is the exact failure the byte-identical
// guarantee is supposed to exclude.

func TestCRLFProducesIdenticalChunksToLF(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "corpus", "saturn-tenth-house.md"))
	if err != nil {
		t.Fatal(err)
	}

	unix, err := ParseDocument("a.md", raw)
	if err != nil {
		t.Fatal(err)
	}
	windows, err := ParseDocument("a.md", []byte(strings.ReplaceAll(string(raw), "\n", "\r\n")))
	if err != nil {
		t.Fatalf("a CRLF file was rejected outright: %v", err)
	}

	if diff := describeDiff(
		mustChunk(t, unix, DefaultChunkConfig()),
		mustChunk(t, windows, DefaultChunkConfig()),
	); diff != "" {
		t.Errorf("CRLF chunks differently: %s", diff)
	}
}

func TestALeadingBOMIsTolerated(t *testing.T) {
	// What an editor on Windows leaves behind. Three bytes nobody can see
	// in a diff, and without the trim the first line is not "---" and the
	// whole document is rejected for a reason the author cannot observe.
	content := "\ufeff---\n{\"title\":\"T\",\"category\":\"c\",\"source\":\"s\"}\n---\nBody.\n"
	if _, err := ParseDocument("bom.md", []byte(content)); err != nil {
		t.Errorf("a BOM-prefixed file was rejected: %v", err)
	}
}

func TestChecksumCoversTheWholeFile(t *testing.T) {
	// The corpus is the only input to ingestion that git protects, and the
	// embeddings computed from it are the one output nothing protects.
	// Task 5.4 compares this against what is stored.
	base := "---\n{\"title\":\"T\",\"category\":\"c\",\"source\":\"s\"}\n---\nBody.\n"

	first, err := ParseDocument("a.md", []byte(base))
	if err != nil {
		t.Fatal(err)
	}

	same, err := ParseDocument("different/path.md", []byte(base))
	if err != nil {
		t.Fatal(err)
	}
	if first.Checksum != same.Checksum {
		t.Error("checksum depends on the path; it must depend only on the bytes")
	}

	// A header-only change must move the checksum: `authority` and
	// `metadata` are what the retriever ranks and filters on, so a corpus
	// that re-ingests only when the body changed would silently keep
	// stale filters.
	edited, err := ParseDocument("a.md", []byte(strings.Replace(
		base, "\"source\":\"s\"", "\"source\":\"s\",\"authority\":99", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if edited.Checksum == first.Checksum {
		t.Error("a front-matter change did not move the checksum")
	}
}
