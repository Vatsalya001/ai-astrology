// Package analytics emits product events.
//
// The one rule, the same as audit: IDs, enums and counts. Never PII.
//
// Analytics is where PII leaks in practice, more reliably than logs. A
// log line is read by an engineer and thrown away; an analytics event is
// shipped to a third party, fanned out to a warehouse, joined against
// other datasets and retained for years. "Just the email so we can
// segment" is how a product ends up with user addresses in four vendors'
// databases and no idea which.
//
// So the vocabulary is closed on both axes. Event names are constants —
// an unknown name is refused rather than silently creating a new funnel
// that nobody is looking at. Property keys are an allowlist, checked at
// emit. A denylist passes the first time somebody adds a key nobody
// thought to forbid.
package analytics

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
)

// The event vocabulary, from the Phase 1 spec §12.
//
// Constants rather than strings at call sites: a typo'd event name is
// not an error anywhere, it is a funnel that quietly reports zero and a
// dashboard that is wrong for a quarter before anyone notices.
const (
	SignupStarted            = "signup_started"
	OTPRequested             = "otp_requested"
	OTPVerified              = "otp_verified"
	SignupCompleted          = "signup_completed"
	LoginCompleted           = "login_completed"
	OnboardingNameCompleted  = "onboarding_name_completed"
	PreferencesUpdated       = "preferences_updated"
	SessionRevoked           = "session_revoked"
	AccountDeletionRequested = "account_deletion_requested"
	AccountDeleted           = "account_deleted"

	// Phase 2, from PHASE-02 §15.
	//
	// No dates, times, place names or coordinates in any payload. Birth
	// date + time + place is close to a unique identifier, so the same
	// rule the logger follows applies here — enums and IDs only, and the
	// property allowlist below enforces it rather than trusting each
	// call site.
	BirthProfileStarted       = "birth_profile_started"
	BirthProfileStepCompleted = "birth_profile_step_completed"
	BirthTimeUnknownSelected  = "birth_time_unknown_selected"
	PlaceSearchPerformed      = "place_search_performed"
	PlaceSelectedViaMap       = "place_selected_via_map"
	BirthProfileCreated       = "birth_profile_created"
	ChartGenerated            = "chart_generated"
	ChartGenerationFailed     = "chart_generation_failed"
	BirthProfileEdited        = "birth_profile_edited"
)

// knownEvents is the closed set. Adding one is a deliberate edit here,
// which is the point — it is the moment to ask what the event is for.
var knownEvents = map[string]bool{
	SignupStarted:            true,
	OTPRequested:             true,
	OTPVerified:              true,
	SignupCompleted:          true,
	LoginCompleted:           true,
	OnboardingNameCompleted:  true,
	PreferencesUpdated:       true,
	SessionRevoked:           true,
	AccountDeletionRequested: true,
	AccountDeleted:           true,

	BirthProfileStarted:       true,
	BirthProfileStepCompleted: true,
	BirthTimeUnknownSelected:  true,
	PlaceSearchPerformed:      true,
	PlaceSelectedViaMap:       true,
	BirthProfileCreated:       true,
	ChartGenerated:            true,
	ChartGenerationFailed:     true,
	BirthProfileEdited:        true,
}

// allowedProperties is what may travel with an event.
//
// Every one of these is an ID, an enum or a count. There is deliberately
// no `email`, `phone`, `name`, `identifier`, `ip` or `birth_date` — and
// no `metadata` escape hatch, because an escape hatch is how the rule
// stops applying.
var allowedProperties = map[string]bool{
	"channel":  true, // email | phone | google | apple
	"attempts": true, // a count
	"fields":   true, // which preference keys changed, never their values
	"provider": true,
	"outcome":  true, // a short enum
	"reason":   true, // a short enum, never free text
	"count":    true,
	"role":     true,
	"is_new":   true,

	// Phase 2. Every one of these is a count, an enum or a duration.
	//
	// Deliberately absent, and worth naming so the omission reads as a
	// decision: no `birth_date`, `birth_time`, `place`, `latitude`,
	// `longitude` or `timezone`. Those are the fields that would make an
	// analytics row identify a person.
	"step":        true, // which onboarding step, 1-3
	"duration_ms": true,
	"chart_type":  true, // D1 | D9 | D10
	"error_code":  true, // a short enum, never a message
	"cached":      true,
}

// Emitter records a product event.
//
// An interface so the sink is a deployment decision rather than a code
// one. Phase 1 ships the log emitter; a vendor arrives behind this
// signature and every call site is unchanged.
type Emitter interface {
	// Emit records one event for one user. A nil userID is legitimate:
	// otp_requested happens before anybody is identified, and attaching
	// the identifier they typed is exactly what must not happen.
	Emit(ctx context.Context, event string, userID *uuid.UUID, props map[string]any)
}

// LogEmitter writes events as structured JSON on stdout.
//
// Not a placeholder. The service already logs structured JSON that a
// pipeline consumes, so this needs no vendor, no SDK, no key and no ADR,
// and it is a real sink from the first deploy. Swapping it for a vendor
// later is a change in main.go.
type LogEmitter struct {
	logger *slog.Logger
}

func NewLogEmitter(logger *slog.Logger) *LogEmitter {
	return &LogEmitter{logger: logger}
}

func (e *LogEmitter) Emit(ctx context.Context, event string, userID *uuid.UUID, props map[string]any) {
	if !knownEvents[event] {
		// Loud, and the event is dropped. A name that is not in the
		// vocabulary is a bug — either a typo or an event somebody added
		// without deciding what it means.
		e.logger.ErrorContext(ctx, "analytics: unknown event refused",
			slog.String("event", event))
		return
	}

	attrs := []any{slog.String("event", event)}
	if userID != nil {
		attrs = append(attrs, slog.String("user_id", userID.String()))
	}

	for key, value := range props {
		if !allowedProperties[key] {
			// Refuse the PROPERTY, not the event. Dropping the whole event
			// would lose a real measurement over one bad key; dropping the
			// key keeps the funnel intact and still cannot leak.
			e.logger.ErrorContext(ctx, "analytics: property refused — not on the allowlist",
				slog.String("event", event),
				slog.String("property", key),
			)
			continue
		}
		attrs = append(attrs, slog.Any(key, value))
	}

	e.logger.InfoContext(ctx, "analytics", attrs...)
}

// Nop discards everything.
//
// For tests and for any context where emitting would be noise. Named
// rather than nil so call sites never need a nil check — a missing nil
// check on an optional dependency is a panic waiting for the one code
// path nobody exercised.
type Nop struct{}

func (Nop) Emit(context.Context, string, *uuid.UUID, map[string]any) {}
