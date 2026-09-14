package auth

import (
	"context"
	"fmt"
	"io"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
)

// ConsoleChannel prints the code to stderr.
//
// Development only. `checkProductionInvariants` refuses to start with
// AUTH_CHANNEL=console when ENV=production, because a production log
// containing live credentials is readable by everyone with log access
// and retained for as long as logs are.
//
// Deliberately NOT written through slog. The project's logger redacts
// `code` as a sensitive key — correctly, since an OTP is a secret — so
// routing this through it printed `"code":"[REDACTED]"` and made the
// development channel useless. The fix is to bypass the logger, not to
// exempt the key: an exemption would blunt the redactor for every other
// call site, including production ones.
//
// This is a developer affordance, not a log line, and writing it as
// obviously-unstructured text says so.
type ConsoleChannel struct {
	// Out defaults to os.Stderr. Injectable for tests.
	Out io.Writer
}

func (c *ConsoleChannel) ID() string { return "console" }

func (c *ConsoleChannel) Send(_ context.Context, identifier, code, _ string) error {
	out := c.Out
	if out == nil {
		out = os.Stderr
	}

	// The identifier is masked even here. A developer's terminal is not
	// production, but dev output gets pasted into issues and shared in
	// screenshots, and an email address is PII wherever it lands. The
	// code is the only part that needs to be readable.
	_, err := fmt.Fprintf(out, "\n  ── OTP for %s: %s  (expires in 5 minutes) ──\n\n",
		maskIdentifier(identifier), code)
	if err != nil {
		return fmt.Errorf("auth: write console otp: %w", err)
	}
	return nil
}

// SMTPChannel delivers to Mailpit in development and to a real relay in
// production.
//
// Mailpit accepts anything on :1025 and sends nothing onward, so the
// full email path is exercised locally without a provider account and
// without the risk of mailing a real person during a test.
type SMTPChannel struct {
	Host string
	Port int
	From string
	// Auth is nil against Mailpit, which wants no credentials. A real
	// relay supplies smtp.PlainAuth.
	Auth smtp.Auth
}

func (c *SMTPChannel) ID() string { return "smtp" }

func (c *SMTPChannel) Send(ctx context.Context, identifier, code, locale string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	subject, body := otpEmail(code, locale)

	// CRLF line endings and a blank line between headers and body are
	// required by RFC 5322. LF alone is accepted by Mailpit and rejected
	// by several real relays, which is a fault that only appears in
	// production.
	msg := strings.Join([]string{
		"From: " + c.From,
		"To: " + identifier,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")

	// The envelope sender and the From: header are not the same thing.
	// SMTP's MAIL FROM takes a bare address; passing the display-name
	// form gets a 501 "invalid FROM parameter" from Mailpit and from
	// real relays alike. The header keeps the friendly form.
	envelopeFrom, err := envelopeAddress(c.From)
	if err != nil {
		return err
	}

	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
	if err := smtp.SendMail(addr, c.Auth, envelopeFrom, []string{identifier}, []byte(msg)); err != nil {
		// The recipient is not in the error. A delivery failure is
		// logged, and a log line carrying an email address is the exact
		// thing the PII rules forbid.
		return fmt.Errorf("auth: send via smtp %s: %w", addr, err)
	}
	return nil
}

// envelopeAddress extracts the bare address from a From value.
//
//	"Ayana <noreply@localhost>" → "noreply@localhost"
//	"noreply@localhost"         → "noreply@localhost"
//
// Both forms are legitimate in SMTP_FROM, and operators write the first
// one because it is what recipients see.
func envelopeAddress(from string) (string, error) {
	if !strings.Contains(from, "<") {
		return strings.TrimSpace(from), nil
	}
	parsed, err := mail.ParseAddress(from)
	if err != nil {
		return "", fmt.Errorf("auth: SMTP_FROM %q is not a valid address: %w", from, err)
	}
	return parsed.Address, nil
}

// otpEmail returns the subject and body for a locale.
//
// Deliberately plain text and deliberately short. The three things that
// matter are the code, how long it lasts, and what to do if it was not
// requested — everything else is noise in a message someone reads for
// four seconds. Phase 1 ships en and hi; §1.14 wires the rest.
func otpEmail(code, locale string) (subject, body string) {
	switch normaliseLocale(locale) {
	case "hi":
		return "आपका Ayana कोड: " + code,
			"आपका सत्यापन कोड " + code + " है।\n\n" +
				"यह 5 मिनट में समाप्त हो जाएगा।\n\n" +
				"यदि आपने यह अनुरोध नहीं किया है, तो इस ईमेल को अनदेखा करें।"
	default:
		return "Your Ayana code: " + code,
			"Your verification code is " + code + ".\n\n" +
				"It expires in 5 minutes.\n\n" +
				"If you didn't request this, you can ignore this email — " +
				"someone may have mistyped their address."
	}
}

func normaliseLocale(locale string) string {
	if locale == "" {
		return "en"
	}
	// "hi-IN" and "hi" are the same language for our purposes.
	if i := strings.IndexAny(locale, "-_"); i > 0 {
		locale = locale[:i]
	}
	return strings.ToLower(locale)
}

// maskIdentifier renders an email or phone safe to log.
//
//	user@example.com → u***@example.com
//	+919876543210    → +91*******210
//
// The domain and the country code survive because they are useful for
// debugging and are not identifying on their own. Enough of the local
// part is removed that the result cannot be matched back to a person.
func maskIdentifier(identifier string) string {
	if identifier == "" {
		return ""
	}

	if at := strings.LastIndex(identifier, "@"); at > 0 {
		return identifier[:1] + "***" + identifier[at:]
	}

	// Phone. Keep a short prefix and the last three digits.
	const keepHead, keepTail = 3, 3
	if len(identifier) <= keepHead+keepTail {
		return strings.Repeat("*", len(identifier))
	}
	return identifier[:keepHead] +
		strings.Repeat("*", len(identifier)-keepHead-keepTail) +
		identifier[len(identifier)-keepTail:]
}
