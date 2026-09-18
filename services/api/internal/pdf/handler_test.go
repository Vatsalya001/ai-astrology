package pdf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/auth"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/redis/ratelimit"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/reqctx"
)

// The two HTTP endpoints, through the REAL auth middleware.
//
// A forged principal would let these tests assert on a code path
// production never runs, and the alternative — exporting a helper whose
// only purpose is manufacturing an identity — is not a thing this
// codebase should own for a test's convenience. So: a real issuer, a
// real token, real extraction.
//
// The ownership middleware is stubbed, because it lives in httpapi and
// httpapi imports this package. What it does is put a verified profile
// id in the request context, which reqctx.WithProfileID does directly.

const notARealPDFSigningKey = "example-not-a-real-pdf-test-key-32b"

// ─── fakes ───────────────────────────────────────────────────────────

type fakeQueue struct {
	mu    sync.Mutex
	tasks []*asynq.Task
	err   error

	/*
	   The queue LOOKS at the status store when a task arrives, the way a
	   worker does the instant it picks one up.

	   Without this it cannot observe ordering at all: a fake that merely
	   appends to a slice records the same thing whether the status was
	   written before the enqueue or after, and the ordering test passes
	   either way. It did. Reversing the two statements in Create left the
	   suite green, which is the test asserting on its own fake rather
	   than on the handler.
	*/
	status    *keyedStatus
	seenState []State
}

func (q *fakeQueue) Enqueue(task *asynq.Task) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return q.err
	}

	// What a worker would find if it started this instant.
	if q.status != nil {
		if payload, err := ParseRenderPayload(task.Payload()); err == nil {
			state := State("")
			if status, getErr := q.status.Get(context.Background(), payload.UserID, payload.JobID); getErr == nil {
				state = status.State
			}
			q.seenState = append(q.seenState, state)
		}
	}

	q.tasks = append(q.tasks, task)
	return nil
}

// stateAtEnqueue is what the store held when the task hit the queue.
func (q *fakeQueue) stateAtEnqueue(t *testing.T) State {
	t.Helper()
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.seenState) != 1 {
		t.Fatalf("observed %d enqueues, want exactly 1", len(q.seenState))
	}
	return q.seenState[0]
}

func (q *fakeQueue) count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.tasks)
}

func (q *fakeQueue) only(t *testing.T) RenderPayload {
	t.Helper()
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.tasks) != 1 {
		t.Fatalf("queued %d tasks, want exactly 1", len(q.tasks))
	}
	payload, err := ParseRenderPayload(q.tasks[0].Payload())
	if err != nil {
		t.Fatalf("queued task payload does not parse: %v", err)
	}
	return payload
}

// keyedStatus is a StatusStore backed by a map, keyed the same way the
// real one is — by user AND job.
//
// Keying it by job alone would be the natural shortcut and would make
// the cross-user test below pass for the wrong reason: it would be
// asserting that this map is scoped, not that the store is.
type keyedStatus struct {
	mu     sync.Mutex
	values map[string]Status
	setErr error
}

func newKeyedStatus() *keyedStatus {
	return &keyedStatus{values: map[string]Status{}}
}

func (k *keyedStatus) Set(_ context.Context, userID, jobID uuid.UUID, status Status) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.setErr != nil {
		return k.setErr
	}
	k.values[statusKey(userID, jobID)] = status
	return nil
}

func (k *keyedStatus) Get(_ context.Context, userID, jobID uuid.UUID) (Status, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	status, ok := k.values[statusKey(userID, jobID)]
	if !ok {
		return Status{}, ErrNoJob
	}
	return status, nil
}

// ─── harness ─────────────────────────────────────────────────────────

/*
countingLimiter allows a fixed number of requests, then refuses.

Real enough to be worth asserting on: it records the RULE and the
SUBJECT it was asked about, which is how "limited per user" is checked
as a fact rather than inferred from a 429 that an IP-keyed limiter
would also produce.
*/
type countingLimiter struct {
	mu       sync.Mutex
	allowed  int
	seen     []string
	rules    []string
	err      error
	requests int
}

func (l *countingLimiter) Allow(
	_ context.Context, rule ratelimit.Rule, subject string,
) (ratelimit.Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.requests++
	l.seen = append(l.seen, subject)
	l.rules = append(l.rules, rule.Name)

	if l.err != nil {
		return ratelimit.Result{}, l.err
	}
	if l.requests > l.allowed {
		return ratelimit.Result{Allowed: false, RetryAfter: 90 * time.Second}, nil
	}
	return ratelimit.Result{Allowed: true, Remaining: l.allowed - l.requests}, nil
}

func (l *countingLimiter) subjects() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.seen...)
}

type httpRig struct {
	router    http.Handler
	queue     *fakeQueue
	status    *keyedStatus
	limiter   *countingLimiter
	logs      *bytes.Buffer
	issuer    *auth.Issuer
	user      uuid.UUID
	profileID uuid.UUID
}

func newHTTPRig(t *testing.T) *httpRig {
	t.Helper()

	issuer, err := auth.NewIssuer(notARealPDFSigningKey, time.Minute, time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	status := newKeyedStatus()
	queue := &fakeQueue{status: status}
	profileID := uuid.New()

	// High enough that the tests which are not about limiting are never
	// refused by it, and low enough that the ones which are can reach it.
	limiter := &countingLimiter{allowed: 100}

	logs := &bytes.Buffer{}
	handler := NewHandler(queue, status, testErrorWriter).
		WithLogger(slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))

	r := chi.NewRouter()
	r.Route("/charts/{birthProfileId}", func(r chi.Router) {
		r.Use(auth.Authenticate(issuer, func(w http.ResponseWriter, r *http.Request, code int, errCode, msg string) {
			testErrorWriter(w, r, code, errCode, msg, nil)
		}))
		// Stands in for RequireProfileOwnership: by the time either
		// handler runs, a VERIFIED profile id is in the context.
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(reqctx.WithProfileID(req.Context(), profileID)))
			})
		})
		r.Post("/pdf", handler.Create(limiter))
		r.Get("/pdf/{jobId}", handler.Status)
	})

	return &httpRig{
		router:    r,
		queue:     queue,
		status:    status,
		limiter:   limiter,
		logs:      logs,
		issuer:    issuer,
		user:      uuid.New(),
		profileID: profileID,
	}
}

func testErrorWriter(w http.ResponseWriter, _ *http.Request, status int, code, message string, _ error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func (h *httpRig) tokenFor(t *testing.T, userID uuid.UUID) string {
	t.Helper()
	token, err := h.issuer.IssueAccessToken(userID, uuid.New(), "user")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	return token
}

func (h *httpRig) do(t *testing.T, method, path string, userID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, http.NoBody)
	if userID != uuid.Nil {
		req.Header.Set("Authorization", "Bearer "+h.tokenFor(t, userID))
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func (h *httpRig) createPath() string {
	return "/charts/" + h.profileID.String() + "/pdf"
}

// ─── POST ────────────────────────────────────────────────────────────

func TestRequestingAPDFReturns202AndAJobID(t *testing.T) {
	h := newHTTPRig(t)

	rec := h.do(t, http.MethodPost, h.createPath(), h.user)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("returned %d, want 202 — the work has been accepted and not done, "+
			"and 200 is a claim the client is entitled to act on: %s",
			rec.Code, rec.Body.String())
	}

	var body struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v — body %s", err, rec.Body.String())
	}
	jobID, err := uuid.Parse(body.JobID)
	if err != nil {
		t.Fatalf("job_id %q is not a uuid; the client has nothing to poll", body.JobID)
	}

	// The queued task must name the same job, or the client polls an id
	// nothing will ever write to.
	if queued := h.queue.only(t); queued.JobID != jobID {
		t.Fatalf("queued job %s but told the client %s", queued.JobID, jobID)
	}
}

// The task is scoped to the AUTHENTICATED user and the VERIFIED profile,
// neither of which came from the request body.
func TestTheQueuedTaskIsScopedToTheCallerNotTheRequest(t *testing.T) {
	h := newHTTPRig(t)

	if rec := h.do(t, http.MethodPost, h.createPath(), h.user); rec.Code != http.StatusAccepted {
		t.Fatalf("returned %d", rec.Code)
	}

	queued := h.queue.only(t)
	if queued.UserID != h.user {
		t.Errorf("queued for user %s, want the authenticated caller %s", queued.UserID, h.user)
	}
	if queued.ProfileID != h.profileID {
		t.Errorf("queued for profile %s, want the one the ownership middleware verified (%s)",
			queued.ProfileID, h.profileID)
	}
}

/*
The status is written BEFORE the task is queued.

The other order has a window: the worker picks the task up, writes
"running", and the request goroutine then overwrites it with "queued".
The client sees the job go backwards and, if the render finished
inside that window, polls a completed job forever.

Asserted by observing that a status exists for the job the moment the
response is written — which is only true if the write happened first.
*/
func TestTheJobIsMarkedQueuedBeforeItIsEnqueued(t *testing.T) {
	h := newHTTPRig(t)

	rec := h.do(t, http.MethodPost, h.createPath(), h.user)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("returned %d", rec.Code)
	}

	/*
	   Asserted at the moment of the enqueue, not afterwards.

	   Reading the store after the response is over would prove only that
	   both statements ran, in either order — which is what the first
	   version of this test did, and it stayed green with the two
	   statements swapped.
	*/
	if got := h.queue.stateAtEnqueue(t); got != StateQueued {
		t.Fatalf("when the task was enqueued the store held %q, want %q. A worker that "+
			"starts immediately will write \"running\" and then have it overwritten by "+
			"\"queued\" — the client watches the job go backwards, and if the render "+
			"finished inside that window it polls a completed job forever", got, StateQueued)
	}

	queued := h.queue.only(t)
	status, err := h.status.Get(context.Background(), h.user, queued.JobID)
	if err != nil {
		t.Fatalf("no status was written for the job the client was told to poll: %v", err)
	}
	if status.State != StateQueued {
		t.Fatalf("status is %q, want %q", status.State, StateQueued)
	}
}

// A queue that is down is a 503 with a retry-shaped message, not a 500.
func TestAnUnreachableQueueIsA503(t *testing.T) {
	h := newHTTPRig(t)
	h.queue.err = errors.New("redis is down")

	rec := h.do(t, http.MethodPost, h.createPath(), h.user)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("returned %d, want 503", rec.Code)
	}
	if h.queue.count() != 0 {
		t.Fatal("a failed enqueue still recorded a task")
	}
}

// ─── the rate limit ──────────────────────────────────────────────────

/*
A user who has had their allowance is refused, and refused cleanly.

This route starts a browser for up to ninety seconds. Unlimited, it
converts one account into as many concurrent Chrome processes as the
fleet will start, and a queue full of one person's renders is a
download that never arrives for anybody else. The specification lists
it: "Rate limit PDF generation (CPU-expensive and trivially
abusable)."
*/
func TestASecondBurstOfPDFRequestsIsRefused(t *testing.T) {
	h := newHTTPRig(t)
	h.limiter.allowed = 2

	for i := range 2 {
		if rec := h.do(t, http.MethodPost, h.createPath(), h.user); rec.Code != http.StatusAccepted {
			t.Fatalf("request %d returned %d, want 202", i+1, rec.Code)
		}
	}

	rec := h.do(t, http.MethodPost, h.createPath(), h.user)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the third request returned %d, want 429", rec.Code)
	}
	if retry := rec.Header().Get("Retry-After"); retry == "" {
		t.Error("a 429 with no Retry-After tells a client nothing about when to come back, " +
			"so a polling client retries immediately and stays refused")
	}

	// And nothing was started for it.
	if h.queue.count() != 2 {
		t.Fatalf("%d renders were queued, want 2 — a refused request still enqueued work",
			h.queue.count())
	}
}

/*
A refused request leaves nothing behind.

The limit is checked before the job id is minted and before the status
is written. Checking it after would fill Redis with "queued" jobs for
renders nobody will ever perform, and a client holding one of those
ids would poll it until the TTL ran out.
*/
func TestARefusedRequestWritesNoJobStatus(t *testing.T) {
	h := newHTTPRig(t)
	h.limiter.allowed = 0

	rec := h.do(t, http.MethodPost, h.createPath(), h.user)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("returned %d, want 429", rec.Code)
	}

	h.status.mu.Lock()
	written := len(h.status.values)
	h.status.mu.Unlock()

	if written != 0 {
		t.Fatalf("a refused request wrote %d job statuses. Nothing will ever run them, "+
			"and a client given one of those ids polls it until the TTL expires", written)
	}
}

// The limit is per USER, not per IP.
//
// An IP limit would punish everyone behind one mobile carrier's NAT,
// which in this product's market is a large share of the users. Asserted
// by reading the subject the limiter was asked about — a 429 alone would
// look identical either way.
func TestThePDFLimitIsKeyedOnTheUser(t *testing.T) {
	h := newHTTPRig(t)

	if rec := h.do(t, http.MethodPost, h.createPath(), h.user); rec.Code != http.StatusAccepted {
		t.Fatalf("returned %d", rec.Code)
	}

	subjects := h.limiter.subjects()
	if len(subjects) != 1 {
		t.Fatalf("the limiter was consulted %d times, want 1", len(subjects))
	}
	if subjects[0] != h.user.String() {
		t.Fatalf("limited on %q, want the user id %q", subjects[0], h.user)
	}
	if h.limiter.rules[0] != RenderLimit.Name {
		t.Fatalf("limited under rule %q, want %q — sharing a rule name with another "+
			"route would make the two share a budget", h.limiter.rules[0], RenderLimit.Name)
	}
}

/*
An unreachable limiter fails OPEN, and says so.

Matching the recompute route and the global throttle. The trade is
deliberate: the status store is the same Redis, so an outage already
means no render can report its result — refusing as well turns a
degraded feature into a broken one.

The log line is part of the behaviour, not decoration. A route that
silently stops being limited is one nobody finds out about.
*/
func TestAnUnreachableLimiterAllowsAndLogs(t *testing.T) {
	h := newHTTPRig(t)
	h.limiter.err = errors.New("redis is down")

	rec := h.do(t, http.MethodPost, h.createPath(), h.user)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("returned %d, want 202 — a Redis outage must not take downloads "+
			"down with it", rec.Code)
	}

	if logged := h.logs.String(); !strings.Contains(logged, "rate limiter unavailable") {
		t.Fatalf("the limiter failed open and logged nothing about it. Logs:\n%s", logged)
	}
}

// ─── GET ─────────────────────────────────────────────────────────────

func TestPollingReportsTheStatus(t *testing.T) {
	h := newHTTPRig(t)

	jobID := uuid.New()
	expires := time.Now().Add(SignedURLTTL)
	if err := h.status.Set(context.Background(), h.user, jobID, Status{
		State: StateDone, URL: "https://storage.example/kundli-x.pdf?sig=1", ExpiresAt: &expires,
	}); err != nil {
		t.Fatalf("seed status: %v", err)
	}

	rec := h.do(t, http.MethodGet, h.createPath()+"/"+jobID.String(), h.user)
	if rec.Code != http.StatusOK {
		t.Fatalf("returned %d: %s", rec.Code, rec.Body.String())
	}

	var got Status
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.State != StateDone || got.URL == "" {
		t.Fatalf("got %+v, want a done status with a URL", got)
	}

	// The body carries a signed URL to private birth data.
	if cc := rec.Header().Get("Cache-Control"); cc == "" || !strings.Contains(cc, "no-store") {
		t.Fatalf("Cache-Control is %q, want no-store — this response holds a signed "+
			"link to somebody's birth chart", cc)
	}
}

/*
Another user's job id is not readable.

This is the only authorisation on the polling route, and the
ownership middleware above cannot supply it: that middleware guards
the PROFILE in the path, and a job id is not a profile. A caller can
pass their own profile — which they own — and any job id they like.

The status key is composed from the authenticated user plus the job
id, so the lookup simply misses. 404, the same answer as for an id
that was invented, so polling cannot be used to learn that somebody
else's job is real.
*/
func TestAnotherUsersJobIsNotFound(t *testing.T) {
	h := newHTTPRig(t)

	stranger := uuid.New()
	jobID := uuid.New()

	// The stranger's job really does exist and really is complete.
	expires := time.Now().Add(SignedURLTTL)
	if err := h.status.Set(context.Background(), stranger, jobID, Status{
		State: StateDone, URL: "https://storage.example/kundli-private.pdf?sig=1", ExpiresAt: &expires,
	}); err != nil {
		t.Fatalf("seed status: %v", err)
	}

	rec := h.do(t, http.MethodGet, h.createPath()+"/"+jobID.String(), h.user)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("polling another user's job returned %d, want 404. Body: %s",
			rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "storage.example") {
		t.Fatal("the refusal leaked the stranger's signed URL")
	}
}

func TestAnUnknownOrMalformedJobIDIsNotFound(t *testing.T) {
	h := newHTTPRig(t)

	for name, jobID := range map[string]string{
		"never existed": uuid.NewString(),
		"not a uuid":    "not-a-uuid",
		"empty-ish":     "%20",
	} {
		t.Run(name, func(t *testing.T) {
			rec := h.do(t, http.MethodGet, h.createPath()+"/"+jobID, h.user)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("returned %d, want 404", rec.Code)
			}
		})
	}
}

// Neither route answers without a token. They sit inside the chart
// subtree in production, but a test that never checks is a test that
// would not notice them being moved out of it.
func TestNeitherPDFRouteAnswersWithoutAToken(t *testing.T) {
	h := newHTTPRig(t)

	for name, call := range map[string]func() *httptest.ResponseRecorder{
		"POST": func() *httptest.ResponseRecorder {
			return h.do(t, http.MethodPost, h.createPath(), uuid.Nil)
		},
		"GET": func() *httptest.ResponseRecorder {
			return h.do(t, http.MethodGet, h.createPath()+"/"+uuid.NewString(), uuid.Nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if rec := call(); rec.Code != http.StatusUnauthorized {
				t.Fatalf("answered %d with no Authorization header, want 401", rec.Code)
			}
		})
	}
	if h.queue.count() != 0 {
		t.Fatal("an unauthenticated request queued a render")
	}
}

// ─── locale ──────────────────────────────────────────────────────────

/*
The locale is allowlisted, not passed through.

It is interpolated into the URL the worker navigates to, which makes
it the one part of the print page's address a caller can influence.
An allowlist means the worst a caller can do is choose between two
languages.
*/
func TestTheLocaleIsAllowlisted(t *testing.T) {
	cases := map[string]string{
		"hi":                       "hi",
		"HI":                       "hi",
		"  hi  ":                   "hi",
		"en":                       "en",
		"":                         "en",
		"fr":                       "en",
		"../../etc/passwd":         "en",
		"en&token=stolen":          "en",
		"hi#/../admin":             "en",
		"https://evil.example/pwn": "en",
	}

	for raw, want := range cases {
		if got := normaliseLocale(raw); got != want {
			t.Errorf("normaliseLocale(%q) = %q, want %q", raw, got, want)
		}
	}
}
