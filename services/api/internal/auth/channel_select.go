package auth

import (
	"fmt"
	"log/slog"
)

// ChannelConfig is the subset of application config this package needs.
//
// Declared here rather than importing internal/config: the consumer
// defines the interface it consumes, which keeps the dependency graph
// acyclic and lets a test construct one without a full Config.
type ChannelConfig struct {
	Channel  string
	IsProd   bool
	SMTPHost string
	SMTPPort int
	SMTPFrom string
}

// NewChannel selects the delivery channel from configuration.
//
// The production guard is duplicated from config.checkProductionInvariants
// on purpose. That one stops a misconfigured process at startup; this one
// stops a channel being constructed at all. Defence in depth is cheap
// here, and the failure it prevents — production logs full of live OTP
// codes — is not recoverable once it has happened, because the logs are
// already shipped and retained.
func NewChannel(cfg ChannelConfig, _ *slog.Logger) (Channel, error) {
	switch cfg.Channel {
	case "console":
		if cfg.IsProd {
			return nil, fmt.Errorf(
				"auth: refusing to build ConsoleChannel in production — it writes OTP codes to the log")
		}
		return &ConsoleChannel{}, nil

	case "smtp":
		if cfg.SMTPHost == "" {
			return nil, fmt.Errorf("auth: AUTH_CHANNEL=smtp but SMTP_HOST is empty")
		}
		return &SMTPChannel{
			Host: cfg.SMTPHost,
			Port: cfg.SMTPPort,
			From: cfg.SMTPFrom,
			// nil against Mailpit, which wants no credentials. A real
			// relay supplies smtp.PlainAuth here.
			Auth: nil,
		}, nil

	case "sms":
		// Deliberately unimplemented. SMS is the one channel with no free
		// tier, and the spec says to build email first because it
		// exercises the identical path. Returning a clear error beats a
		// stub that silently drops codes — a user who never receives one
		// cannot tell the difference from a broken account.
		return nil, fmt.Errorf(
			"auth: AUTH_CHANNEL=sms is not implemented yet; a gateway (MSG91/Twilio) is required")

	default:
		return nil, fmt.Errorf("auth: unknown AUTH_CHANNEL %q", cfg.Channel)
	}
}
