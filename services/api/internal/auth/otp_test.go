package auth

import (
	"strings"
	"testing"
)

func TestGenerateCodeShape(t *testing.T) {
	for _, length := range []int{4, 6, 8, 10} {
		code, err := generateCode(length)
		if err != nil {
			t.Fatalf("generateCode(%d): %v", length, err)
		}
		if len(code) != length {
			t.Errorf("got %d digits, want %d: %q", len(code), length, code)
		}
		for _, r := range code {
			if r < '0' || r > '9' {
				t.Errorf("code %q contains a non-digit %q — it must be enterable on a numeric keypad", code, r)
			}
		}
	}
}

// A length outside the sane range is a configuration mistake, and a
// 2-digit OTP is not a second factor. Refuse rather than comply.
func TestGenerateCodeRejectsAbsurdLengths(t *testing.T) {
	for _, length := range []int{-1, 0, 3, 11, 64} {
		if _, err := generateCode(length); err == nil {
			t.Errorf("generateCode(%d) succeeded; it must refuse", length)
		}
	}
}

// The property that makes an OTP a secret at all.
//
// Not a proof of randomness — that needs a statistical suite — but it
// catches the failure that actually happens: a constant, a counter, or a
// clock-seeded source returning the same value within a tick.
func TestGenerateCodeDoesNotRepeat(t *testing.T) {
	const draws = 2000

	seen := make(map[string]int, draws)
	for range draws {
		code, err := generateCode(6)
		if err != nil {
			t.Fatalf("generateCode: %v", err)
		}
		seen[code]++
	}

	// 2000 draws from 10^6 — expected collisions ≈ 2, and P(>15) is
	// vanishingly small. A generator stuck on one value scores 2000.
	if len(seen) < draws-15 {
		t.Errorf("only %d distinct codes in %d draws — the generator is not random",
			len(seen), draws)
	}
}

// Every digit position must vary. A generator that fixes the first digit
// silently divides the keyspace by ten, and the length check above would
// not notice.
func TestGenerateCodeVariesInEveryPosition(t *testing.T) {
	const length, draws = 6, 400

	distinct := make([]map[byte]bool, length)
	for i := range distinct {
		distinct[i] = map[byte]bool{}
	}

	for range draws {
		code, err := generateCode(length)
		if err != nil {
			t.Fatalf("generateCode: %v", err)
		}
		for i := range length {
			distinct[i][code[i]] = true
		}
	}

	for i, set := range distinct {
		if len(set) < 8 {
			t.Errorf("digit %d took only %d distinct values across %d codes — "+
				"that position looks fixed or heavily biased", i, len(set), draws)
		}
	}
}

func TestHashCodeIsStableAndNotReversible(t *testing.T) {
	const code = "123456"

	if hashCode(code) != hashCode(code) {
		t.Fatal("hashCode is not deterministic; verification could never succeed")
	}
	if hashCode(code) == hashCode("123457") {
		t.Error("two different codes hash identically")
	}
	if strings.Contains(hashCode(code), code) {
		t.Error("the hash contains the plaintext code")
	}
	if len(hashCode(code)) != 64 {
		t.Errorf("hash is %d hex chars, want 64 (sha256)", len(hashCode(code)))
	}
}

func TestCodesMatch(t *testing.T) {
	stored := hashCode("428917")

	if !codesMatch(stored, "428917") {
		t.Error("the correct code was rejected")
	}
	for _, wrong := range []string{"428918", "428916", "", "42891", "4289170", "428917 "} {
		if codesMatch(stored, wrong) {
			t.Errorf("incorrect code %q was accepted", wrong)
		}
	}
}

func TestRedisKeyNamespacesByChannel(t *testing.T) {
	// The same string can be a valid identifier on two channels. Without
	// the channel in the key, requesting an email code would overwrite an
	// in-flight phone code for the same string.
	if redisKey("email", "x") == redisKey("phone", "x") {
		t.Error("email and phone share a key; one channel can clobber the other")
	}
	if got, want := redisKey("email", "a@b.com"), "otp:email:a@b.com"; got != want {
		t.Errorf("redisKey = %q, want %q", got, want)
	}
}

func TestMaskIdentifier(t *testing.T) {
	cases := map[string]string{
		"user@example.com": "u***@example.com",
		"a@b.co":           "a***@b.co",
		"+919876543210":    "+91*******210",
		"1234567890":       "123****890",
		"":                 "",
		"short":            "*****",
	}

	for in, want := range cases {
		if got := maskIdentifier(in); got != want {
			t.Errorf("maskIdentifier(%q) = %q, want %q", in, got, want)
		}
	}
}

// The point of masking: the original must not be recoverable from the
// output, and the output must not be the input.
func TestMaskIdentifierAlwaysRemovesSomething(t *testing.T) {
	for _, id := range []string{
		"user@example.com",
		"+919876543210",
		"averylongemailaddress@somedomain.example",
		"9876543210",
	} {
		masked := maskIdentifier(id)
		if masked == id {
			t.Errorf("maskIdentifier(%q) returned the input unchanged", id)
		}
		if !strings.Contains(masked, "*") {
			t.Errorf("maskIdentifier(%q) = %q — nothing was masked", id, masked)
		}
	}
}

func TestNormaliseLocale(t *testing.T) {
	cases := map[string]string{
		"hi": "hi", "hi-IN": "hi", "hi_IN": "hi",
		"en": "en", "en-GB": "en", "EN": "en",
		"": "en",
	}
	for in, want := range cases {
		if got := normaliseLocale(in); got != want {
			t.Errorf("normaliseLocale(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOTPEmailCarriesTheCodeAndNothingElse(t *testing.T) {
	const code = "482913"

	for _, locale := range []string{"en", "hi", "hi-IN", "", "fr"} {
		subject, body := otpEmail(code, locale)

		if !strings.Contains(subject, code) && !strings.Contains(body, code) {
			t.Errorf("locale %q: the code appears in neither subject nor body", locale)
		}
		if !strings.Contains(body, code) {
			t.Errorf("locale %q: the body does not contain the code", locale)
		}
		if subject == "" || body == "" {
			t.Errorf("locale %q: empty subject or body", locale)
		}
	}

	// An unknown locale must fall back to English rather than sending an
	// empty message.
	frSubject, _ := otpEmail(code, "fr")
	enSubject, _ := otpEmail(code, "en")
	if frSubject != enSubject {
		t.Error("an unsupported locale did not fall back to English")
	}
}

// ─── Channel selection ───────────────────────────────────────────────

func TestNewChannelSelectsByConfig(t *testing.T) {
	console, err := NewChannel(ChannelConfig{Channel: "console"}, nil)
	if err != nil {
		t.Fatalf("console: %v", err)
	}
	if console.ID() != "console" {
		t.Errorf("ID() = %q, want console", console.ID())
	}

	smtpCh, err := NewChannel(ChannelConfig{
		Channel: "smtp", SMTPHost: "localhost", SMTPPort: 1025, SMTPFrom: "a@b",
	}, nil)
	if err != nil {
		t.Fatalf("smtp: %v", err)
	}
	if smtpCh.ID() != "smtp" {
		t.Errorf("ID() = %q, want smtp", smtpCh.ID())
	}
}

// The second layer of the same guard config.Load() enforces. Belt and
// braces, because production logs full of live OTP codes cannot be
// un-shipped once they exist.
func TestNewChannelRefusesConsoleInProduction(t *testing.T) {
	_, err := NewChannel(ChannelConfig{Channel: "console", IsProd: true}, nil)
	if err == nil {
		t.Fatal("NewChannel built a ConsoleChannel in production")
	}
	if !strings.Contains(err.Error(), "production") {
		t.Errorf("error does not explain why: %v", err)
	}
}

func TestNewChannelRejectsIncompleteAndUnknownConfig(t *testing.T) {
	cases := map[string]ChannelConfig{
		"smtp with no host": {Channel: "smtp", SMTPHost: ""},
		// Unimplemented must be an error, not a stub that drops codes —
		// a user who never receives one cannot tell that from a broken
		// account.
		"sms":     {Channel: "sms"},
		"unknown": {Channel: "carrier-pigeon"},
		"empty":   {Channel: ""},
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewChannel(cfg, nil); err == nil {
				t.Errorf("NewChannel accepted %+v", cfg)
			}
		})
	}
}

// SMTP's MAIL FROM takes a bare address; the From: header takes the
// display-name form. Conflating them is a 501 from Mailpit and from real
// relays, and it is invisible until something actually sends — which is
// how this was found.
func TestEnvelopeAddress(t *testing.T) {
	cases := map[string]string{
		"Ayana <noreply@localhost>":         "noreply@localhost",
		"noreply@localhost":                 "noreply@localhost",
		"  noreply@localhost  ":             "noreply@localhost",
		`"Ayana Support" <help@ayana.test>`: "help@ayana.test",
	}

	for in, want := range cases {
		got, err := envelopeAddress(in)
		if err != nil {
			t.Errorf("envelopeAddress(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("envelopeAddress(%q) = %q, want %q", in, got, want)
		}
		if strings.ContainsAny(got, "<>") {
			t.Errorf("envelopeAddress(%q) = %q still contains angle brackets — "+
				"SMTP will reject it with 501", in, got)
		}
	}
}

func TestEnvelopeAddressRejectsMalformed(t *testing.T) {
	for _, bad := range []string{"Ayana <not an address>", "<>", "A <"} {
		if _, err := envelopeAddress(bad); err == nil {
			t.Errorf("envelopeAddress(%q) accepted a malformed value", bad)
		}
	}
}
