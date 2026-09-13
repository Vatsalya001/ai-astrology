package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// newTestLogger builds a logger writing into a buffer, using the same
// redaction and context handling as production.
func newTestLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level:       slog.LevelDebug,
		ReplaceAttr: redactAttr,
	})
	return slog.New(contextHandler{Handler: handler}), &buf
}

// TestPIIIsRedacted is the load-bearing test in this package.
//
// Birth date, time and place are included deliberately: in combination
// they are close to a unique identifier for a person, which makes them
// PII in exactly the way an email address is.
func TestPIIIsRedacted(t *testing.T) {
	sensitive := []struct {
		key   string
		value string
	}{
		{"email", "someone@example.com"},
		{"phone", "+919876543210"},
		{"name", "A Person"},
		{"birth_date", "1994-08-17"},
		{"birth_time", "14:35"},
		{"birth_place", "Jaipur, Rajasthan"},
		{"latitude", "26.9124"},
		{"longitude", "75.7873"},
		{"password", "hunter2"},
		{"access_token", "eyJhbGciOi"},
		{"refresh_token", "r3fr35ht0k3n"},
		{"api_key", "sk-ant-secret"},
		{"authorization", "Bearer abc123"},
		{"otp", "482913"},
		{"webhook_secret", "whsec_live"},
	}

	for _, s := range sensitive {
		t.Run(s.key, func(t *testing.T) {
			logger, buf := newTestLogger()
			logger.Info("test event", slog.String(s.key, s.value))

			out := buf.String()
			if strings.Contains(out, s.value) {
				t.Errorf("%q leaked into the log output: %s", s.key, out)
			}
			if !strings.Contains(out, redacted) {
				t.Errorf("expected %s marker for key %q, got: %s", redacted, s.key, out)
			}
		})
	}
}

func TestRedactionIsCaseInsensitive(t *testing.T) {
	logger, buf := newTestLogger()
	logger.Info("test", slog.String("Email", "someone@example.com"))

	if strings.Contains(buf.String(), "someone@example.com") {
		t.Errorf("case-variant key was not redacted: %s", buf.String())
	}
}

func TestNonSensitiveFieldsSurvive(t *testing.T) {
	logger, buf := newTestLogger()
	logger.Info("test",
		slog.String("user_id", "0f3a-uuid"),
		slog.Int("status", 200),
		slog.String("path", "/api/v1/meta"),
	)

	out := buf.String()
	for _, want := range []string{"0f3a-uuid", "200", "/api/v1/meta"} {
		if !strings.Contains(out, want) {
			t.Errorf("non-sensitive value %q was dropped: %s", want, out)
		}
	}
}

func TestTraceIDIsAttachedFromContext(t *testing.T) {
	logger, buf := newTestLogger()
	ctx := WithTraceID(context.Background(), "trace-abc-123")

	logger.InfoContext(ctx, "test event")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("log output is not valid JSON: %v", err)
	}

	if record["trace_id"] != "trace-abc-123" {
		t.Errorf("trace_id = %v, want %q", record["trace_id"], "trace-abc-123")
	}
}

func TestNoTraceIDWhenContextHasNone(t *testing.T) {
	logger, buf := newTestLogger()
	logger.InfoContext(context.Background(), "test event")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("log output is not valid JSON: %v", err)
	}

	if _, present := record["trace_id"]; present {
		t.Error("trace_id present despite no ID on the context")
	}
}

func TestTraceIDFromHandlesNilContext(t *testing.T) {
	//nolint:staticcheck // deliberately passing nil to prove it is safe
	if got := TraceIDFrom(nil); got != "" {
		t.Errorf("TraceIDFrom(nil) = %q, want empty string", got)
	}
}

func TestRedactionSurvivesWithAttrsAndGroups(t *testing.T) {
	// A logger built with .With() must keep redacting — WithAttrs and
	// WithGroup have to rewrap the handler or the guarantee silently
	// disappears on any derived logger.
	logger, buf := newTestLogger()
	derived := logger.With(slog.String("component", "auth")).WithGroup("request")

	derived.Info("test", slog.String("email", "leak@example.com"))

	if strings.Contains(buf.String(), "leak@example.com") {
		t.Errorf("PII leaked through a derived logger: %s", buf.String())
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":    slog.LevelDebug,
		"info":     slog.LevelInfo,
		"warn":     slog.LevelWarn,
		"warning":  slog.LevelWarn,
		"error":    slog.LevelError,
		"DEBUG":    slog.LevelDebug,
		"nonsense": slog.LevelInfo, // unknown falls back to info
	}

	for input, want := range cases {
		if got := parseLevel(input); got != want {
			t.Errorf("parseLevel(%q) = %v, want %v", input, got, want)
		}
	}
}
