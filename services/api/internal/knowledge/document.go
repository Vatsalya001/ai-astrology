// Package knowledge parses and chunks the authored astrology corpus.
//
// It is the Go half of PHASE-05 §4's ingestion split — "Go orchestrates,
// Python embeds".
//
// Parsing and chunking are pure: no database, no HTTP, no clock, no
// filesystem. That is what makes `cmd/ingest-kb --check` reproducible and
// lets the whole chunking contract be proven by golden files with no model
// involved. `ingest.go` orchestrates the I/O, and it does so through
// interfaces declared in this package — so even the full pipeline is
// testable without a database or a model, which is what lets it run in CI
// under the rule that CI never calls one.
//
// ── Why the front matter is JSON and not YAML ──
//
// The obvious choice for authored markdown is YAML front matter, and this
// is deliberately not that. Three reasons, in order of how much they cost
// when ignored:
//
//  1. `metadata` lands in a JSONB column verbatim. Authoring it as JSON
//     means the bytes a human wrote are the bytes Postgres stores — no
//     translation step that can coerce a type on the way through.
//
//  2. YAML's scalar resolution is the opposite of what a corpus of
//     astrological terms wants. `language: no` is Norwegian to a human and
//     `false` to a YAML 1.1 parser; `sign: y` is a boolean. The failure is
//     silent and lands in a filter predicate, where it reads as a
//     retrieval bug months later.
//
//  3. No new dependency. A YAML parser sits in the module graph already,
//     transitively, but promoting it to direct for this is a dependency
//     this package does not need: `encoding/json` has exactly one parse of
//     any input, which is the property that matters for determinism.
//
// The cost is honest and should be stated: JSON front matter is uglier to
// write than YAML, and 400–600 documents is a lot of hand-written commas.
// `ingest-kb --check` exists for that reason — it names the file and the
// offending field rather than failing somewhere in a transaction.
package knowledge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// frontMatterFence delimits the JSON header from the markdown body.
//
// `---` rather than something JSON-aware because the body is markdown read
// by humans and `---` is what every static-site generator taught them to
// expect at the top of a file.
const frontMatterFence = "---"

// Document is one authored file: a header a human reviewed and a body a
// human wrote.
//
// Field names match migration 000007's columns deliberately. The ingester
// maps this straight onto `knowledge_documents`, and a rename on one side
// without the other is the kind of mismatch that compiles.
type Document struct {
	Title    string `json:"title"`
	Category string `json:"category"`
	Language string `json:"language"`

	// Provenance. NOT NULL with no default in the schema, and the same
	// here: PHASE-05 §4 rules out scraped and copyrighted material, and a
	// corpus where provenance is optional cannot be audited for licence
	// cleanliness after the fact.
	Source string `json:"source"`

	// 0–100. Classical sources outrank editorial when the retriever has to
	// choose between a text and a paraphrase of it.
	Authority int `json:"authority"`

	AstrologySystem string `json:"astrology_system"`

	Version int `json:"version"`

	// The filter surface — {planet, house, sign, nakshatra, topic[]}.
	// Held as raw JSON rather than a map so the authored bytes reach
	// Postgres unaltered and the chunker cannot reorder them.
	Metadata json.RawMessage `json:"metadata"`

	// Body is the markdown after the front matter, with surrounding
	// whitespace trimmed. Not part of the JSON header.
	Body string `json:"-"`

	// Checksum is SHA-256 over the whole file, header included.
	//
	// The corpus is the one input to ingestion that git protects and the
	// embeddings computed from it are the one output nothing protects: a
	// `vector(768)` column is 3 KB of floats where a flipped bit is
	// plausible at every value and the only symptom is retrieval that is
	// slightly, unprovably worse. Pinning the input lets `ingest-kb`
	// answer "is what is in the database still what we authored?" — see
	// task 5.4, which is where this gets stored and compared.
	Checksum string `json:"-"`

	// Path is where this came from, for error messages. Relative to the
	// corpus root so the string is stable across machines.
	Path string `json:"-"`
}

// ParseDocument reads one authored markdown file.
//
// Validation here mirrors the CHECK constraints in migration 000007 rather
// than deferring to them. Both layers are worth having: Postgres is the
// backstop that cannot be bypassed, and this is the one that names the file
// and the field while an author is still looking at it. A constraint
// violation surfacing from inside a 600-document transaction tells you
// only that something, somewhere, was blank.
func ParseDocument(path string, raw []byte) (*Document, error) {
	sum := sha256.Sum256(raw)

	header, body, err := splitFrontMatter(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	doc := &Document{
		// Defaults that match the schema's, so an authored file only has
		// to name what differs from the common case.
		Language:        "en",
		AstrologySystem: "vedic",
		Authority:       50,
		Version:         1,
	}

	decoder := json.NewDecoder(bytes.NewReader(header))
	// A typo'd key is a silently ignored key, and in a filter surface that
	// means a document that quietly never matches the query it was written
	// for. `extra="forbid"` is the rule on the Python side for the same
	// reason; this is its Go equivalent.
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(doc); err != nil {
		return nil, fmt.Errorf("%s: parse front matter: %w", path, err)
	}

	doc.Body = strings.TrimSpace(body)
	doc.Checksum = hex.EncodeToString(sum[:])
	doc.Path = path

	if doc.Metadata == nil {
		doc.Metadata = json.RawMessage("{}")
	}

	if err := doc.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return doc, nil
}

func (d *Document) validate() error {
	for _, field := range []struct {
		name  string
		value string
	}{
		{"title", d.Title},
		{"category", d.Category},
		{"source", d.Source},
		{"body", d.Body},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required and must not be blank", field.name)
		}
	}

	if d.Authority < 0 || d.Authority > 100 {
		return fmt.Errorf("authority is %d, must be between 0 and 100", d.Authority)
	}

	// kd_language_lower in the schema. Checked here too because the
	// retriever filters on an exact match: one document authored as "EN"
	// is one document no English query will ever reach.
	if d.Language != strings.ToLower(d.Language) {
		return fmt.Errorf("language %q must be lowercase", d.Language)
	}

	if d.Version < 1 {
		return fmt.Errorf("version is %d, must be at least 1", d.Version)
	}

	// Rejected rather than normalised. `metadata` is the filter surface and
	// a non-object there — a bare array, a string — would make every
	// `@>` containment query against it fail to match without erroring.
	var probe map[string]any
	if err := json.Unmarshal(d.Metadata, &probe); err != nil {
		return fmt.Errorf("metadata must be a JSON object: %w", err)
	}

	return nil
}

// splitFrontMatter divides a file at its fences.
//
// Strict about the opening fence being the first line: a file that merely
// contains `---` somewhere in its prose must fail loudly rather than be
// read as a document whose header is a paragraph of English.
func splitFrontMatter(raw []byte) (header []byte, body string, err error) {
	// A UTF-8 BOM ahead of the fence is what an editor on Windows leaves
	// behind, and it makes the first line not equal "---" by three bytes
	// nobody can see in a diff.
	text := strings.TrimPrefix(string(raw), "\ufeff")

	// Normalised before splitting, not after. CRLF line endings would
	// otherwise leave a trailing \r on the fence (so it never matches) and
	// on every heading and sentence downstream — producing chunks that
	// differ from the same content authored on Linux, which is precisely
	// the byte-identical guarantee this package sells.
	text = strings.ReplaceAll(text, "\r\n", "\n")

	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != frontMatterFence {
		return nil, "", fmt.Errorf("must begin with a %q fence", frontMatterFence)
	}

	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) != frontMatterFence {
			continue
		}
		header = []byte(strings.Join(lines[1:index], "\n"))
		body = strings.Join(lines[index+1:], "\n")
		return header, body, nil
	}

	return nil, "", fmt.Errorf("front matter is not closed by a %q fence", frontMatterFence)
}
