package pdf

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/charts"
)

// The orchestration, without a browser.
//
// Every failure below — Chrome timing out, the upload failing, the
// signature failing, a render returning nothing — is a branch a real
// Chrome will not enter on request. They are also precisely the branches
// where the job can get stuck at "running", which is the worst outcome
// available because it looks to the user like progress.

// ─── fakes ───────────────────────────────────────────────────────────

type fakeBrowser struct {
	mu    sync.Mutex
	urls  []string
	body  []byte
	err   error
	delay time.Duration
}

func (f *fakeBrowser) PrintToPDF(ctx context.Context, pageURL string) ([]byte, error) {
	f.mu.Lock()
	f.urls = append(f.urls, pageURL)
	delay, body, err := f.delay, f.body, f.err
	f.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return body, err
}

func (f *fakeBrowser) lastURL() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.urls) == 0 {
		return ""
	}
	return f.urls[len(f.urls)-1]
}

type fakeUploader struct {
	mu        sync.Mutex
	keys      []string
	bodies    [][]byte
	types     []string
	putErr    error
	signErr   error
	signedTTL time.Duration
}

func (f *fakeUploader) Put(_ context.Context, key string, body io.Reader, _ int64, contentType string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putErr != nil {
		return f.putErr
	}
	raw, _ := io.ReadAll(body)
	f.keys = append(f.keys, key)
	f.bodies = append(f.bodies, raw)
	f.types = append(f.types, contentType)
	return nil
}

func (f *fakeUploader) SignedURL(_ context.Context, key string, ttl time.Duration) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.signErr != nil {
		return "", f.signErr
	}
	f.signedTTL = ttl
	return "https://storage.example/" + key + "?signature=x", nil
}

type fakeMinter struct {
	mu     sync.Mutex
	scopes []charts.PrintScope
	token  string
	err    error
}

func (f *fakeMinter) Mint(_ context.Context, scope charts.PrintScope) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	f.scopes = append(f.scopes, scope)
	if f.token == "" {
		return "tok-" + uuid.NewString(), nil
	}
	return f.token, nil
}

// memoryStatus is a StatusStore backed by a map, so these tests need no
// Redis. The real store gets its own integration test.
type memoryStatus struct {
	mu      sync.Mutex
	written []Status
	setErr  error
}

/*
Set honours the context, because go-redis does.

This is not incidental fidelity. The renderer writes its terminal
"failed" status on a context derived with context.WithoutCancel,
precisely so the write survives the deadline that just fired. A fake
that ignores ctx cannot tell that apart from a write on the cancelled
context — and it did not: with WithoutCancel removed, the hanging-
render test still passed. The fake was asserting on itself.
*/
func (m *memoryStatus) Set(ctx context.Context, _, _ uuid.UUID, status Status) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setErr != nil {
		return m.setErr
	}
	m.written = append(m.written, status)
	return nil
}

func (m *memoryStatus) states() []State {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]State, 0, len(m.written))
	for _, s := range m.written {
		out = append(out, s.State)
	}
	return out
}

func (m *memoryStatus) last() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.written) == 0 {
		return Status{}
	}
	return m.written[len(m.written)-1]
}

// discardLogger keeps the test output readable. The renderer logs on
// every failure branch, and there are a lot of failure branches here.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

// ─── harness ─────────────────────────────────────────────────────────

type rig struct {
	renderer *Renderer
	browser  *fakeBrowser
	uploader *fakeUploader
	minter   *fakeMinter
	status   *memoryStatus
	payload  RenderPayload
}

func newRig(t *testing.T) *rig {
	t.Helper()

	browser := &fakeBrowser{body: []byte("%PDF-1.4 fake")}
	uploader := &fakeUploader{}
	minter := &fakeMinter{}
	status := &memoryStatus{}

	renderer, err := NewRenderer(browser, uploader, minter, status,
		"http://web.internal:3000", discardLogger())
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}

	return &rig{
		renderer: renderer,
		browser:  browser,
		uploader: uploader,
		minter:   minter,
		status:   status,
		payload: RenderPayload{
			JobID:     uuid.New(),
			UserID:    uuid.New(),
			ProfileID: uuid.New(),
			Locale:    "en",
		},
	}
}

func (r *rig) run(t *testing.T) error {
	t.Helper()
	task, err := NewRenderTask(r.payload)
	if err != nil {
		t.Fatalf("NewRenderTask: %v", err)
	}
	return r.renderer.Handle(context.Background(), task)
}

// ─── the happy path ──────────────────────────────────────────────────

func TestASuccessfulRenderUploadsSignsAndRecordsDone(t *testing.T) {
	r := newRig(t)

	if err := r.run(t); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if got := r.status.states(); len(got) != 2 || got[0] != StateRunning || got[1] != StateDone {
		t.Fatalf("status went %v, want [running done]", got)
	}

	final := r.status.last()
	if final.URL == "" {
		t.Fatal("a completed job carries no URL; there is nothing for the user to download")
	}
	if final.ExpiresAt == nil {
		t.Fatal("a completed job carries no expiry, so the UI cannot say when the link dies")
	}
	if r.uploader.signedTTL != SignedURLTTL {
		t.Fatalf("signed for %s, want %s", r.uploader.signedTTL, SignedURLTTL)
	}
	if len(r.uploader.types) != 1 || r.uploader.types[0] != "application/pdf" {
		t.Fatalf("uploaded as %v, want application/pdf — anything else downloads "+
			"instead of opening, which on a phone is the feature not working",
			r.uploader.types)
	}
}

// The token is minted for the payload's scope and nothing else.
func TestTheRenderMintsATokenForExactlyThePayloadsScope(t *testing.T) {
	r := newRig(t)

	if err := r.run(t); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(r.minter.scopes) != 1 {
		t.Fatalf("minted %d tokens, want exactly 1", len(r.minter.scopes))
	}
	got := r.minter.scopes[0]
	if got.UserID != r.payload.UserID || got.ProfileID != r.payload.ProfileID {
		t.Fatalf("minted for %+v, want user %s profile %s",
			got, r.payload.UserID, r.payload.ProfileID)
	}
}

/*
The object key names nobody.

Birth date plus time plus place is close to a unique identifier, and
the security rules treat it like an email address. An object key turns
up in bucket listings, access logs and CDN metrics — none of which are
places any of that belongs. Nor is the profile id, which is guessable
by whoever already has it.
*/
func TestTheObjectKeyCarriesNoIdentifier(t *testing.T) {
	r := newRig(t)

	if err := r.run(t); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(r.uploader.keys) != 1 {
		t.Fatalf("uploaded %d objects, want 1", len(r.uploader.keys))
	}

	key := r.uploader.keys[0]
	for name, value := range map[string]string{
		"the user id":    r.payload.UserID.String(),
		"the profile id": r.payload.ProfileID.String(),
		"the job id":     r.payload.JobID.String(),
	} {
		if strings.Contains(key, value) {
			t.Errorf("the object key contains %s: %q", name, key)
		}
	}
	if !strings.HasPrefix(key, "kundli-") || !strings.HasSuffix(key, ".pdf") {
		t.Errorf("object key %q is not kundli-{uuid}.pdf", key)
	}
}

// The page URL is built from the API's own web origin and the minted
// token, and carries nothing from the payload but the locale.
func TestThePageURLIsBuiltFromTheServersOwnOrigin(t *testing.T) {
	r := newRig(t)
	r.minter.token = "the-token"
	r.payload.Locale = "hi"

	if err := r.run(t); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	got := r.browser.lastURL()
	for _, want := range []string{
		"http://web.internal:3000/kundli/print?",
		"token=the-token",
		"locale=hi",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("page URL %q is missing %q", got, want)
		}
	}
	if strings.Contains(got, r.payload.ProfileID.String()) {
		t.Errorf("the page URL names the profile: %q. The print route takes its "+
			"scope from the token; a profile id in the URL is an id the route "+
			"could start trusting", got)
	}
}

// ─── the failures ────────────────────────────────────────────────────

/*
Every failure ends at "failed", never at "running".

This is the property the whole error path exists for. A job left at
"running" is polled forever by a client that has no way to know the
work stopped — strictly worse than an error, because an error at least
offers a retry button.
*/
func TestEveryFailureLeavesTheJobFailedRatherThanRunning(t *testing.T) {
	boom := errors.New("boom")

	cases := map[string]func(*rig){
		"the browser fails":        func(r *rig) { r.browser.err = boom },
		"the browser returns none": func(r *rig) { r.browser.body = nil },
		"the upload fails":         func(r *rig) { r.uploader.putErr = boom },
		"the signature fails":      func(r *rig) { r.uploader.signErr = boom },
		"the mint fails":           func(r *rig) { r.minter.err = boom },
	}

	for name, break_ := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			break_(r)

			if err := r.run(t); err == nil {
				t.Fatal("Handle returned nil; asynq would mark this job succeeded")
			}

			states := r.status.states()
			if len(states) == 0 || states[len(states)-1] != StateFailed {
				t.Fatalf("status ended at %v, want the last to be %q. A job stuck at "+
					"%q is polled forever by a client that cannot know the work stopped",
					states, StateFailed, StateRunning)
			}
		})
	}
}

/*
A failure message tells the user nothing about the inside.

The underlying errors here name buckets, hosts, and — on a chromedp
timeout — the page URL, which carries a live print token. None of that
may reach a response body.
*/
func TestAFailureMessageLeaksNothing(t *testing.T) {
	r := newRig(t)
	r.minter.token = "secret-print-token"
	r.browser.err = errors.New(
		"navigate http://web.internal:3000/kundli/print?token=secret-print-token: timeout")

	if err := r.run(t); err == nil {
		t.Fatal("Handle returned nil")
	}

	message := r.status.last().Message
	if message == "" {
		t.Fatal("a failed job has no message, so the UI has nothing to show")
	}
	for _, leak := range []string{"secret-print-token", "web.internal", "token", "timeout", "navigate"} {
		if strings.Contains(strings.ToLower(message), strings.ToLower(leak)) {
			t.Errorf("the user-facing message contains %q: %q", leak, message)
		}
	}
}

/*
A render that hangs is recorded as failed, not left running.

The subtle part is WHICH context writes that status. The renderer's
own deadline has just cancelled ctx, so a status write on ctx would
fail too — and the job would stay at "running", which is exactly what
the deadline was added to prevent. context.WithoutCancel is what makes
the difference, and this test is what proves it.
*/
func TestAHangingRenderStillRecordsFailure(t *testing.T) {
	r := newRig(t)

	// Longer than the deadline this test installs, so the browser is
	// still "working" when the context dies.
	r.browser.delay = 2 * time.Second

	// A deadline of the caller's, well inside RenderTimeout, so the test
	// does not wait ninety seconds to observe the same branch.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	task, err := NewRenderTask(r.payload)
	if err != nil {
		t.Fatalf("NewRenderTask: %v", err)
	}

	if err := r.renderer.Handle(ctx, task); err == nil {
		t.Fatal("a cancelled render returned nil")
	}

	states := r.status.states()
	if len(states) == 0 || states[len(states)-1] != StateFailed {
		t.Fatalf("a cancelled render left the status at %v. The write must use a "+
			"context that survives the cancellation, or the job stays %q forever",
			states, StateRunning)
	}
}

// An unparseable payload is not retried.
//
// Three retries cost three browsers and produce three identical parse
// failures. asynq.SkipRetry is how the queue is told the difference
// between "try again" and "this will never work".
func TestAnUnparseablePayloadIsNotRetried(t *testing.T) {
	r := newRig(t)

	err := r.renderer.Handle(context.Background(),
		asynq.NewTask(TypeRender, []byte("{not json")))
	if err == nil {
		t.Fatal("Handle accepted an unparseable payload")
	}
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("error %v does not wrap asynq.SkipRetry, so the queue will retry "+
			"a payload that cannot ever parse", err)
	}
}

// ─── the task ────────────────────────────────────────────────────────

// The payload carries identifiers and nothing else.
//
// It sits in Redis and in asynq's web UI. Birth details there would be
// PII in two systems that were never meant to hold any.
func TestTheTaskPayloadCarriesNoBirthData(t *testing.T) {
	task, err := NewRenderTask(RenderPayload{
		JobID: uuid.New(), UserID: uuid.New(), ProfileID: uuid.New(), Locale: "en",
	})
	if err != nil {
		t.Fatalf("NewRenderTask: %v", err)
	}

	body := strings.ToLower(string(task.Payload()))
	for _, field := range []string{
		"birth_date", "birth_time", "birth_place", "latitude", "longitude",
		"label", "name", "email", "phone",
	} {
		if strings.Contains(body, field) {
			t.Errorf("the queued payload has a %q field: %s", field, task.Payload())
		}
	}
}

func TestATaskWithoutIdentifiersIsRefused(t *testing.T) {
	full := RenderPayload{JobID: uuid.New(), UserID: uuid.New(), ProfileID: uuid.New()}

	for name, mutate := range map[string]func(*RenderPayload){
		"no job":     func(p *RenderPayload) { p.JobID = uuid.Nil },
		"no user":    func(p *RenderPayload) { p.UserID = uuid.Nil },
		"no profile": func(p *RenderPayload) { p.ProfileID = uuid.Nil },
	} {
		t.Run(name, func(t *testing.T) {
			payload := full
			mutate(&payload)
			if _, err := NewRenderTask(payload); err == nil {
				t.Fatal("accepted a payload missing an identifier")
			}
		})
	}
}

// The task id is the job id, so a retried HTTP request collapses onto
// one render rather than starting a second browser.
func TestTheTaskIsIdentifiedByTheJobID(t *testing.T) {
	jobID := uuid.New()
	task, err := NewRenderTask(RenderPayload{
		JobID: jobID, UserID: uuid.New(), ProfileID: uuid.New(),
	})
	if err != nil {
		t.Fatalf("NewRenderTask: %v", err)
	}

	parsed, err := ParseRenderPayload(task.Payload())
	if err != nil {
		t.Fatalf("ParseRenderPayload: %v", err)
	}
	if parsed.JobID != jobID {
		t.Fatalf("payload job id %s, want %s", parsed.JobID, jobID)
	}

	// The queue and the task-id dedup are asserted against real Redis in
	// job_integration_test.go. asynq.Task exposes no way to read its own
	// options, so a unit test asserting on them would be asserting on
	// what this test file passed in — not on what asynq does with it.
}
