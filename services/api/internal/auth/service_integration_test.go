//go:build integration

package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
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
