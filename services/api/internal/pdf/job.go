// Package pdf turns a chart into a downloadable document.
//
// ── Why a real browser ──
//
// The PDF is produced by driving headless Chrome at the app's own print
// route rather than by a Go PDF library. A library would mean a second
// implementation of the chart — its geometry, its glyph placement, its
// typography — maintained in parallel with the one on screen, and the
// two would drift. They would drift silently, because nobody looks at a
// PDF as often as they look at a screen.
//
// Driving the real page costs a browser in the worker image. It buys a
// document that cannot disagree with the product.
//
// ── Why a queue ──
//
// A render is seconds, not milliseconds, and it holds a browser process
// while it runs. Doing it inside the HTTP request would tie up a request
// slot per download and time out under any load at all. So: enqueue,
// return a job id, poll.
//
// It gets its own asynq queue for the same reason — a slow render must
// not sit in front of the transit refresh.
package pdf

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/jobs"
)

const (
	// TypeRender is the asynq task type.
	TypeRender = "pdf:render"
)

// QueuePDF is the queue renders land on.
//
// Re-exported from the jobs registry rather than declared here as a
// second string literal. Two literals is how a task ends up enqueued to
// a queue the server does not consume — which fails silently: the
// enqueue succeeds, the client polls "queued" forever, and nothing is
// logged because nothing went wrong.
const QueuePDF = jobs.QueuePDF

const (
	// RenderTimeout bounds one render.
	//
	// Generous, because it covers Chrome starting, the page loading, the
	// chart's fonts arriving and the print stylesheet settling. A render
	// that has not finished in ninety seconds is stuck rather than slow,
	// and the job should fail so the user sees an error instead of a
	// spinner that never resolves.
	RenderTimeout = 90 * time.Second

	// RenderMaxRetry is deliberately low.
	//
	// Unlike the transit refresh, a user is watching. Three attempts at
	// asynq's backoff is well inside the time somebody will wait; more
	// would mean the job is still retrying long after they gave up, and
	// each attempt costs a browser.
	RenderMaxRetry = 3

	// SignedURLTTL is how long the download link works.
	//
	// The specification's twenty-four hours. Long enough to mail the link
	// to yourself and open it on a laptop; short enough that a link
	// pasted somewhere public expires before most people find it.
	SignedURLTTL = 24 * time.Hour

	// StatusTTL is how long a finished job's status is readable.
	//
	// Longer than SignedURLTTL by an hour, so a client polling right at
	// the boundary gets "your link expired" from a 403 on the object
	// rather than "no such job", which reads like the render never
	// happened.
	StatusTTL = SignedURLTTL + time.Hour
)

// RenderPayload is what the worker needs to render one document.
//
// It carries identifiers only. No birth date, no place, no name: the
// payload sits in Redis and in asynq's web UI, and birth date plus birth
// time plus birth place is close enough to a unique identifier that the
// security rules treat it exactly like an email address.
type RenderPayload struct {
	// JobID is minted by the API and returned to the caller, who polls
	// on it. Also the asynq task id, so an enqueue that is retried by a
	// flaky client collapses onto the same task rather than starting a
	// second browser.
	JobID uuid.UUID `json:"job_id"`

	// UserID and ProfileID are what the print token will be scoped to.
	UserID    uuid.UUID `json:"user_id"`
	ProfileID uuid.UUID `json:"profile_id"`

	// Locale is the document's language. Carried on the payload rather
	// than read from the profile because it is a property of the request
	// — the same chart is printed in Hindi by one reader and English by
	// another, and the worker has no session to ask.
	Locale string `json:"locale"`
}

// NewRenderTask builds the queued task.
//
// asynq.TaskID(JobID), not asynq.Unique: the two solve different
// problems and only one of them is right here.
//
//	Unique(ttl)  deduplicates by PAYLOAD for a window. Two different
//	             users asking for their own PDFs have different payloads,
//	             so it would not collide — but one user asking twice in
//	             the window would be silently refused, with no job id to
//	             poll, which reads to them as a broken button.
//
//	TaskID(id)   deduplicates by the id WE minted. A retried HTTP request
//	             carrying the same job id is one task; a genuine second
//	             request gets a new id and a new render.
//
// The caller decides which of those two a request is, which is the right
// place for that decision — not here, and not in Redis.
func NewRenderTask(payload RenderPayload) (*asynq.Task, error) {
	if payload.JobID == uuid.Nil || payload.UserID == uuid.Nil || payload.ProfileID == uuid.Nil {
		return nil, fmt.Errorf("pdf: render task needs a job, a user and a profile")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("pdf: marshal render payload: %w", err)
	}

	return asynq.NewTask(TypeRender, body,
		asynq.Queue(QueuePDF),
		asynq.TaskID(payload.JobID.String()),
		asynq.MaxRetry(RenderMaxRetry),
		// Slack over RenderTimeout: the handler enforces its own deadline
		// and wants to record "failed" before asynq kills it. A task
		// killed by asynq leaves the status stuck at "running" forever.
		asynq.Timeout(RenderTimeout+15*time.Second),
		asynq.Retention(StatusTTL),
	), nil
}

// ParseRenderPayload reads a task body back.
func ParseRenderPayload(body []byte) (RenderPayload, error) {
	var payload RenderPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return RenderPayload{}, fmt.Errorf("pdf: parse render payload: %w", err)
	}
	if payload.JobID == uuid.Nil || payload.UserID == uuid.Nil || payload.ProfileID == uuid.Nil {
		return RenderPayload{}, fmt.Errorf("pdf: render payload is missing an identifier")
	}
	return payload, nil
}
