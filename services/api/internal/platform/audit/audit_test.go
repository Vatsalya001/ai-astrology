package audit

import (
	"encoding/json"
	"strings"
	"testing"
)

// An audit trail outlives everything else in the system and is read by
// people who never saw this code. Once an email address is in one, it is
// effectively permanent — so the allowlist is the guard, and this is the
// test that it actually refuses.
func TestSanitiseDropsAnythingNotOnTheAllowlist(t *testing.T) {
	in := map[string]any{
		"channel": "email",     // allowed
		"reason":  "incorrect", // allowed
		// Everything below is the kind of thing that gets added "just for
		// debugging" and then ships.
		"email":      "user@example.com",
		"phone":      "+919876543210",
		"identifier": "user@example.com",
		"name":       "Priya",
		"code":       "482913",
		"ip":         "203.0.113.7",
		"birth_date": "1990-01-01",
	}

	clean, dropped := sanitise(in)

	for _, forbidden := range []string{"email", "phone", "identifier", "name", "code", "ip", "birth_date"} {
		if _, present := clean[forbidden]; present {
			t.Errorf("%q survived sanitisation", forbidden)
		}
	}
	if clean["channel"] != "email" {
		t.Error("an allowed key was dropped")
	}
	if clean["reason"] != "incorrect" {
		t.Error("an allowed key was dropped")
	}
	if len(dropped) != 7 {
		t.Errorf("%d keys reported as dropped, want 7: %v", len(dropped), dropped)
	}
}

// A nested object could smuggle PII under an allowed key —
// {"reason": {"email": "..."}} passes a key check and defeats the point.
func TestSanitiseRejectsNonScalarValues(t *testing.T) {
	clean, dropped := sanitise(map[string]any{
		"reason":  map[string]any{"email": "user@example.com"},
		"channel": []string{"email", "user@example.com"},
		"outcome": "ok",
	})

	if _, present := clean["reason"]; present {
		t.Error("a nested object survived under an allowed key")
	}
	if _, present := clean["channel"]; present {
		t.Error("a slice survived under an allowed key")
	}
	if clean["outcome"] != "ok" {
		t.Error("a scalar under an allowed key was dropped")
	}
	if len(dropped) != 2 {
		t.Errorf("dropped = %v, want 2 entries", dropped)
	}
}

func TestSanitiseAcceptsEveryScalarKind(t *testing.T) {
	clean, dropped := sanitise(map[string]any{
		"count":       3,
		"grace_hours": 168,
		"outcome":     true,
		"reason":      "none",
		"role":        nil,
	})

	if len(dropped) != 0 {
		t.Errorf("scalars were dropped: %v", dropped)
	}
	if len(clean) != 5 {
		t.Errorf("%d keys survived, want 5", len(clean))
	}
}

// The dropped-key report must name the KEY and never the value —
// logging the value would leak the very thing that was just refused.
func TestDroppedReportCarriesKeysNotValues(t *testing.T) {
	_, dropped := sanitise(map[string]any{"email": "secret@example.com"})

	joined := strings.Join(dropped, ",")
	if !strings.Contains(joined, "email") {
		t.Errorf("the dropped report does not name the key: %v", dropped)
	}
	if strings.Contains(joined, "secret@example.com") {
		t.Errorf("the dropped report leaked the value it refused: %v", dropped)
	}
}

// The column is NOT NULL DEFAULT '{}'. A JSON null would read as
// "unknown" rather than "nothing to say".
func TestEmptyMetadataMarshalsToAnEmptyObject(t *testing.T) {
	for _, in := range []map[string]any{nil, {}} {
		got, err := marshalMetadata(in)
		if err != nil {
			t.Fatalf("marshalMetadata: %v", err)
		}
		if string(got) != "{}" {
			t.Errorf("marshalMetadata(%v) = %s, want {}", in, got)
		}
	}
}

func TestMarshalProducesValidJSON(t *testing.T) {
	got, err := marshalMetadata(map[string]any{"channel": "email", "count": 2})
	if err != nil {
		t.Fatalf("marshalMetadata: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatalf("the output is not valid JSON: %v", err)
	}
	if back["channel"] != "email" {
		t.Errorf("round trip lost data: %v", back)
	}
}
