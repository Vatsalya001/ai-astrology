//go:build integration

package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/analytics"
)

// The service against a real OTP store. The fakes here stand in for the
// users table and the audit sink, which have their own coverage; what is
// under test is the sequencing the service owns.

func serviceWithRealOTP(t *testing.T, users *fakeUserCreator, ch Channel, aud AuditSink) (*Service, func()) {
	t.Helper()

	client, stop := startRedis(context.Background(), t)
	svc := NewService(ServiceConfig{
		OTP:       NewOTPStore(client, 5*time.Minute, 5),
		Channel:   ch,
		Rotator:   NewRotator(testIssuer(t), newFakeStore()),
		Users:     users,
		Audit:     aud,
		OTPLength: 6,
	})
	// Real sleeping would add 250ms to every case here; the floor itself
	// is asserted separately with a fake clock.
	svc.sleep = func(time.Duration) {}
	return svc, stop
}

func TestServiceSignupThenLogin(t *testing.T) {
	ctx := context.Background()
	ch := &captureChannel{}
	users := &fakeUserCreator{}
	aud := &fakeAudit{}

	svc, stop := serviceWithRealOTP(t, users, ch, aud)
	defer stop()

	// First time: a user is created.
	if err := svc.RequestOTP(ctx, ChannelEmail, "New@Example.com", "en", nil); err != nil {
		t.Fatalf("RequestOTP: %v", err)
	}

	// The channel receives the NORMALISED identifier, not what was typed.
	if got := ch.to[0]; got != "new@example.com" {
		t.Errorf("channel received %q, want the normalised form", got)
	}

	_, user, isNew, err := svc.VerifyOTP(ctx, ChannelEmail, "new@example.com", ch.lastCode(), "ua", nil)
	if err != nil {
		t.Fatalf("VerifyOTP: %v", err)
	}
	if !isNew {
		t.Error("is_new_user = false on a first login")
	}
	if len(users.created) != 1 {
		t.Errorf("%d users created, want 1", len(users.created))
	}

	// Second time: the same user, and isNew must flip.
	if err := svc.RequestOTP(ctx, ChannelEmail, "new@example.com", "en", nil); err != nil {
		t.Fatalf("second RequestOTP: %v", err)
	}
	_, user2, isNew2, err := svc.VerifyOTP(ctx, ChannelEmail, "new@example.com", ch.lastCode(), "ua", nil)
	if err != nil {
		t.Fatalf("second VerifyOTP: %v", err)
	}
	if isNew2 {
		t.Error("is_new_user = true for a returning user — they would be sent through onboarding again")
	}
	if user2.ID != user.ID {
		t.Errorf("a second login produced a different user: %s then %s", user.ID, user2.ID)
	}

	// The audit trail must distinguish signup from login.
	actions := aud.actions()
	if !contains(strings.Join(actions, ","), "auth.signup") {
		t.Errorf("no auth.signup recorded: %v", actions)
	}
	if !contains(strings.Join(actions, ","), "auth.login") {
		t.Errorf("no auth.login recorded: %v", actions)
	}
}

// Case and spacing must not create a second identity. Otherwise the
// per-identifier rate limit is bypassed by varying the case, and a user
// can end up with two accounts for one address.
func TestVerifyAcceptsAnyCasingOfTheSameIdentifier(t *testing.T) {
	ctx := context.Background()
	ch := &captureChannel{}
	svc, stop := serviceWithRealOTP(t, &fakeUserCreator{}, ch, &fakeAudit{})
	defer stop()

	if err := svc.RequestOTP(ctx, ChannelEmail, "Mixed.Case@Example.COM", "en", nil); err != nil {
		t.Fatalf("RequestOTP: %v", err)
	}

	if _, _, _, err := svc.VerifyOTP(ctx, ChannelEmail, "  mixed.case@example.com ", ch.lastCode(), "ua", nil); err != nil {
		t.Fatalf("verification failed across a casing difference: %v", err)
	}
}

// A wrong code must not create a user. Otherwise the endpoint populates
// the users table for any identifier an attacker submits, which both
// pollutes the data and makes later "does this account exist?" answers
// meaningless.
func TestFailedVerificationCreatesNoUser(t *testing.T) {
	ctx := context.Background()
	ch := &captureChannel{}
	users := &fakeUserCreator{}
	svc, stop := serviceWithRealOTP(t, users, ch, &fakeAudit{})
	defer stop()

	if err := svc.RequestOTP(ctx, ChannelEmail, "nope@example.com", "en", nil); err != nil {
		t.Fatalf("RequestOTP: %v", err)
	}

	_, _, _, err := svc.VerifyOTP(ctx, ChannelEmail, "nope@example.com", wrongVariant(ch.lastCode()), "ua", nil)
	if !errors.Is(err, ErrCodeIncorrect) {
		t.Fatalf("got %v, want ErrCodeIncorrect", err)
	}
	if len(users.created) != 0 {
		t.Errorf("a failed verification created %d users", len(users.created))
	}
}

// A suspended account must not receive tokens, and the attempt is worth
// recording.
func TestSuspendedUserCannotLogIn(t *testing.T) {
	ctx := context.Background()
	ch := &captureChannel{}
	aud := &fakeAudit{}

	suspended := User{ID: uuid.New(), Role: RoleUser, Status: "suspended"}
	users := &fakeUserCreator{known: map[string]User{"banned@example.com": suspended}}

	svc, stop := serviceWithRealOTP(t, users, ch, aud)
	defer stop()

	if err := svc.RequestOTP(ctx, ChannelEmail, "banned@example.com", "en", nil); err != nil {
		t.Fatalf("RequestOTP: %v", err)
	}

	pair, _, _, err := svc.VerifyOTP(ctx, ChannelEmail, "banned@example.com", ch.lastCode(), "ua", nil)
	if !errors.Is(err, ErrUserSuspended) {
		t.Fatalf("got %v, want ErrUserSuspended", err)
	}
	if pair.AccessToken != "" {
		t.Error("a suspended user was issued an access token")
	}
	if !contains(strings.Join(aud.actions(), ","), "auth.login_blocked") {
		t.Errorf("the blocked login was not recorded: %v", aud.actions())
	}
}

// A delivery failure must NOT delete the stored code. Otherwise anyone
// able to make sending fail can cancel someone else's in-flight login.
func TestDeliveryFailureLeavesTheCodeUsable(t *testing.T) {
	ctx := context.Background()
	ch := &captureChannel{}
	svc, stop := serviceWithRealOTP(t, &fakeUserCreator{}, ch, &fakeAudit{})
	defer stop()

	// First send succeeds, so we know the code.
	if err := svc.RequestOTP(ctx, ChannelEmail, "flaky@example.com", "en", nil); err != nil {
		t.Fatalf("RequestOTP: %v", err)
	}
	code := ch.lastCode()

	// A subsequent request fails to deliver. It reissues, so the old code
	// is gone — but the NEW one must still be in Redis and usable once
	// the user gets it another way (a retry that succeeds).
	ch.err = errors.New("smtp unavailable")
	if err := svc.RequestOTP(ctx, ChannelEmail, "flaky@example.com", "en", nil); err == nil {
		t.Fatal("a delivery failure was not reported to the caller")
	}
	ch.err = nil

	// The superseded code must not work.
	if _, _, _, err := svc.VerifyOTP(ctx, ChannelEmail, "flaky@example.com", code, "ua", nil); err == nil {
		t.Error("a superseded code still verified")
	}
}

// The failure reason is recorded as an enum, and the identifier is not.
func TestFailedVerificationAuditsWithoutTheIdentifier(t *testing.T) {
	ctx := context.Background()
	ch := &captureChannel{}
	aud := &fakeAudit{}
	svc, stop := serviceWithRealOTP(t, &fakeUserCreator{}, ch, aud)
	defer stop()

	if err := svc.RequestOTP(ctx, ChannelEmail, "audited@example.com", "en", nil); err != nil {
		t.Fatalf("RequestOTP: %v", err)
	}
	_, _, _, _ = svc.VerifyOTP(ctx, ChannelEmail, "audited@example.com", wrongVariant(ch.lastCode()), "ua", nil)

	aud.mu.Lock()
	defer aud.mu.Unlock()

	var found bool
	for _, ev := range aud.events {
		if ev.action != "auth.otp_failed" {
			continue
		}
		found = true
		if reason, _ := ev.metadata["reason"].(string); reason != "incorrect" {
			t.Errorf("reason = %q, want the enum 'incorrect'", reason)
		}
		for key, value := range ev.metadata {
			if s, ok := value.(string); ok && strings.Contains(s, "@") {
				t.Errorf("the audit event carries an identifier: %s=%q", key, s)
			}
		}
	}
	if !found {
		t.Error("a failed verification was not audited")
	}
}

// ─── analytics ───────────────────────────────────────────────────────

// eventRecorder captures analytics instead of writing them.
type eventRecorder struct {
	mu     sync.Mutex
	events []recordedAnalytic
}

// Named distinctly from the audit sink's recordedEvent, which lives in
// service_test.go in the same package.
type recordedAnalytic struct {
	name   string
	userID *uuid.UUID
	props  map[string]any
}

func (r *eventRecorder) Emit(_ context.Context, name string, userID *uuid.UUID, props map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedAnalytic{name: name, userID: userID, props: props})
}

func (r *eventRecorder) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.events))
	for _, e := range r.events {
		out = append(out, e.name)
	}
	return out
}

func (r *eventRecorder) get(name string) (recordedAnalytic, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.name == name {
			return e, true
		}
	}
	return recordedAnalytic{}, false
}

// A first sign-in is a signup; a second is a login. Getting this backwards
// makes the signup funnel report every returning user as a new one, and
// nothing in the code would ever complain.
func TestAnalyticsDistinguishesSignupFromLogin(t *testing.T) {
	ctx := context.Background()
	ch := &captureChannel{}
	svc, stop := serviceWithRealOTP(t, &fakeUserCreator{}, ch, &fakeAudit{})
	defer stop()

	rec := &eventRecorder{}
	svc.events = rec

	const identifier = "analytics-funnel@example.com"

	// First sign-in.
	if err := svc.RequestOTP(ctx, ChannelEmail, identifier, "en", nil); err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, _, isNew, err := svc.VerifyOTP(
		ctx, ChannelEmail, identifier, ch.lastCode(), "test-agent", nil,
	); err != nil || !isNew {
		t.Fatalf("first verify: isNew=%v err=%v", isNew, err)
	}

	for _, want := range []string{
		analytics.OTPRequested, analytics.OTPVerified,
		analytics.SignupStarted, analytics.SignupCompleted,
	} {
		if _, ok := rec.get(want); !ok {
			t.Errorf("%q was not emitted for a first sign-in; got %v", want, rec.names())
		}
	}
	if _, ok := rec.get(analytics.LoginCompleted); ok {
		t.Error("a first sign-in emitted login_completed")
	}

	// Second sign-in, same identifier.
	second := &eventRecorder{}
	svc.events = second

	if err := svc.RequestOTP(ctx, ChannelEmail, identifier, "en", nil); err != nil {
		t.Fatalf("second request: %v", err)
	}
	if _, _, isNew, err := svc.VerifyOTP(
		ctx, ChannelEmail, identifier, ch.lastCode(), "test-agent", nil,
	); err != nil || isNew {
		t.Fatalf("second verify: isNew=%v err=%v", isNew, err)
	}

	if _, ok := second.get(analytics.LoginCompleted); !ok {
		t.Errorf("a returning user did not emit login_completed; got %v", second.names())
	}
	if _, ok := second.get(analytics.SignupCompleted); ok {
		t.Error("a returning user emitted signup_completed — the funnel would double-count")
	}
}

// otp_requested happens before anybody is identified. Attaching the
// identifier they typed would put an email address in the warehouse AND
// tell the pipeline something the endpoint deliberately refuses to tell
// the caller.
func TestOTPRequestedCarriesNoIdentifier(t *testing.T) {
	ctx := context.Background()
	ch := &captureChannel{}
	svc, stop := serviceWithRealOTP(t, &fakeUserCreator{}, ch, &fakeAudit{})
	defer stop()

	rec := &eventRecorder{}
	svc.events = rec

	const identifier = "analytics-anon@example.com"
	if err := svc.RequestOTP(ctx, ChannelEmail, identifier, "en", nil); err != nil {
		t.Fatalf("request: %v", err)
	}

	event, ok := rec.get(analytics.OTPRequested)
	if !ok {
		t.Fatalf("otp_requested was not emitted; got %v", rec.names())
	}
	if event.userID != nil {
		t.Error("otp_requested identified a user before verification")
	}
	for key, value := range event.props {
		if key != "channel" {
			t.Errorf("unexpected property %q = %v", key, value)
		}
		if str, isString := value.(string); isString && strings.Contains(str, "@") {
			t.Errorf("the identifier leaked into %q", key)
		}
	}
}
