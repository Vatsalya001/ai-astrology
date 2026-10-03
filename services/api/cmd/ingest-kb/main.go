// Command ingest-kb reads the authored astrology corpus, chunks it
// deterministically, and (from task 5.4) embeds and stores it.
//
// It lives with api-service because api-service owns the
// `knowledge_documents` and `knowledge_chunks` tables — the single-writer
// rule applies to corpus data exactly as it does to user data. PHASE-05 §4:
// "Go orchestrates, Python embeds."
//
// Usage:
//
//	go run ./cmd/ingest-kb --check               # parse and chunk, touch nothing
//	go run ./cmd/ingest-kb --check --print 3     # ...and show three chunks
//
// `--check` is the whole of task 5.2 and it needs no database, no model and
// no network. That is the point of doing chunking in Go: the step that
// decides what gets embedded is reproducible and reviewable on its own,
// before anything is paid for.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/config"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/knowledge"
)

// corpusSubdir is where the authored markdown lives, relative to the
// repository root. PHASE-05 §4.
const corpusSubdir = "packages/content/knowledge"

// rootMarker identifies the repository root when walking up from the
// working directory.
//
// `Taskfile.yml` rather than `.git`, because a git worktree or a submodule
// checkout has a `.git` FILE at a level that is not the root, and the
// symptom would be a corpus directory that cannot be found from inside one.
const rootMarker = "Taskfile.yml"

// defaultCorpusDir finds the corpus by walking up to the repository root.
//
// Not a relative path constant, which is what this was first and which
// broke immediately: `../../packages/content/knowledge` is correct from
// `services/api`, where `task` runs, and wrong from
// `services/api/cmd/ingest-kb`, where `go test` runs. A default that
// depends on the caller's working directory is a default that works until
// somebody invokes the tool from somewhere reasonable.
func defaultCorpusDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return corpusSubdir
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, rootMarker)); err == nil {
			return filepath.Join(dir, corpusSubdir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the filesystem root without finding a marker. Return
			// the bare relative path so the error names the corpus rather
			// than the search.
			return corpusSubdir
		}
		dir = parent
	}
}

func main() {
	corpusDir := flag.String("corpus", defaultCorpusDir(),
		"directory of authored markdown, searched recursively")
	check := flag.Bool("check", false,
		"parse and chunk only; write nothing and contact nothing")
	printN := flag.Int("print", 0,
		"print the first N chunks in full, for eyeballing boundaries")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := config.LoadKnowledgeBase()
	if err != nil {
		logger.Error("configuration", slog.Any("err", err))
		os.Exit(2)
	}

	chunkCfg := knowledge.ChunkConfig{
		SizeTokens:    cfg.ChunkSizeTokens,
		OverlapTokens: cfg.ChunkOverlapTokens,
	}

	corpus, err := loadCorpus(*corpusDir, chunkCfg)
	if err != nil {
		logger.Error("corpus", slog.Any("err", err))
		os.Exit(1)
	}

	report(corpus, chunkCfg, *printN)

	if !*check {
		// Task 5.4 is the embed-and-insert half. Refusing explicitly rather
		// than silently succeeding: a command that prints a summary and
		// exits 0 reads as "ingested", and the next person to look would
		// find an empty table and no error anywhere.
		fmt.Fprintln(os.Stderr,
			"\nnothing was written: embedding and insertion are task 5.4.\n"+
				"Re-run with --check to make that explicit.")
		os.Exit(3)
	}
}

// loadedDocument pairs a parsed document with its chunks.
type loadedDocument struct {
	doc    *knowledge.Document
	chunks []knowledge.Chunk
}

// loadCorpus walks the corpus directory in a fixed order and chunks
// everything in it.
//
// Fixed order matters beyond tidiness: it is what makes the corpus
// checksum below mean anything, and in task 5.4 it is what makes two
// ingestion runs produce the same insert order — which is the difference
// between a re-run that is a no-op and one that deadlocks against itself
// on a concurrent run.
func loadCorpus(dir string, cfg knowledge.ChunkConfig) ([]loadedDocument, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", dir, err)
	}

	var paths []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}
		// README.md and the like are documentation ABOUT the corpus, and
		// ingesting them puts instructions for authors into the retrieval
		// index where they will eventually be quoted back at a user.
		if strings.HasPrefix(entry.Name(), "_") || entry.Name() == "README.md" {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}

	if len(paths) == 0 {
		return nil, fmt.Errorf("no .md files under %s", root)
	}

	// WalkDir is already lexical, but sorting makes that a property of this
	// function rather than of its dependency.
	sort.Strings(paths)

	// Every error is collected rather than returned at the first one. An
	// author fixing a 600-document corpus one error per run is an author
	// who stops fixing it.
	var documents []loadedDocument
	var problems []string

	for _, path := range paths {
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			relative = path
		}

		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", relative, readErr))
			continue
		}

		doc, parseErr := knowledge.ParseDocument(filepath.ToSlash(relative), raw)
		if parseErr != nil {
			problems = append(problems, parseErr.Error())
			continue
		}

		chunks, chunkErr := knowledge.ChunkDocument(doc, cfg)
		if chunkErr != nil {
			problems = append(problems, chunkErr.Error())
			continue
		}

		documents = append(documents, loadedDocument{doc: doc, chunks: chunks})
	}

	if len(problems) > 0 {
		return nil, fmt.Errorf("%d of %d documents are invalid:\n  %s",
			len(problems), len(paths), strings.Join(problems, "\n  "))
	}

	if err := checkIdentityCollisions(documents); err != nil {
		return nil, err
	}

	return documents, nil
}

// checkIdentityCollisions enforces `kd_identity_idx` before the database
// does.
//
// The unique index is on (title, language, version), which is what makes
// re-ingestion an upsert rather than a duplication. Two files sharing an
// identity is therefore not a constraint violation to be reported from
// inside a transaction — it is one authored document overwriting another,
// and the loser disappears from the corpus silently.
func checkIdentityCollisions(documents []loadedDocument) error {
	seen := make(map[string]string, len(documents))

	var collisions []string
	for _, loaded := range documents {
		key := fmt.Sprintf("%s\x00%s\x00%d",
			loaded.doc.Title, loaded.doc.Language, loaded.doc.Version)

		if first, ok := seen[key]; ok {
			collisions = append(collisions, fmt.Sprintf(
				"%q (language %s, version %d) is authored in both %s and %s",
				loaded.doc.Title, loaded.doc.Language, loaded.doc.Version,
				first, loaded.doc.Path))
			continue
		}
		seen[key] = loaded.doc.Path
	}

	if len(collisions) > 0 {
		// Sorted: map iteration above means the order these were found in
		// is not stable, and an error message that reorders between runs is
		// one nobody can diff.
		sort.Strings(collisions)
		return fmt.Errorf("duplicate document identities (kd_identity_idx):\n  %s",
			strings.Join(collisions, "\n  "))
	}

	return nil
}

func report(corpus []loadedDocument, cfg knowledge.ChunkConfig, printN int) {
	totalChunks, totalTokens := 0, 0
	largest := 0

	byCategory := map[string]int{}

	for _, loaded := range corpus {
		byCategory[loaded.doc.Category]++
		for _, chunk := range loaded.chunks {
			totalChunks++
			totalTokens += chunk.TokenCount
			if chunk.TokenCount > largest {
				largest = chunk.TokenCount
			}
		}
	}

	fmt.Printf("corpus\n")
	fmt.Printf("  documents          %d\n", len(corpus))
	fmt.Printf("  chunks             %d\n", totalChunks)
	fmt.Printf("  estimated tokens   %d\n", totalTokens)
	fmt.Printf("  largest chunk      %d of %d allowed\n", largest, cfg.SizeTokens)
	fmt.Printf("  window / overlap   %d / %d\n", cfg.SizeTokens, cfg.OverlapTokens)
	fmt.Printf("  corpus checksum    %s\n", corpusChecksum(corpus))

	fmt.Printf("\ncategories\n")
	for _, category := range sortedKeys(byCategory) {
		fmt.Printf("  %-18s %d\n", category, byCategory[category])
	}

	// PHASE-05 §16 wants ≥400 documents. Stated as progress rather than
	// enforced: the gate belongs to task 5.7, and a chunker that refuses to
	// run until the corpus is finished is a chunker nobody can develop
	// against.
	fmt.Printf("\nphase gate: %d of 400 documents\n", len(corpus))

	if printN > 0 {
		printChunks(corpus, printN)
	}
}

// corpusChecksum is a single value over every authored byte.
//
// Derived from the per-document checksums in sorted path order so that it
// is independent of filesystem walk order. Task 5.4 uses it to answer "is
// what is in the database still what we authored?" — worth having because
// the embeddings are the one artefact in this system that git does not
// protect and a wrong float in a vector produces no error, just slightly
// worse retrieval.
func corpusChecksum(corpus []loadedDocument) string {
	hash := sha256.New()
	for _, loaded := range corpus {
		fmt.Fprintf(hash, "%s %s\n", loaded.doc.Checksum, loaded.doc.Path)
	}
	return hex.EncodeToString(hash.Sum(nil))[:16]
}

func printChunks(corpus []loadedDocument, limit int) {
	shown := 0
	for _, loaded := range corpus {
		for _, chunk := range loaded.chunks {
			if shown >= limit {
				return
			}
			fmt.Printf("\n─── %s #%d · %d tokens · %s\n%s\n",
				loaded.doc.Path, chunk.Index, chunk.TokenCount, chunk.Metadata, chunk.Content)
			shown++
		}
	}
}

func sortedKeys(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
