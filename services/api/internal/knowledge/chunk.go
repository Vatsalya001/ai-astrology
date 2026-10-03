package knowledge

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ChunkConfig is the windowing policy, from `KB_CHUNK_SIZE_TOKENS` and
// `KB_CHUNK_OVERLAP_TOKENS` (PHASE-05 §9).
type ChunkConfig struct {
	// SizeTokens is the target window. 350 per §4.
	SizeTokens int

	// OverlapTokens is how much of the previous chunk's tail each chunk
	// repeats. 50 per §4.
	//
	// Overlap exists because a window boundary that lands mid-explanation
	// leaves a chunk whose last sentence sets up a conclusion the next
	// chunk delivers, and retrieval returns one or the other.
	OverlapTokens int
}

// DefaultChunkConfig matches PHASE-05 §9.
func DefaultChunkConfig() ChunkConfig {
	return ChunkConfig{SizeTokens: 350, OverlapTokens: 50}
}

func (c ChunkConfig) validate() error {
	if c.SizeTokens < 1 {
		return fmt.Errorf("chunk size is %d tokens, must be at least 1", c.SizeTokens)
	}
	if c.OverlapTokens < 0 {
		return fmt.Errorf("chunk overlap is %d tokens, must not be negative", c.OverlapTokens)
	}
	// The one configuration that does not merely produce bad chunks but
	// fails to terminate: if the overlap is as large as the window, every
	// chunk begins where the previous one began. The windowing loop below
	// also forces progress independently, so this is a second line of
	// defence — but a config that could only work because of a guard
	// elsewhere is a config to reject at the boundary.
	if c.OverlapTokens >= c.SizeTokens {
		return fmt.Errorf(
			"chunk overlap (%d) must be smaller than chunk size (%d): equal or larger "+
				"means each window restarts at the previous window's start",
			c.OverlapTokens, c.SizeTokens,
		)
	}
	return nil
}

// Chunk is one retrievable passage. Maps onto `knowledge_chunks`.
type Chunk struct {
	// Content is what gets embedded and what the model eventually reads.
	// The document title is prepended — see buildContent.
	Content string

	// Index is the position within the document, 0-based.
	// `kc_position_unique` is (document_id, chunk_index).
	Index int

	// TokenCount is EstimateTokens(Content). An estimate; see tokens.go.
	TokenCount int

	// Metadata is the document's filter surface plus the section this
	// chunk came from.
	//
	// Derived, unlike `Document.Metadata`: it is re-marshalled, so key
	// order is `encoding/json`'s sorted order rather than the authored
	// order. Deterministic, which is what the byte-identical guarantee
	// needs — just not verbatim.
	Metadata json.RawMessage

	// Section is the markdown heading this chunk sits under, "" at the top
	// of a document. Duplicated into Metadata; kept as a field because the
	// ingester's dry-run output is read by humans.
	Section string
}

// ChunkDocument splits one document into retrievable passages.
//
// Deterministic: the same bytes in produce byte-identical chunks out, on
// any machine, in any order, forever. That is task 5.2's acceptance
// criterion and it is not a nicety — chunk boundaries decide embeddings,
// and an embedding that moves because the chunker drifted is a corpus that
// has to be rebuilt to find out whether retrieval got better or worse.
//
// Everything that could introduce order-dependence is therefore kept out:
// no map iteration on any path that affects output, no clock, no
// filesystem, no concurrency.
func ChunkDocument(doc *Document, cfg ChunkConfig) ([]Chunk, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	sections := splitSections(doc.Body)

	var chunks []Chunk
	for _, section := range sections {
		// A heading with no prose under it yields no units and therefore no
		// windows, so it produces no chunk — which is the wanted behaviour
		// and is implemented by windowUnits rather than by a check here. An
		// explicit `if len(units) == 0 { continue }` stood here until
		// mutation testing removed it and nothing failed.
		//
		// It matters that nothing is emitted: a chunk that is nothing but a
		// title embeds to something plausible and then competes with
		// passages that have content.
		units := unitsOf(section.blocks)

		prefix := buildPrefix(doc.Title, section.heading)
		budget := effectiveBudget(cfg, EstimateTokens(prefix))

		// Before windowing, not during. A single sentence longer than the
		// whole window has to be cut somewhere, and doing it here keeps
		// the windowing loop honest: every unit reaching it fits, so
		// "first unit is always admitted" cannot produce an oversize
		// chunk.
		units = splitOversizedUnits(units, budget)

		for _, window := range windowUnits(units, cfg, budget) {
			content := prefix + "\n\n" + joinUnits(units[window.start:window.end])

			metadata, err := chunkMetadata(doc.Metadata, section.heading)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", doc.Path, err)
			}

			chunks = append(chunks, Chunk{
				Content:    content,
				Index:      len(chunks),
				TokenCount: EstimateTokens(content),
				Metadata:   metadata,
				Section:    section.heading,
			})
		}
	}

	if len(chunks) == 0 {
		// Not an empty slice. A document that produces nothing is either
		// blank — which ParseDocument already refused — or made entirely of
		// headings, and either way it is an authoring mistake that must not
		// ingest quietly as a document with no retrievable content.
		return nil, fmt.Errorf("%s: produced no chunks (headings but no prose?)", doc.Path)
	}

	return chunks, nil
}

// buildPrefix is the per-chunk heading line.
//
// PHASE-05 §4: "parent document title prepended". It is the single highest
// -value line in the chunk, because astrology prose is written in the third
// person about an unnamed subject: a passage reading "authority arrives
// late, and is kept" is unembeddable on its own and unambiguous under
// "Saturn in the 10th House". The section heading is appended for the same
// reason one step down.
func buildPrefix(title, heading string) string {
	if heading == "" {
		return "# " + title
	}
	return "# " + title + " — " + heading
}

// chunkMetadata copies the document's filter surface down to the chunk and
// records the section.
//
// Copied rather than joined at query time because `metadata @> $2` in §4's
// retrieval SQL runs against `knowledge_chunks` and is what the
// `jsonb_path_ops` GIN index serves. Filtering through a join to the
// document would not use that index.
func chunkMetadata(docMetadata json.RawMessage, section string) (json.RawMessage, error) {
	fields := map[string]any{}
	if len(docMetadata) > 0 {
		if err := json.Unmarshal(docMetadata, &fields); err != nil {
			return nil, fmt.Errorf("document metadata: %w", err)
		}
	}

	if section != "" {
		// Namespaced so it cannot collide with an authored key. A document
		// whose metadata legitimately contains "section" would otherwise be
		// silently overwritten here.
		fields["_section"] = section
	}

	// json.Marshal sorts map keys, so this is stable across runs. Relied on
	// rather than assumed — TestChunkingIsByteIdentical would catch it if
	// the standard library ever stopped.
	return json.Marshal(fields)
}

// window is a half-open range over the unit list.
type window struct {
	start int
	end   int
}

// effectiveBudget is the token room left for prose once the prepended
// title line is paid for.
//
// Charged against every window because the title is prepended to every
// chunk. Ignoring it would make each chunk overshoot by however long the
// title is — small, but it compounds with the overlap and is free to get
// right.
func effectiveBudget(cfg ChunkConfig, prefixTokens int) int {
	budget := cfg.SizeTokens - prefixTokens
	if budget < 1 {
		// A title longer than the whole window. Pathological, and the only
		// sane response is one unit per chunk rather than zero chunks.
		return 1
	}
	return budget
}

// windowUnits is the windowing itself: pack units up to the token budget,
// then step back by the overlap.
func windowUnits(units []unit, cfg ChunkConfig, budget int) []window {
	var windows []window

	start := 0
	for start < len(units) {
		end := start
		used := 0
		for end < len(units) {
			cost := units[end].tokens
			if end > start && used+cost > budget {
				break
			}
			used += cost
			end++
		}

		windows = append(windows, window{start: start, end: end})

		if end >= len(units) {
			break
		}

		// Step back far enough to repeat ~OverlapTokens of tail, but never
		// so far that the next window starts at or before this one.
		//
		// The `next > start` floor is the termination guarantee. It holds
		// even if a single unit is larger than the entire overlap budget,
		// which is the case that makes the obvious implementation loop.
		next := end
		carried := 0
		for next > start+1 && carried+units[next-1].tokens <= cfg.OverlapTokens {
			next--
			carried += units[next].tokens
		}
		start = next
	}

	return windows
}

// joinUnits reassembles units into markdown.
//
// Units from the same block rejoin with the separator that block uses — a
// space for prose, a newline for a list or a table — and a block boundary
// becomes a blank line. Without this a chunk spanning a bulleted list comes
// out as one run-on paragraph, which is both unreadable in the "Why am I
// seeing this?" panel and worse to embed.
func joinUnits(units []unit) string {
	var out strings.Builder

	for index, u := range units {
		if index > 0 {
			switch {
			case u.block != units[index-1].block:
				out.WriteString("\n\n")
			default:
				out.WriteString(u.separator)
			}
		}
		out.WriteString(u.text)
	}

	return out.String()
}
