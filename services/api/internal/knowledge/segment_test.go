package knowledge

import (
	"strings"
	"testing"
	"unicode"
)

func TestSplitSentencesKeepsThingsThatLookLikeBoundariesButAreNot(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{
			"plain prose",
			"Saturn is slow. Jupiter is generous.",
			[]string{"Saturn is slow.", "Jupiter is generous."},
		},
		{
			// On nearly every page of this corpus.
			"a decimal is not a boundary",
			"The Moon moves 13.2 degrees per day. Ketu sits 180.0 degrees from Rahu.",
			[]string{
				"The Moon moves 13.2 degrees per day.",
				"Ketu sits 180.0 degrees from Rahu.",
			},
		},
		{
			"interior-period abbreviations",
			"Consider e.g. this clause, i.e. this one, and stop.",
			[]string{"Consider e.g. this clause, i.e. this one, and stop."},
		},
		{
			"a title is not a boundary",
			"See Dr. Raman's tables. They disagree.",
			[]string{"See Dr. Raman's tables.", "They disagree."},
		},
		{
			"etc. mid-sentence",
			"Houses, signs, nakshatras, etc. all filter the same way.",
			[]string{"Houses, signs, nakshatras, etc. all filter the same way."},
		},
		{
			"a lowercase continuation is not a boundary",
			"Saturn rules Capricorn. and Aquarius too.",
			[]string{"Saturn rules Capricorn. and Aquarius too."},
		},
		{
			"terminator runs and closers stay attached",
			"Really?! Yes. (He said so.) Then left.",
			[]string{"Really?!", "Yes.", "(He said so.)", "Then left."},
		},
		{
			// Devanagari has no letter case, so the "next character is
			// uppercase" rule can never fire. Without the danda rule a
			// Hindi paragraph is one unit however long it is.
			"the danda breaks",
			"शनि दशम भाव में। यह कर्म का भाव है॥",
			[]string{"शनि दशम भाव में।", "यह कर्म का भाव है॥"},
		},
		{
			"a dotted identifier is one sentence",
			"Visit example.com for tables.",
			[]string{"Visit example.com for tables."},
		},
		{
			"no terminator at all",
			"A fragment with no full stop",
			[]string{"A fragment with no full stop"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitSentences(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d sentences %q, want %d %q",
					len(got), got, len(tc.want), tc.want)
			}
			for index := range got {
				if got[index] != tc.want[index] {
					t.Errorf("sentence %d:\n got %q\nwant %q", index, got[index], tc.want[index])
				}
			}
		})
	}
}

func TestSentenceSplittingLosesNoNonSpaceCharacter(t *testing.T) {
	// The split is the step where content can silently disappear, and a
	// dropped clause in a knowledge chunk is a claim the model then makes
	// without its qualifier.
	for _, input := range []string{
		"Saturn is slow. Jupiter is generous.",
		"Really?! Yes. (He said so.)",
		"शनि दशम भाव में। यह कर्म का भाव है॥",
		"...!?!?...",
		"e.g. one. i.e. two. Three.",
	} {
		joined := strings.Join(splitSentences(input), "")
		if stripSpace(joined) != stripSpace(input) {
			t.Errorf("input %q round-trips to %q", input, joined)
		}
	}
}

func TestHeadingOf(t *testing.T) {
	cases := map[string]struct {
		heading string
		ok      bool
	}{
		"# Title":                  {"Title", true},
		"### Deeper":               {"Deeper", true},
		"## Career and vocation":   {"Career and vocation", true},
		"## Closing hashes ##":     {"Closing hashes", true},
		"  ## Indented":            {"Indented", true},
		"#NoSpace":                 {"", false},
		"####### Seven is too far": {"", false},
		"Not a heading":            {"", false},
		"#":                        {"", false},
		"## ":                      {"", false},
		"":                         {"", false},
	}

	for line, want := range cases {
		heading, ok := headingOf(line)
		if ok != want.ok || heading != want.heading {
			t.Errorf("%q → (%q, %v), want (%q, %v)", line, heading, ok, want.heading, want.ok)
		}
	}
}

func TestBlockClassification(t *testing.T) {
	cases := []struct {
		name       string
		lines      []string
		lineAtomic bool
	}{
		{"prose", []string{"One sentence.", "wrapped onto two lines."}, false},
		{"dash list", []string{"- one", "- two"}, true},
		{"star list", []string{"* one", "* two"}, true},
		{"plus list", []string{"+ one", "+ two"}, true},
		{"ordered list", []string{"1. one", "2. two"}, true},
		{"paren list", []string{"1) one", "2) two"}, true},
		{"table", []string{"| a | b |", "|---|---|"}, true},
		{"code fence", []string{"```", "x = 1", "```"}, true},
		{"tilde fence", []string{"~~~", "x = 1", "~~~"}, true},
		// The near-miss: a sentence about subtraction is not a list.
		{"prose starting with a dash word", []string{"-5 degrees is the orb."}, false},
		{"a numbered year", []string{"1947. A year, not an item."}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blocks := blocksOf(tc.lines)
			if len(blocks) != 1 {
				t.Fatalf("got %d blocks, want 1: %+v", len(blocks), blocks)
			}
			if got := blocks[0].lineAtomic; got != tc.lineAtomic {
				t.Errorf("lineAtomic is %v, want %v", got, tc.lineAtomic)
			}
		})
	}
}

func TestALeadInParagraphAndItsListSplitIntoTwoBlocks(t *testing.T) {
	// The commonest layout in the corpus, and the one that a
	// classify-the-whole-block implementation gets wrong in one direction
	// or the other: as prose the list collapses into a run-on line; as a
	// list the lead-in sentence becomes unsplittable.
	blocks := blocksOf([]string{
		"Occupations traditionally include:",
		"- Administration and law",
		"- Engineering",
	})

	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2 (the lead-in and the list)", len(blocks))
	}
	if blocks[0].lineAtomic {
		t.Error("the lead-in sentence was classified as a list")
	}
	if !blocks[1].lineAtomic {
		t.Error("the list was classified as prose; its items would be rejoined with spaces")
	}
	if len(blocks[1].lines) != 2 {
		t.Errorf("the list has %d lines, want 2", len(blocks[1].lines))
	}
}

func TestAWrappedListItemStaysWithItsList(t *testing.T) {
	blocks := blocksOf([]string{
		"- Research requiring decades",
		"  rather than quarters",
		"- Institutions",
	})

	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want 1: the indented line is a wrapped item, "+
			"not a new paragraph: %+v", len(blocks), blocks)
	}
}

func TestBlankLinesInsideACodeFenceDoNotSplitIt(t *testing.T) {
	blocks := blocksOf([]string{"```", "a = 1", "", "b = 2", "```", "", "After."})

	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2 (the fence and the paragraph)", len(blocks))
	}
	if len(blocks[0].lines) != 5 {
		t.Errorf("the fence was split at its blank line: %q", blocks[0].lines)
	}
}

// ── The token estimator ──

func TestEstimateTokensIsBoundedByWordsAndRunes(t *testing.T) {
	// The estimator is a heuristic (see tokens.go) so these are the
	// properties worth pinning rather than specific counts:
	//
	//	words ≤ estimate ≤ runes
	//
	// The lower bound is the one that protects against truncation. An
	// estimator that could return fewer tokens than there are words would
	// let a chunk past the window, and truncation at the embedding model
	// is silent — the database keeps claiming to hold text that was never
	// embedded.
	for _, text := range []string{
		"Saturn in the tenth house rewards endurance.",
		"The Moon moves 13.2 degrees per day.",
		"Viśākhā, Bṛhat Parāśara Horā Śāstra, Pūrvāṣāḍhā",
		"शनि दशम भाव में",
		"a b c d e f g",
		strings.Repeat("interpretation ", 50),
		"| Saturn | 10 | Authority |",
	} {
		words := len(strings.Fields(text))
		runes := len([]rune(strings.Join(strings.Fields(text), "")))
		got := EstimateTokens(text)

		if got < words {
			t.Errorf("%q: estimate %d is below the word count %d — it could let a "+
				"chunk past the window and be silently truncated", text, got, words)
		}
		if got > runes {
			t.Errorf("%q: estimate %d exceeds the character count %d", text, got, runes)
		}
	}
}

func TestEstimateTokensChargesDevanagariPerCharacter(t *testing.T) {
	// PHASE-05 §1 ships `en` only, which makes this the branch that is
	// wrong before anyone notices. Devanagari tokens are one or two
	// characters, so the Latin divisor would undercount a Hindi chunk
	// several-fold and produce chunks well over the window.
	hindi := "शनिदशमभाव"
	latin := "saturntenth"

	perRune := EstimateTokens(hindi)
	if perRune != len([]rune(hindi)) {
		t.Errorf("Devanagari run charged %d tokens for %d characters",
			perRune, len([]rune(hindi)))
	}

	if EstimateTokens(latin) >= len([]rune(latin)) {
		t.Errorf("a Latin run of %d characters was charged %d tokens — it should be "+
			"divided, not counted per character", len(latin), EstimateTokens(latin))
	}
}

func TestEstimateTokensIsEmptyForEmptyAndWhitespace(t *testing.T) {
	for _, text := range []string{"", " ", "\n\n\t ", "\r\n"} {
		if got := EstimateTokens(text); got != 0 {
			t.Errorf("%q estimated at %d tokens", text, got)
		}
	}
}

func TestEstimateTokensDoesNotSplitNumberedOrdinals(t *testing.T) {
	// "10th" is one word to a tokeniser. Splitting it into a number and a
	// word would double-charge every house reference in the corpus, which
	// is most sentences in it.
	if EstimateTokens("10th") != 1 {
		t.Errorf("10th estimated at %d tokens, want 1", EstimateTokens("10th"))
	}
}

func stripSpace(s string) string {
	var out strings.Builder
	for _, r := range s {
		if !unicode.IsSpace(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}
