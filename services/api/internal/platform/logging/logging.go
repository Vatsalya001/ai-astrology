// Package logging configures structured JSON logging for api-service.
//
// Two things matter here and both are easy to get wrong:
//
//  1. PII redaction happens at the logger, not at the call site.
//     Relying on every developer to remember not to log an email is
//     how emails end up in production logs.
//
//  2. Every line carries a trace_id, pulled from the request context.
//     A request in this system crosses three processes; without a
//     correlating ID the logs are unreadable.
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// sensitiveKeys are redacted wherever they appear as an attribute key,
// at any nesting depth.
//
// Birth date, time and place are on this list deliberately: in
// combination they are close to a unique identifier for a person, which
// makes them PII in exactly the way an email address is.
var sensitiveKeys = map[string]struct{}{
	"email":          {},
	"phone":          {},
	"name":           {},
	"display_name":   {},
	"address":        {},
	"date_of_birth":  {},
	"birth_date":     {},
	"time_of_birth":  {},
	"birth_time":     {},
	"place_of_birth": {},
	"birth_place":    {},
	"latitude":       {},
	"longitude":      {},
	"password":       {},
	"token":          {},
	"access_token":   {},
	"refresh_token":  {},
	"api_key":        {},
	"secret":         {},
	"authorization":  {},
	"key_secret":     {},
	"webhook_secret": {},
	"signature":      {},
	"otp":            {},
	"code":           {},
}

const redacted = "[REDACTED]"

// redactAttr is slog's ReplaceAttr hook.
//
// Limitation worth knowing: slog does not descend into arbitrary struct
// values passed via slog.Any. Log scalars, or implement slog.LogValuer on
// your type to control its own representation. Phase 1 adds a lint rule
// for this; until then, the convention is "log IDs, not objects".
func redactAttr(_ []string, a slog.Attr) slog.Attr {
	if _, found := sensitiveKeys[strings.ToLower(a.Key)]; found {
		return slog.String(a.Key, redacted)
	}
	return a
}

// contextHandler decorates every record with values carried on the
// context — currently the trace ID.
type contextHandler struct {
	slog.Handler
}

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := TraceIDFrom(ctx); id != "" {
		r.AddAttrs(slog.String("trace_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{Handler: h.Handler.WithGroup(name)}
}

// New builds the application logger and installs it as the slog default.
//
// service is included on every line so that when all three services ship
// logs to the same place, you can tell them apart.
func New(level, service string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:       parseLevel(level),
		ReplaceAttr: redactAttr,
	})

	logger := slog.New(contextHandler{Handler: handler}).
		With(slog.String("service", service))

	slog.SetDefault(logger)
	return logger
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
