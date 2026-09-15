package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/analytics"
)

// minOTPResponseTime is the floor on how long /auth/otp/request takes.
//
// Enumeration resistance has two halves and the body is only one of
// them. Creating a user is several database writes; looking one up is a
// single indexed read. That difference is measurable from outside and
// turns the endpoint into a free "is this number registered?" oracle —
// a real privacy leak for a product where the answer is "this person
// consults astrologers".
//
// 250ms comfortably exceeds both paths on any machine this runs on. It
// is a floor, not a sleep: a request that already took longer is not
// delayed further.
const minOTPResponseTime = 250 * time.Millisecond

var (
	ErrInvalidIdentifier = errors.New("auth: identifier is not valid for this channel")
	ErrUnknownChannel    = errors.New("auth: unknown channel")
	ErrUserSuspended     = errors.New("auth: account is suspended")
)

// Channel names accepted by the API.
const (
	ChannelEmail = "email"
	ChannelPhone = "phone"
)

// UserCreator is what auth needs from the users domain.
//
// Declared here, by the consumer. `auth` never imports `users` — the
// dependency runs the other way round at wiring time, which keeps the
// graph acyclic and this package testable with a fake.
type UserCreator interface {
	// FindOrCreateByIdentity returns the user for a verified identifier,
	// creating one if this is a first login. The boolean reports whether
	// a user was created, which the client needs to decide between
	// onboarding and the home screen.
	FindOrCreateByIdentity(ctx context.Context, p FindOrCreateParams) (User, bool, error)
}

type FindOrCreateParams struct {
	Channel    string
	Identifier string
}

// User is the minimum auth needs to mint a token. Deliberately not the
// full profile: anything more would put PII in this package's reach.
type User struct {
	ID     uuid.UUID
	Role   string
	Status string
}

// AuditSink records auth events.
//
// IDs, actions and enums only. Never an identifier, never a code.
type AuditSink interface {
	Record(ctx context.Context, userID *uuid.UUID, action string, metadata map[string]any, ipHash []byte)
}

// Service is the auth business logic, independent of HTTP.
type Service struct {
	otp     *OTPStore
	channel Channel
	rotator *Rotator
	users   UserCreator
	audit   AuditSink
	events  analytics.Emitter
	logger  *slog.Logger

	otpLength int
	// now and sleep are injectable so the timing floor can be asserted
	// without the test taking a quarter-second per case.
	now   func() time.Time
	sleep func(time.Duration)
}

type ServiceConfig struct {
	OTP     *OTPStore
	Channel Channel
	Rotator *Rotator
	Users   UserCreator
	Audit   AuditSink
	// Events is optional. A nil one becomes analytics.Nop rather than a
	// nil check at every call site.
	Events    analytics.Emitter
	Logger    *slog.Logger
	OTPLength int
}

func NewService(cfg ServiceConfig) *Service {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	var events analytics.Emitter = analytics.Nop{}
	if cfg.Events != nil {
		events = cfg.Events
	}
	return &Service{
		otp:       cfg.OTP,
		channel:   cfg.Channel,
		rotator:   cfg.Rotator,
		users:     cfg.Users,
		audit:     cfg.Audit,
		events:    events,
		logger:    logger,
		otpLength: cfg.OTPLength,
		now:       time.Now,
		sleep:     time.Sleep,
	}
}

// RequestOTP issues and delivers a code.
//
// Returns nothing on success, deliberately. The response body is
// identical whether or not the identifier is known — see
// minOTPResponseTime for the other half of that property.
func (s *Service) RequestOTP(ctx context.Context, channel, identifier, locale string, ipHash []byte) error {
	start := s.now()
	defer s.padTo(start, minOTPResponseTime)

	normalised, err := NormaliseIdentifier(channel, identifier)
	if err != nil {
		return err
	}

	code, err := s.otp.Issue(ctx, channel, normalised, s.otpLength)
	if err != nil {
		return err
	}

	if err := s.channel.Send(ctx, normalised, code, locale); err != nil {
		// Delivery failed. The code stays in Redis until its TTL: the
		// alternative is deleting it, which would let anyone cancel
		// someone else's in-flight login by triggering a send failure.
		return fmt.Errorf("auth: deliver code: %w", err)
	}

	s.audit.Record(ctx, nil, "auth.otp_requested", map[string]any{
		"channel": channel,
	}, ipHash)

	// Anonymous by construction. Whether this identifier already has an
	// account is deliberately NOT looked up here: it would tell the
	// analytics pipeline something the endpoint refuses to tell the
	// caller, and a warehouse is a worse place to leak it than a response
	// body. signup_started is emitted on verification, where the answer is
	// already known for free.
	s.events.Emit(ctx, analytics.OTPRequested, nil, map[string]any{
		"channel": channel,
	})

	return nil
}

// VerifyOTP consumes a code and returns tokens.
//
// `isNew` tells the client whether to route to onboarding or home. It is
// derived from whether a user row was created, not from any field on the
// user, so a returning user whose profile is incomplete still skips
// onboarding rather than being sent round again.
func (s *Service) VerifyOTP(
	ctx context.Context,
	channel, identifier, code, userAgent string,
	ipHash []byte,
) (pair TokenPair, user User, isNew bool, err error) {
	normalised, err := NormaliseIdentifier(channel, identifier)
	if err != nil {
		return TokenPair{}, User{}, false, err
	}

	if err := s.otp.Verify(ctx, channel, normalised, code); err != nil {
		s.audit.Record(ctx, nil, "auth.otp_failed", map[string]any{
			"channel": channel,
			// The reason is an enum, never the code or the identifier.
			"reason": otpFailureReason(err),
		}, ipHash)
		return TokenPair{}, User{}, false, err
	}

	user, isNew, err = s.users.FindOrCreateByIdentity(ctx, FindOrCreateParams{
		Channel:    channel,
		Identifier: normalised,
	})
	if err != nil {
		return TokenPair{}, User{}, false, fmt.Errorf("auth: find or create user: %w", err)
	}

	if user.Status == "suspended" {
		s.audit.Record(ctx, &user.ID, "auth.login_blocked", map[string]any{
			"reason": "suspended",
		}, ipHash)
		return TokenPair{}, User{}, false, ErrUserSuspended
	}

	pair, err = s.rotator.Issue(ctx, user.ID, user.Role, userAgent, ipHash)
	if err != nil {
		return TokenPair{}, User{}, false, err
	}

	action := "auth.login"
	if isNew {
		action = "auth.signup"
	}
	s.audit.Record(ctx, &user.ID, action, map[string]any{
		"channel": channel,
	}, ipHash)

	s.events.Emit(ctx, analytics.OTPVerified, &user.ID, map[string]any{
		"channel": channel,
	})

	// signup_started fires here rather than at the request, because this
	// is the first point at which "is this a new account" is known without
	// a lookup that would leak the answer into the pipeline.
	if isNew {
		s.events.Emit(ctx, analytics.SignupStarted, &user.ID, map[string]any{"channel": channel})
		s.events.Emit(ctx, analytics.SignupCompleted, &user.ID, map[string]any{"channel": channel})
	} else {
		s.events.Emit(ctx, analytics.LoginCompleted, &user.ID, map[string]any{"channel": channel})
	}

	return pair, user, isNew, nil
}

// Refresh rotates a refresh token.
func (s *Service) Refresh(ctx context.Context, presented, userAgent string, ipHash []byte) (TokenPair, error) {
	pair, err := s.rotator.Rotate(ctx, presented, RoleUser, userAgent, ipHash)
	if err != nil {
		if errors.Is(err, ErrRefreshReused) {
			// Worth an alert. A reused token means one leaked, and the
			// family has already been revoked by the time we get here.
			s.logger.WarnContext(ctx, "refresh token reuse detected; family revoked")
			s.audit.Record(ctx, nil, "auth.refresh_reuse_detected", nil, ipHash)
		}
		return TokenPair{}, err
	}
	return pair, nil
}

func (s *Service) padTo(start time.Time, minimum time.Duration) {
	if elapsed := s.now().Sub(start); elapsed < minimum {
		s.sleep(minimum - elapsed)
	}
}

func otpFailureReason(err error) string {
	switch {
	case errors.Is(err, ErrTooManyAttempts):
		return "too_many_attempts"
	case errors.Is(err, ErrCodeNotFound):
		return "no_active_code"
	case errors.Is(err, ErrCodeIncorrect):
		return "incorrect"
	default:
		return "error"
	}
}

// ─── Identifier normalisation ────────────────────────────────────────

// Email is validated loosely on purpose. A strict RFC 5322 regex rejects
// addresses that work, and the only thing that actually proves an
// address is deliverable is sending to it — which is what happens next.
var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s.]+\.[^@\s]+$`)

// E.164: a leading +, a non-zero country code, up to 15 digits total.
var phonePattern = regexp.MustCompile(`^\+[1-9]\d{7,14}$`)

// NormaliseIdentifier canonicalises and validates.
//
// Normalisation before storage and before the rate-limit key matters:
// without it "User@Example.com" and "user@example.com" are two
// identities with separate OTP windows, so the per-identifier limit can
// be bypassed by varying the case.
func NormaliseIdentifier(channel, identifier string) (string, error) {
	identifier = strings.TrimSpace(identifier)

	switch channel {
	case ChannelEmail:
		identifier = strings.ToLower(identifier)
		if !emailPattern.MatchString(identifier) {
			return "", ErrInvalidIdentifier
		}
		return identifier, nil

	case ChannelPhone:
		// Strip the separators people type. Anything else is a mistake
		// rather than formatting.
		identifier = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(identifier)
		if !phonePattern.MatchString(identifier) {
			return "", ErrInvalidIdentifier
		}
		return identifier, nil

	default:
		return "", ErrUnknownChannel
	}
}

// RevokeByToken ends the session a refresh token belongs to.
//
// Revokes the single session, not the family: logging out is a normal
// action, and taking the whole lineage down would sign the user out of
// every device whenever they signed out of one.
func (s *Service) RevokeByToken(ctx context.Context, presented string) error {
	return s.rotator.RevokeByToken(ctx, presented)
}

// ContactLookup returns the verified contact a user can receive codes on.
//
// Declared by the consumer. Re-verification must send to the address
// already on the account, never to one supplied in the request — that
// would let whoever holds a stolen access token nominate their own
// inbox and satisfy the check they were meant to fail.
type ContactLookup interface {
	VerifiedContact(ctx context.Context, userID uuid.UUID) (channel, identifier string, err error)
}

// FreshOTP re-verifies possession at the moment of a dangerous action.
//
// Account deletion and data export both use it. An access token lives
// fifteen minutes and an unlocked laptop is enough to use one; that is
// not the bar for erasing an account or downloading someone's entire
// history.
type FreshOTP struct {
	otp      *OTPStore
	channel  Channel
	contacts ContactLookup
	length   int
}

func NewFreshOTP(otp *OTPStore, ch Channel, contacts ContactLookup, length int) *FreshOTP {
	return &FreshOTP{otp: otp, channel: ch, contacts: contacts, length: length}
}

// Challenge sends a code to the account's own verified contact.
func (f *FreshOTP) Challenge(ctx context.Context, userID uuid.UUID, locale string) error {
	channel, identifier, err := f.contacts.VerifiedContact(ctx, userID)
	if err != nil {
		return err
	}

	code, err := f.otp.Issue(ctx, freshScope(channel), identifier, f.length)
	if err != nil {
		return err
	}
	return f.channel.Send(ctx, identifier, code, locale)
}

// VerifyFresh consumes a challenge code.
func (f *FreshOTP) VerifyFresh(ctx context.Context, userID uuid.UUID, code string) error {
	if code == "" {
		return ErrCodeNotFound
	}
	channel, identifier, err := f.contacts.VerifiedContact(ctx, userID)
	if err != nil {
		return err
	}
	return f.otp.Verify(ctx, freshScope(channel), identifier, code)
}

// freshScope namespaces re-verification codes away from login codes.
//
// Without it, a code requested to log in would also authorise account
// deletion — and the two are not the same consent. It also means an
// in-flight login code is not clobbered by a deletion challenge.
func freshScope(channel string) string { return "fresh:" + channel }

// IdentityLinker links an OAuth identity to an account.
//
// Declared by the consumer, like UserCreator. Keyed on the provider's
// stable subject, never on the email — an address can be reassigned
// within a Workspace domain, and keying on it would hand the new holder
// the previous owner's account.
type IdentityLinker interface {
	FindOrCreateByOAuth(ctx context.Context, provider, subject, email string) (User, bool, error)
}

// CompleteOAuth turns a verified provider identity into tokens.
func (s *Service) CompleteOAuth(
	ctx context.Context,
	linker IdentityLinker,
	provider, subject, email, userAgent string,
	ipHash []byte,
) (TokenPair, User, bool, error) {
	user, isNew, err := linker.FindOrCreateByOAuth(ctx, provider, subject, email)
	if err != nil {
		return TokenPair{}, User{}, false, fmt.Errorf("auth: link oauth identity: %w", err)
	}

	if user.Status == "suspended" {
		s.audit.Record(ctx, &user.ID, "auth.login_blocked",
			map[string]any{"reason": "suspended", "provider": provider}, ipHash)
		return TokenPair{}, User{}, false, ErrUserSuspended
	}

	pair, err := s.rotator.Issue(ctx, user.ID, user.Role, userAgent, ipHash)
	if err != nil {
		return TokenPair{}, User{}, false, err
	}

	action := "auth.login"
	if isNew {
		action = "auth.signup"
	}
	s.audit.Record(ctx, &user.ID, action, map[string]any{"provider": provider}, ipHash)

	event := analytics.LoginCompleted
	if isNew {
		s.events.Emit(ctx, analytics.SignupStarted, &user.ID, map[string]any{"channel": provider})
		event = analytics.SignupCompleted
	}
	s.events.Emit(ctx, event, &user.ID, map[string]any{
		// The channel for an OAuth sign-in is the provider, which the
		// vocabulary already allows: email | phone | google | apple.
		"channel":  provider,
		"provider": provider,
	})

	return pair, user, isNew, nil
}
