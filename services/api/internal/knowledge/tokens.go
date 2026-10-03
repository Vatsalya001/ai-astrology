package knowledge

import (
	"unicode"
)

// ── Token counting without a tokenizer ──
//
// `nomic-embed-text` tokenises with a BERT WordPiece vocabulary that has no
// Go implementation, and vendoring a 30k-entry vocab into this repo to
// decide where to cut a paragraph is not a trade worth making. So this is
// an ESTIMATE, and the rest of the package is built so that being wrong
// about it is survivable:
//
//   - The window is 350 tokens against a model that accepts 2048. The
//     estimate would have to be wrong by a factor of five before a chunk
//     got truncated, and truncation is the only failure that is silent.
//   - The estimate is deliberately HIGH, so chunks come out smaller than
//     the configured window rather than larger. Erring the other way
//     trades a slightly-short chunk for a silently-clipped one.
//   - `token_count` is stored, so if the estimate is ever shown to be bad
//     the stored values are re-derivable without re-embedding.
//
// Measured against the real thing — Ollama reports `prompt_eval_count` for
// every embed call — by `services/ai/scripts/verify_token_estimate.py`,
// which is a script and not a test because `.claude/rules/testing.md`
// forbids CI from calling a model. Run it after changing anything here.

// charsPerToken is the divisor for Latin-script words.
//
// English BPE and WordPiece vocabularies average close to four characters
// per token on prose. Applied per word with a ceiling, which is what makes
// this overestimate: "the" and "of" are single tokens either way, while a
// long word is charged one token per four characters when a real vocab
// would have a single entry for a common stem like "interpretation".
const charsPerToken = 4

// EstimateTokens approximates the tokeniser's count for a string.
//
// Deterministic by construction — it is a pure fold over runes with no map
// iteration, no locale and no randomness. That matters more than accuracy
// here: an estimator that varied between runs would break the
// byte-identical chunking guarantee, and a chunk boundary that moves
// invalidates every embedding downstream of it.
func EstimateTokens(text string) int {
	total := 0

	// Length of the Latin-script run being accumulated, in characters
	// rather than bytes: a tokeniser counts neither, but characters are the
	// closer proxy and the byte count would charge every accented letter
	// double.
	runLength := 0

	flush := func() {
		if runLength == 0 {
			return
		}
		total += (runLength + charsPerToken - 1) / charsPerToken
		runLength = 0
	}

	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			// Whitespace is not itself a token — WordPiece folds it into
			// the word that follows. It only ends the current run.
			flush()

		case isLatinWordRune(r):
			runLength++

		case unicode.IsLetter(r) || unicode.IsMark(r):
			// Any other script: one token per rune.
			//
			// Devanagari is why this branch exists. Its tokens are far
			// shorter than four characters — often one or two — so the
			// Latin divisor would undercount a Hindi chunk by three or four
			// times and produce chunks well over the window. PHASE-05 §1
			// ships `en` only, which makes this branch untravelled today
			// and exactly the thing to get right before it is not: a
			// corpus the chunker silently mis-sizes is a corpus that has to
			// be re-embedded.
			flush()
			total++

		default:
			// Punctuation, symbols, emoji. Nearly always their own token,
			// and charging one each overestimates slightly where a vocab
			// merges ", " into a single entry.
			flush()
			total++
		}
	}

	flush()

	return total
}

// isLatinWordRune reports whether a rune belongs to a run that should be
// charged by length rather than per character.
//
// Digits are included: "10th" is one word to a tokeniser and splitting it
// into a word and a number would double-charge every house reference in
// the corpus.
func isLatinWordRune(r rune) bool {
	if r < unicode.MaxASCII {
		return r == '_' || r == '\'' ||
			('a' <= r && r <= 'z') ||
			('A' <= r && r <= 'Z') ||
			('0' <= r && r <= '9')
	}
	// Latin-1 and Latin Extended cover the accented forms that appear in
	// transliterated Sanskrit — "Viśākhā", "Bṛhat" — which are words, not
	// one token per letter.
	return unicode.Is(unicode.Latin, r) || unicode.IsDigit(r)
}
