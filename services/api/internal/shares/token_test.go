package shares

import (
	"encoding/base64"
	"strings"
	"testing"
)

// The token, which is a bearer credential that will end up in a group
// chat. Every property below is one that failing would make that fact
// dangerous rather than merely untidy.

func TestATokenIsUnpredictable(t *testing.T) {
	const rounds = 2000

	seen := make(map[string]struct{}, rounds)
	for range rounds {
		token, _, err := MintToken()
		if err != nil {
			t.Fatalf("MintToken: %v", err)
		}
		if _, dup := seen[token]; dup {
			t.Fatalf("MintToken repeated %q within %d draws", token, rounds)
		}
		seen[token] = struct{}{}
	}
}

/*
The token carries the entropy it claims to.

Asserted on the DECODED length, not on the string's. A base64 string
of the right length can be made from far fewer random bytes — padding,
a shortened draw, a hex string that happens to fit — and the character
count would not notice. 22 characters is what 16 bytes produces; what
matters is that 16 bytes went in.
*/
func TestATokenCarries128BitsOfEntropy(t *testing.T) {
	token, _, err := MintToken()
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("token %q is not base64url: %v", token, err)
	}
	if len(raw) != TokenBytes {
		t.Fatalf("token decodes to %d bytes, want %d — %d bits is not enough to "+
			"make guessing hopeless for a credential that ends up in a group chat",
			len(raw), TokenBytes, len(raw)*8)
	}
}

// URL-safe, because it goes in a path segment. A '+' or a '/' would be
// re-encoded by some clients and not by others, and a link that works on
// one phone and not another is worse than one that never works.
func TestATokenIsURLSafe(t *testing.T) {
	for range 200 {
		token, _, err := MintToken()
		if err != nil {
			t.Fatalf("MintToken: %v", err)
		}
		if strings.ContainsAny(token, "+/=?&#% ") {
			t.Fatalf("token %q contains a character that a URL will mangle", token)
		}
	}
}

/*
Mint returns the hash of the token it returned, and nothing else.

The pairing is the whole point of returning both together: a caller
that could get a token without its hash would be free to store the
plaintext, which is the mistake this file exists to prevent.
*/
func TestMintReturnsTheHashOfItsOwnToken(t *testing.T) {
	token, hash, err := MintToken()
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	if hash != HashToken(token) {
		t.Fatal("the returned hash is not the hash of the returned token; the stored " +
			"row and the link in the user's hand would not correspond")
	}
	if strings.Contains(hash, token) {
		t.Fatalf("the hash contains the plaintext token: %q", hash)
	}
	if len(hash) != 64 {
		t.Fatalf("hash is %d characters, want 64 — the column's CHECK constraint "+
			"would reject it and every share creation would fail", len(hash))
	}
}

// Two different tokens never hash alike, and the same token always does.
func TestHashingIsDeterministicAndDistinct(t *testing.T) {
	a, aHash, _ := MintToken()
	b, bHash, _ := MintToken()

	if HashToken(a) != aHash || HashToken(b) != bHash {
		t.Fatal("hashing the same token twice produced different results")
	}
	if aHash == bHash {
		t.Fatalf("two distinct tokens hashed alike: %q and %q", a, b)
	}
}

/*
Punctuation a human adds is repaired; the token itself is not.

People send these by hand. "Here you go: <link>." arrives with a full
stop attached, and a link that 404s for that reads to the recipient as
"the owner turned this off" — which is the one message it must not
send by accident.

What must NOT happen is rewriting the token's body. A token that is
wrong stays wrong; only the wrapper comes off.
*/
func TestNormaliseRepairsWrappingButNotTheToken(t *testing.T) {
	/*
	   Deliberately self-describing rather than random-looking.

	   The pre-commit secret scanner flagged the previous value as a
	   generic API key, and its own advice is the right one: make a test
	   value obviously fake instead of adding an allowlist entry, because
	   an allowlist blunts the scanner for real secrets too.

	   Still 22 characters of the base64url alphabet, which is the shape
	   this function actually has to handle.
	*/
	const token = "example-not-a-real-tok"

	for name, given := range map[string]string{
		"clean":          token,
		"trailing space": token + " ",
		"leading space":  "  " + token,
		"full stop":      token + ".",
		"comma":          token + ",",
		"in brackets":    token + ")",
		"quoted":         token + `"`,
		"sentence end":   "\t" + token + ".\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got := NormaliseToken(given); got != token {
				t.Fatalf("NormaliseToken(%q) = %q, want %q", given, got, token)
			}
		})
	}

	// And a token damaged in its body is left damaged — repairing it
	// would mean accepting tokens nobody issued.
	for _, damaged := range []string{
		"example-not-a-real-to", // one character short
		"Example-not-a-real-tok",
		"example-not-a-real-toX",
	} {
		if got := NormaliseToken(damaged); got == token {
			t.Fatalf("NormaliseToken(%q) produced the VALID token %q — a damaged "+
				"token must stay damaged, or this function is inventing credentials",
				damaged, token)
		}
	}
}

func TestAnEmptyTokenNormalisesToEmpty(t *testing.T) {
	for _, given := range []string{"", "   ", "...", "\t\n"} {
		if got := NormaliseToken(given); got != "" {
			t.Fatalf("NormaliseToken(%q) = %q, want empty", given, got)
		}
	}
}
