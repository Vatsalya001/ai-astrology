package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The guard that matters: PII must not reach the sink.
//
// Analytics leaks PII more reliably than logs do. A log line is read by
// an engineer and thrown away; an analytics event is shipped to a third
// party, fanned out to a warehouse and retained for years.

func capture(t *testing.T) (*LogEmitter, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return NewLogEmitter(slog.New(slog.NewJSONHandler(&buf, nil))), &buf
}

func TestPropertiesOffTheAllowlistAreRefused(t *testing.T) {
	// Every one of these is something a well-meaning person would add to
	// "improve segmentation".
	leaks := map[string]any{
		"email":      "person@example.com",
		"phone":      "+919876543210",
		"name":       "Priya",
		"identifier": "person@example.com",
		"ip":         "203.0.113.7",
		"birth_date": "1994-03-21",
		"address":    "12 Example Road",
		"metadata":   map[string]any{"email": "person@example.com"},
	}

	for key, value := range leaks {
		t.Run(key, func(t *testing.T) {
			emitter, buf := capture(t)
			emitter.Emit(context.Background(), OTPRequested, nil, map[string]any{key: value})

			out := buf.String()
			if strings.Contains(out, "person@example.com") ||
				strings.Contains(out, "+919876543210") ||
				strings.Contains(out, "Priya") ||
				strings.Contains(out, "203.0.113.7") ||
				strings.Contains(out, "1994-03-21") ||
				strings.Contains(out, "12 Example Road") {
				t.Fatalf("PII reached the sink via %q:\n%s", key, out)
			}
			if !strings.Contains(out, "property refused") {
				t.Errorf("the refusal was silent; nobody would ever find out:\n%s", out)
			}
		})
	}
}

// A refused property must not take the event with it. Losing a real
// measurement over one bad key is a worse trade than dropping the key.
func TestARefusedPropertyKeepsTheEvent(t *testing.T) {
	emitter, buf := capture(t)

	emitter.Emit(context.Background(), OTPVerified, nil, map[string]any{
		"channel":  "email",
		"attempts": 2,
		"email":    "person@example.com",
	})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var event map[string]any
	for _, line := range lines {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Fatalf("decode %q: %v", line, err)
		}
		if parsed["msg"] == "analytics" {
			event = parsed
		}
	}

	if event == nil {
		t.Fatal("the event was dropped along with the bad property")
	}
	if event["channel"] != "email" {
		t.Errorf("channel = %v, want email", event["channel"])
	}
	if event["attempts"] != float64(2) {
		t.Errorf("attempts = %v, want 2", event["attempts"])
	}
	if _, present := event["email"]; present {
		t.Error("the email survived")
	}
}

// A typo'd event name is not an error anywhere. It is a funnel that
// quietly reports zero and a dashboard that is wrong for a quarter.
func TestUnknownEventsAreRefused(t *testing.T) {
	emitter, buf := capture(t)

	emitter.Emit(context.Background(), "login_complete", nil, nil) // missing the 'd'

	out := buf.String()
	if strings.Contains(out, `"msg":"analytics"`) {
		t.Errorf("an unknown event was emitted:\n%s", out)
	}
	if !strings.Contains(out, "unknown event refused") {
		t.Errorf("the refusal was silent:\n%s", out)
	}
}

// Every name in the spec's §12 list must be accepted, or a call site
// wired correctly would still emit nothing.
func TestTheWholeSpecVocabularyIsAccepted(t *testing.T) {
	spec := []string{
		"signup_started", "otp_requested", "otp_verified", "signup_completed",
		"login_completed", "onboarding_name_completed", "preferences_updated",
		"session_revoked", "account_deletion_requested", "account_deleted",
	}

	for _, name := range spec {
		emitter, buf := capture(t)
		emitter.Emit(context.Background(), name, nil, nil)

		if !strings.Contains(buf.String(), `"event":"`+name+`"`) {
			t.Errorf("%q is in the spec but not in knownEvents", name)
		}
	}

	if len(knownEvents) != len(spec) {
		t.Errorf("knownEvents has %d entries, the spec lists %d — one of them has drifted",
			len(knownEvents), len(spec))
	}
}

// otp_requested happens before anybody is identified. A nil user is the
// correct representation of that; the alternative is attaching the
// identifier they typed, which is the exact thing that must not happen.
func TestANilUserEmitsNoUserField(t *testing.T) {
	emitter, buf := capture(t)
	emitter.Emit(context.Background(), OTPRequested, nil, map[string]any{"channel": "email"})

	if strings.Contains(buf.String(), "user_id") {
		t.Errorf("a user_id appeared for an anonymous event:\n%s", buf.String())
	}
}

func TestAKnownUserIsRecordedAsAnID(t *testing.T) {
	emitter, buf := capture(t)
	id := uuid.New()

	emitter.Emit(context.Background(), LoginCompleted, &id, map[string]any{"channel": "email"})

	if !strings.Contains(buf.String(), id.String()) {
		t.Errorf("the user id is missing:\n%s", buf.String())
	}
}

// Nop exists so call sites never need a nil check. A missing nil check
// on an optional dependency is a panic waiting for the one code path
// nobody exercised.
func TestNopIsSafe(t *testing.T) {
	var emitter Emitter = Nop{}
	emitter.Emit(context.Background(), LoginCompleted, nil, map[string]any{"channel": "email"})
}
