package observability

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"
)

// InitSentry configures error reporting.
//
// An empty DSN disables it entirely and is not an error — development
// and CI run without a Sentry project, and requiring one would make the
// stack harder to start for no benefit.
//
// The BeforeSend hook is the load-bearing part. Sentry captures request
// context automatically, and this product's request context is unusually
// sensitive: birth details, conversation content, auth headers. An error
// reporter that faithfully uploads all of it is a PII leak with an
// enterprise support contract.
func InitSentry(cfg Config, dsn string) (Shutdown, error) {
	if dsn == "" {
		slog.Debug("sentry disabled: no DSN configured")
		return func(context.Context) error { return nil }, nil
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:         dsn,
		Environment: cfg.Environment,
		Release:     fmt.Sprintf("%s@%s", cfg.ServiceName, cfg.Version),
		ServerName:  cfg.ServiceName,

		// Never send the request body. It can contain birth details in
		// Phase 2 and conversation content in Phase 5.
		SendDefaultPII: false,

		// Traces go to OpenTelemetry, not Sentry. One tracing backend.
		EnableTracing: false,

		BeforeSend: scrub,
	})
	if err != nil {
		return nil, fmt.Errorf("init sentry: %w", err)
	}

	return func(context.Context) error {
		// Sentry's flush is synchronous and takes its own timeout.
		if !sentry.Flush(5 * time.Second) {
			return fmt.Errorf("sentry: flush timed out, some events were dropped")
		}
		return nil
	}, nil
}

// sensitiveHeaders are stripped from every event.
//
// Deliberately broader than "obviously a credential": a trace header is
// attacker-controlled, and a cookie carries the session.
var sensitiveHeaders = []string{
	"Authorization",
	"Cookie",
	"Set-Cookie",
	"X-Internal-Token",
	"Idempotency-Key",
	"Proxy-Authorization",
}

// scrub removes request data that must never leave the process.
//
// Belt and braces alongside SendDefaultPII: that flag governs what the
// SDK collects by default, while this governs what actually leaves.
func scrub(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if event.Request != nil {
		for _, h := range sensitiveHeaders {
			delete(event.Request.Headers, h)
		}
		// A query string can carry an OTP or an email in a badly-built
		// link. Drop it rather than try to parse out the safe parts.
		event.Request.QueryString = ""
		event.Request.Data = ""
		event.Request.Cookies = ""
	}

	// The user object is an ID and nothing else. Sentry's UI will happily
	// display an email if given one.
	if event.User.Email != "" || event.User.Username != "" || event.User.Name != "" {
		event.User = sentry.User{ID: event.User.ID}
	}

	return event
}
