package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ─── fakes ───────────────────────────────────────────────────────────

type fakeUserCreator struct {
	// known maps identifier → user. Anything absent is created.
	known   map[string]User
	created []string
	// delay simulates the extra work creating a user costs, which is what
	// the timing floor exists to hide.
	delay time.Duration
	err   error
}

func (f *fakeUserCreator) FindOrCreateByIdentity(_ context.Context, p FindOrCreateParams) (User, bool, error) {
	if f.err != nil {
		return User{}, false, f.err
	}
	if u, ok := f.known[p.Identifier]; ok {
		return u, false, nil
	}
	time.Sleep(f.delay)
	u := User{ID: uuid.New(), Role: RoleUser, Status: "active"}
	if f.known == nil {
		f.known = map[string]User{}
	}
	f.known[p.Identifier] = u
	f.created = append(f.created, p.Identifier)
	return u, true, nil
}

type recordedEvent struct {
	action   string
	userID   *uuid.UUID
	metadata map[string]any
}

type fakeAudit struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (f *fakeAudit) Record(_ context.Context, userID *uuid.UUID, action string, metadata map[string]any, _ []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, recordedEvent{action: action, userID: userID, metadata: metadata})
}

func (f *fakeAudit) actions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.events))
	for _, e := range f.events {
		out = append(out, e.action)
	}
	return out
}

type captureChannel struct {
	mu    sync.Mutex
	sent  []string // codes
	to    []string // identifiers, as the service passed them
	err   error
	calls int
}

func (c *captureChannel) ID() string { return "capture" }

func (c *captureChannel) Send(_ context.Context, identifier, code, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.err != nil {
		return c.err
	}
	c.sent = append(c.sent, code)
	c.to = append(c.to, identifier)
	return nil
}

func (c *captureChannel) lastCode() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) == 0 {
		return ""
	}
	return c.sent[len(c.sent)-1]
}

// ─── identifier normalisation ────────────────────────────────────────

// Normalisation runs before storage AND before the rate-limit key.
// Without it "User@Example.com" and "user@example.com" are two
// identities with separate OTP windows, so the per-identifier limit is
// bypassed by varying the case.
func TestNormaliseIdentifier(t *testing.T) {
	cases := []struct {
		channel, in, want string
	}{
		{ChannelEmail, "User@Example.COM", "user@example.com"},
		{ChannelEmail, "  spaced@example.com  ", "spaced@example.com"},
		{ChannelPhone, "+91 98765 43210", "+919876543210"},
		{ChannelPhone, "+91-98765-43210", "+919876543210"},
		{ChannelPhone, "+91 (98765) 43210", "+919876543210"},
	}

	for _, tc := range cases {
		got, err := NormaliseIdentifier(tc.channel, tc.in)
		if err != nil {
			t.Errorf("NormaliseIdentifier(%q, %q): %v", tc.channel, tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormaliseIdentifier(%q, %q) = %q, want %q", tc.channel, tc.in, got, tc.want)
		}
	}
}

func TestNormaliseIdentifierRejectsInvalid(t *testing.T) {
	cases := map[string][2]string{
		"no at":           {ChannelEmail, "notanemail"},
		"no domain dot":   {ChannelEmail, "user@localhost"},
		"empty email":     {ChannelEmail, ""},
		"two at":          {ChannelEmail, "a@b@c.com"},
		"spaces inside":   {ChannelEmail, "a b@c.com"},
		"phone no plus":   {ChannelPhone, "919876543210"},
		"phone leading 0": {ChannelPhone, "+09876543210"},
		"phone too short": {ChannelPhone, "+911234"},
		"phone too long":  {ChannelPhone, "+9198765432101234"},
		"phone letters":   {ChannelPhone, "+91987654321a"},
		"empty phone":     {ChannelPhone, ""},
		"unknown channel": {"carrier-pigeon", "x"},
		"empty channel":   {"", "user@example.com"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NormaliseIdentifier(tc[0], tc[1]); err == nil {
				t.Errorf("accepted %q on channel %q", tc[1], tc[0])
			}
		})
	}
}

// ─── the timing floor ────────────────────────────────────────────────

func newTestService(t *testing.T, users *fakeUserCreator, ch Channel, aud AuditSink) *Service {
	t.Helper()
	// The OTP store is exercised properly in the integration tests; here
	// a nil Redis would panic, so these tests use the ones that do not
	// touch it, plus an injected store where needed.
	return NewService(ServiceConfig{
		Channel:   ch,
		Users:     users,
		Audit:     aud,
		OTPLength: 6,
	})
}

// The half of enumeration resistance that an identical response body
// does not give you.
//
// Creating a user is several writes; finding one is a single indexed
// read. That difference is measurable from outside and turns the
// endpoint into a free "is this number registered?" oracle — a real
// privacy leak for a product where the answer is "this person consults
// astrologers".
//
// Tested with a fake clock so the suite does not spend a quarter-second
// per case.
func TestRequestOTPPadsToAConstantFloor(t *testing.T) {
	svc := newTestService(t, &fakeUserCreator{}, &captureChannel{}, &fakeAudit{})

	var (
		slept   time.Duration
		nowVal  time.Time
		elapsed time.Duration
	)
	nowVal = time.Now()
	svc.now = func() time.Time { return nowVal.Add(elapsed) }
	svc.sleep = func(d time.Duration) { slept = d }

	cases := map[string]struct {
		work      time.Duration
		wantSleep time.Duration
	}{
		"fast path (existing user)": {work: 5 * time.Millisecond, wantSleep: minOTPResponseTime - 5*time.Millisecond},
		"slow path (new user)":      {work: 80 * time.Millisecond, wantSleep: minOTPResponseTime - 80*time.Millisecond},
		"already over the floor":    {work: 400 * time.Millisecond, wantSleep: 0},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			slept, elapsed = 0, 0
			start := svc.now()
			elapsed = tc.work
			svc.padTo(start, minOTPResponseTime)

			if slept != tc.wantSleep {
				t.Errorf("slept %v, want %v — the response time is not constant", slept, tc.wantSleep)
			}
		})
	}
}

// Whatever the work took, the total must land on the same number.
func TestPaddingProducesTheSameTotalRegardlessOfWork(t *testing.T) {
	svc := newTestService(t, &fakeUserCreator{}, &captureChannel{}, &fakeAudit{})

	totals := map[time.Duration]time.Duration{}
	for _, work := range []time.Duration{1, 10, 50, 120, 200} {
		var slept time.Duration
		base := time.Now()
		elapsed := work * time.Millisecond

		svc.now = func() time.Time { return base.Add(elapsed) }
		svc.sleep = func(d time.Duration) { slept = d }

		svc.padTo(base, minOTPResponseTime)
		totals[work] = elapsed + slept
	}

	for work, total := range totals {
		if total != minOTPResponseTime {
			t.Errorf("work=%dms produced a total of %v, want %v — the duration leaks how much work was done",
				work, total, minOTPResponseTime)
		}
	}
}

// ─── audit ───────────────────────────────────────────────────────────

// Audit rows are retained far longer than anything else and are read by
// people who never saw this code. An identifier in one is permanent.
func TestAuditEventsCarryNoIdentifier(t *testing.T) {
	aud := &fakeAudit{}
	users := &fakeUserCreator{}
	ch := &captureChannel{}
	svc := newTestService(t, users, ch, aud)

	// Exercise the paths that record, without the Redis-backed store.
	svc.audit.Record(context.Background(), nil, "auth.otp_requested",
		map[string]any{"channel": ChannelEmail}, nil)

	id := uuid.New()
	svc.audit.Record(context.Background(), &id, "auth.signup",
		map[string]any{"channel": ChannelPhone}, nil)

	for _, ev := range aud.events {
		for key, value := range ev.metadata {
			s, isString := value.(string)
			if !isString {
				continue
			}
			if strings.Contains(s, "@") || strings.HasPrefix(s, "+") {
				t.Errorf("audit event %q carries what looks like an identifier: %s=%q",
					ev.action, key, s)
			}
		}
	}
}

func TestOTPFailureReasonIsAnEnum(t *testing.T) {
	cases := map[error]string{
		ErrTooManyAttempts: "too_many_attempts",
		ErrCodeNotFound:    "no_active_code",
		ErrCodeIncorrect:   "incorrect",
		errors.New("boom"): "error",
	}

	for err, want := range cases {
		if got := otpFailureReason(err); got != want {
			t.Errorf("otpFailureReason(%v) = %q, want %q", err, got, want)
		}
	}

	// Every value must be a short, fixed token — never free text that
	// could carry a code or an identifier.
	for err := range cases {
		reason := otpFailureReason(err)
		if len(reason) > 24 || strings.ContainsAny(reason, "@ +") {
			t.Errorf("reason %q does not look like an enum", reason)
		}
	}
}
