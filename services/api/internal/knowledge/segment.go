package knowledge

import (
	"strings"
	"unicode"
)

// ── The segmentation model ──
//
// Three levels, coarsest first:
//
//	section — everything under one markdown heading. A HARD boundary: no
//	          chunk and no overlap ever crosses one.
//	block   — a run of non-blank lines. Paragraph, list, table, code fence.
//	unit    — a sentence, or for a list or table, a line.
//
// Sections are hard because PHASE-05 §4's premise is that astrology content
// is naturally atomic — "Saturn in the 10th house" is one idea — and a
// heading is the strongest statement an author makes about where one idea
// ends. Bleeding the tail of a "Career" section into the head of a
// "Marriage" one is exactly the dilution §4 warns about when it says more
// context is not better context.

// section is a heading and the blocks beneath it.
type section struct {
	// heading is the text of the markdown heading, without its `#`s.
	// Empty for content above the first heading.
	heading string
	blocks  []block
}

// block is a run of lines of one kind, classified by what rejoining it
// needs.
type block struct {
	lines []string
	kind  lineKind

	// separator rejoins units from this block: " " for prose, "\n" for
	// anything whose line structure carries meaning.
	separator string

	// lineAtomic means units from this block must not be split into
	// sentences — true for lists, tables and code.
	lineAtomic bool
}

// unit is the smallest thing the windower moves.
type unit struct {
	text      string
	tokens    int
	block     int
	separator string

	// multiline means the text carries its own newlines and must be cut at
	// a line boundary if it ever has to be cut at all.
	multiline bool
}

// splitSections divides a body at its ATX headings.
func splitSections(body string) []section {
	var sections []section

	current := section{}
	var pending []string

	flushBlocks := func() {
		current.blocks = append(current.blocks, blocksOf(pending)...)
		pending = nil
	}

	flushSection := func() {
		flushBlocks()
		if current.heading != "" || len(current.blocks) > 0 {
			sections = append(sections, current)
		}
		current = section{}
	}

	inFence := false
	for _, line := range strings.Split(body, "\n") {
		if isFenceDelimiter(line) {
			inFence = !inFence
			pending = append(pending, line)
			continue
		}

		// A `#` inside a fenced block is a shell comment or a markdown
		// example, not a heading. Checked before heading detection because
		// treating one as a section boundary would split a code sample in
		// half and leave the closing fence in the next chunk.
		if !inFence {
			if heading, ok := headingOf(line); ok {
				flushSection()
				current.heading = heading
				continue
			}
		}

		pending = append(pending, line)
	}

	flushSection()

	return sections
}

// headingOf recognises an ATX heading: one to six `#` then a space.
//
// The trailing space is required, so `#career` is prose and `# Career` is a
// heading. Setext headings (underlining with `===`) are not supported, and
// that is a deliberate narrowing: `---` already means front matter here,
// and a format with two meanings for one token is a format that will cut a
// document in the wrong place.
func headingOf(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " ")

	hashes := 0
	for hashes < len(trimmed) && trimmed[hashes] == '#' {
		hashes++
	}
	if hashes == 0 || hashes > 6 || hashes >= len(trimmed) || trimmed[hashes] != ' ' {
		return "", false
	}

	heading := strings.TrimSpace(trimmed[hashes+1:])
	// Closing hashes — `## Career ##` — are decoration in ATX and would
	// otherwise end up in the chunk prefix and in metadata.
	heading = strings.TrimSpace(strings.TrimRight(heading, "#"))
	if heading == "" {
		return "", false
	}

	return heading, true
}

func isFenceDelimiter(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// lineKind is what a single line looks like.
//
// Classification is per LINE and blocks are then runs of one kind, rather
// than classifying a whole block by whether any line in it looks like a
// list. The difference shows up on the commonest layout in this corpus:
//
//	Occupations traditionally include:
//	- Administration and law
//	- Engineering
//
// With no blank line between them that is one block. Classified as a whole
// it is either all prose — collapsing the list into a run-on line — or all
// list, leaving the lead-in sentence unsplittable. Split at the kind
// change, both parts are handled correctly.
type lineKind int

const (
	kindProse lineKind = iota
	kindList
	kindTable
	kindCode
)

// blocksOf groups lines into blocks, breaking on blank lines and on a
// change of line kind. Fenced code stays whole even though it contains
// blank lines and `#` characters.
func blocksOf(lines []string) []block {
	var blocks []block
	var current []string
	currentKind := kindProse
	inFence := false

	flush := func() {
		if len(current) == 0 {
			return
		}
		blocks = append(blocks, blockFor(current, currentKind))
		current = nil
	}

	for _, line := range lines {
		if isFenceDelimiter(line) {
			if inFence {
				// Closing delimiter: belongs to the fence it closes.
				current = append(current, line)
				inFence = false
				flush()
				continue
			}
			flush()
			inFence = true
			currentKind = kindCode
			current = append(current, line)
			continue
		}

		if inFence {
			current = append(current, line)
			continue
		}

		if strings.TrimSpace(line) == "" {
			flush()
			currentKind = kindProse
			continue
		}

		kind := kindOf(line, currentKind, len(current) > 0)
		if len(current) > 0 && kind != currentKind {
			flush()
		}
		currentKind = kind
		current = append(current, line)
	}

	flush()

	return blocks
}

// kindOf classifies one line, given what the line before it was.
//
// The context argument exists for list continuations: a wrapped list item's
// second line is indistinguishable from prose on its own.
func kindOf(line string, previous lineKind, hasPrevious bool) lineKind {
	trimmed := strings.TrimSpace(line)

	switch {
	case strings.HasPrefix(trimmed, "|"):
		// A table. Splitting it drops the header row from every chunk
		// after the first, leaving columns nothing identifies.
		return kindTable

	case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "),
		strings.HasPrefix(trimmed, "+ "), isOrderedListItem(trimmed):
		// A list. Each item is one idea by construction, which is exactly
		// the unit the windower wants.
		return kindList
	}

	// An indented line directly under a list item is that item wrapped, not
	// a new paragraph. Without this, every hard-wrapped bullet in the
	// corpus becomes its own prose block.
	if hasPrevious && previous == kindList && line != trimmed {
		return kindList
	}

	return kindProse
}

func blockFor(lines []string, kind lineKind) block {
	if kind == kindProse {
		return block{lines: lines, kind: kind, separator: " ", lineAtomic: false}
	}
	// Lists, tables and code: line structure carries meaning, so lines
	// rejoin with newlines and are never split into sentences. Half a code
	// fence is both unreadable and, rendered in the chat UI, a broken block
	// that swallows the rest of the message.
	return block{lines: lines, kind: kind, separator: "\n", lineAtomic: true}
}

// maxOrderedListDigits bounds how long an ordered-list marker may be.
//
// Two, which is the narrowest bound that still covers every list a human
// writes and excludes the case that found this: a paragraph opening with a
// year. "1947. A year, not an item." is legal markdown for list item 1947
// and is prose every time it appears in this corpus. Treating it as a list
// made the whole paragraph one unsplittable unit, so a long one got cut at
// a word boundary instead of a sentence boundary.
const maxOrderedListDigits = 2

func isOrderedListItem(trimmed string) bool {
	digits := 0
	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits > maxOrderedListDigits || digits+1 >= len(trimmed) {
		return false
	}
	return (trimmed[digits] == '.' || trimmed[digits] == ')') && trimmed[digits+1] == ' '
}

// unitsOf flattens blocks into the units the windower moves.
func unitsOf(blocks []block) []unit {
	var units []unit

	for index, b := range blocks {
		switch b.kind {
		case kindCode, kindTable:
			// One unit for the whole block, lines verbatim.
			//
			// Not one unit per line, which is what this did first and what
			// the golden file caught: per-line units drop the blank lines
			// inside a code fence, because a blank line is not a unit. In
			// code a blank line is content, and in a table there is no
			// window size at which splitting is right — a chunk of data
			// rows without the header has columns nothing identifies.
			text := strings.Join(trimBlankEdges(b.lines), "\n")
			if strings.TrimSpace(text) == "" {
				continue
			}
			units = append(units, unit{
				text:      text,
				tokens:    EstimateTokens(text),
				block:     index,
				separator: b.separator,
				multiline: true,
			})
			continue

		case kindList:
			for _, line := range b.lines {
				if strings.TrimSpace(line) == "" {
					continue
				}
				// Trailing whitespace only. The leading indentation of a
				// wrapped item is what makes it a continuation rather than
				// a new paragraph, and trimming it changes the markdown.
				units = append(units, newUnit(strings.TrimRight(line, " \t"), index, b.separator))
			}
			continue
		}

		// Prose: the lines of a paragraph are a hard-wrap artefact, so they
		// are rejoined before sentence splitting. Splitting line by line
		// would make every wrapped line its own unit and let a window end
		// mid-sentence at column 80.
		paragraph := strings.Join(trimEach(b.lines), " ")
		for _, sentence := range splitSentences(paragraph) {
			units = append(units, newUnit(sentence, index, b.separator))
		}
	}

	return units
}

// newUnit does NOT trim its text. Callers pass exactly the bytes the chunk
// should carry — prose sentences arrive trimmed from splitSentences, and a
// list line keeps the leading indentation that makes it a continuation.
func newUnit(text string, blockIndex int, separator string) unit {
	return unit{
		text:      text,
		tokens:    EstimateTokens(text),
		block:     blockIndex,
		separator: separator,
	}
}

func trimEach(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// trimBlankEdges drops blank lines from the start and end of a block while
// keeping the ones in the middle.
func trimBlankEdges(lines []string) []string {
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[start:end]
}

// splitOversizedUnits cuts any unit that cannot fit a window on its own.
//
// Reached by a 400-word sentence, a wide table row, or a code fence longer
// than the window. The cut is at a word boundary, which is ugly and
// correct: the alternative is a chunk that overflows the embedding model's
// context and gets silently truncated, losing content that the database
// still claims to hold.
func splitOversizedUnits(units []unit, budget int) []unit {
	oversized := false
	for _, u := range units {
		if u.tokens > budget {
			oversized = true
			break
		}
	}
	if !oversized {
		// The overwhelmingly common path. Returned as-is so the slice is
		// not copied for every document in the corpus.
		return units
	}

	out := make([]unit, 0, len(units)+1)
	for _, u := range units {
		if u.tokens <= budget {
			out = append(out, u)
			continue
		}
		for _, piece := range splitOversized(u, budget) {
			piece.block = u.block
			piece.separator = u.separator
			out = append(out, piece)
		}
	}

	return out
}

// splitOversized cuts one unit down to size.
//
// A multiline unit — a table or a code fence longer than the window — is
// cut at LINE boundaries first. Cutting it mid-line, which is what the word
// splitter does, leaves a half-written table row or a syntactically broken
// line of code; cutting between lines at least leaves both halves
// readable. Only a single line that is itself over budget falls through to
// words.
func splitOversized(u unit, budget int) []unit {
	if !u.multiline {
		pieces := splitByWords(u.text, budget)
		out := make([]unit, 0, len(pieces))
		for _, piece := range pieces {
			out = append(out, newUnit(piece, u.block, u.separator))
		}
		return out
	}

	var out []unit
	var current []string
	used := 0

	flush := func() {
		if len(current) == 0 {
			return
		}
		next := newUnit(strings.Join(current, "\n"), u.block, u.separator)
		next.multiline = true
		out = append(out, next)
		current = nil
		used = 0
	}

	for _, line := range strings.Split(u.text, "\n") {
		cost := EstimateTokens(line)

		if cost > budget {
			flush()
			for _, piece := range splitByWords(line, budget) {
				out = append(out, newUnit(piece, u.block, u.separator))
			}
			continue
		}

		if len(current) > 0 && used+cost > budget {
			flush()
		}
		current = append(current, line)
		used += cost
	}

	flush()

	return out
}

// splitByWords breaks text into pieces of at most budget estimated tokens.
func splitByWords(text string, budget int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	var pieces []string
	var current []string
	used := 0

	for _, word := range words {
		cost := EstimateTokens(word)
		if len(current) > 0 && used+cost > budget {
			pieces = append(pieces, strings.Join(current, " "))
			current = nil
			used = 0
		}
		current = append(current, word)
		used += cost
	}

	if len(current) > 0 {
		pieces = append(pieces, strings.Join(current, " "))
	}

	return pieces
}

// ── Sentence splitting ──

// nonBreakingBefore are the words that end in a period without ending a
// sentence.
//
// Short on purpose. Every entry is a rule that can misfire the other way —
// a real sentence ending in "etc." will not be split — and a long list of
// abbreviations is a long list of chances to be wrong about a corpus nobody
// has written yet. These are the ones that actually appear in astrological
// prose.
var nonBreakingBefore = map[string]bool{
	"approx": true,
	"c":      true,
	"ca":     true,
	"cf":     true,
	"dr":     true,
	"e.g":    true,
	"eg":     true,
	"et al":  true,
	"etc":    true,
	"fig":    true,
	"i.e":    true,
	"ie":     true,
	"mr":     true,
	"mrs":    true,
	"ms":     true,
	"no":     true,
	"vs":     true,
}

// splitSentences divides a paragraph at sentence boundaries.
//
// A boundary needs all three of: a terminator, whitespace after it, and a
// following character that could start a sentence. Requiring all three is
// what keeps "29.5 degrees" and "the 10th house (see §4) is" in one piece,
// and those appear on nearly every page of this corpus.
func splitSentences(paragraph string) []string {
	runes := []rune(paragraph)

	var sentences []string
	start := 0

	for index := 0; index < len(runes); index++ {
		if !isTerminator(runes[index]) {
			continue
		}

		// Consume a run of terminators and the closing punctuation that
		// can follow one: `?!`, `."`, `.)`.
		end := index
		for end+1 < len(runes) && (isTerminator(runes[end+1]) || isCloser(runes[end+1])) {
			end++
		}

		if !breaksHere(runes, start, index, end) {
			index = end
			continue
		}

		if sentence := strings.TrimSpace(string(runes[start : end+1])); sentence != "" {
			sentences = append(sentences, sentence)
		}
		start = end + 1
		index = end
	}

	if tail := strings.TrimSpace(string(runes[start:])); tail != "" {
		sentences = append(sentences, tail)
	}

	return sentences
}

// breaksHere decides whether the terminator run ending at `end` is a real
// sentence boundary.
func breaksHere(runes []rune, start, terminator, end int) bool {
	// No special case for the danda, though there was one here first.
	//
	// Mutation testing deleted it and every test still passed, which is the
	// definition of a guard that cannot be trusted. The general rule below
	// already covers Devanagari: `unicode.IsLower` is false for a caseless
	// script, so the "next character could begin a sentence" test passes
	// for Hindi exactly as it does for a capital letter. Removing it also
	// fixed two cases the special case got wrong — a danda at the very end
	// of a paragraph split off an empty sentence, and a danda used as a
	// digit separator split mid-number.

	// Must be followed by whitespace. "3.5" and "example.com" are not two
	// sentences.
	if end+1 >= len(runes) {
		return false
	}
	if !unicode.IsSpace(runes[end+1]) {
		return false
	}

	// A single period after a known abbreviation is not a boundary.
	if runes[terminator] == '.' && end == terminator {
		if nonBreakingBefore[lastWordBefore(runes, start, terminator)] {
			return false
		}
	}

	// The next real character has to look like a beginning. A lowercase
	// letter means the period was doing something else — most often an
	// abbreviation this package has never heard of, which is the case the
	// short list above cannot cover.
	for cursor := end + 1; cursor < len(runes); cursor++ {
		if unicode.IsSpace(runes[cursor]) {
			continue
		}
		return !unicode.IsLower(runes[cursor])
	}

	// Trailing whitespace to the end of the paragraph: nothing follows, so
	// there is no second sentence to split off.
	return false
}

// lastWordBefore is the lowercased word immediately preceding a period.
//
// Keeps interior periods, so "e.g." is looked up as "e.g" rather than "g" —
// without that, the abbreviation list could only ever match single-period
// forms.
func lastWordBefore(runes []rune, start, terminator int) string {
	begin := terminator
	for begin > start && !unicode.IsSpace(runes[begin-1]) {
		begin--
	}
	return strings.ToLower(strings.TrimSpace(string(runes[begin:terminator])))
}

func isTerminator(r rune) bool {
	switch r {
	case '.', '!', '?', '।', '॥':
		return true
	}
	return false
}

func isCloser(r rune) bool {
	switch r {
	case '"', '\'', ')', ']', '}', '»', '”', '’':
		return true
	}
	return false
}
