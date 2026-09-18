package pdf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/charts"
)

// Browser renders a URL to PDF bytes.
//
// An interface with one method, declared by the consumer, so the
// orchestration below — mint a token, render, upload, sign, record — can
// be tested without a browser. That matters more than usual here: every
// failure branch in this file is a branch a real Chrome will not enter
// on demand, and an untested failure branch is where a job gets stuck at
// "running" forever.
type Browser interface {
	PrintToPDF(ctx context.Context, pageURL string) ([]byte, error)
}

// Uploader is the storage surface this needs.
type Uploader interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error
	SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// Minter issues print tokens. Satisfied by *charts.PrintTokens.
type Minter interface {
	Mint(ctx context.Context, scope charts.PrintScope) (string, error)
}

// StatusWriter is the half of StatusStore the renderer uses.
//
// An interface rather than *StatusStore so the orchestration tests need
// no Redis. The write side is the only half the worker touches — it
// never reads a status back — and narrowing to that makes the direction
// of data flow legible from the type alone.
type StatusWriter interface {
	Set(ctx context.Context, userID, jobID uuid.UUID, status Status) error
}

// Renderer runs one render job.
type Renderer struct {
	browser  Browser
	uploader Uploader
	minter   Minter
	status   StatusWriter
	logger   *slog.Logger

	// webURL is the origin headless Chrome is pointed at. The API's own
	// view of where the web app is, never anything from the request.
	webURL string

	now func() time.Time
}

func NewRenderer(
	browser Browser,
	uploader Uploader,
	minter Minter,
	status StatusWriter,
	webURL string,
	logger *slog.Logger,
) (*Renderer, error) {
	if browser == nil || uploader == nil || minter == nil || status == nil {
		return nil, fmt.Errorf("pdf: renderer needs a browser, an uploader, a minter and a status store")
	}

	parsed, err := url.Parse(webURL)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("pdf: web url %q is not usable", webURL)
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Renderer{
		browser:  browser,
		uploader: uploader,
		minter:   minter,
		status:   status,
		webURL:   strings.TrimSuffix(webURL, "/"),
		logger:   logger,
		now:      time.Now,
	}, nil
}

// WithClock replaces the clock, so a test can assert on ExpiresAt.
func (r *Renderer) WithClock(now func() time.Time) *Renderer {
	if now != nil {
		r.now = now
	}
	return r
}

// Handle is the asynq handler.
func (r *Renderer) Handle(ctx context.Context, task *asynq.Task) error {
	payload, err := ParseRenderPayload(task.Payload())
	if err != nil {
		// Unparseable payload will never parse. Retrying it three times
		// costs three browsers and changes nothing, so it is skipped.
		return fmt.Errorf("%w: %w", asynq.SkipRetry, err)
	}

	/*
	   Its own deadline, inside asynq's.

	   asynq's Timeout cancels the context and moves on. If that were the
	   only bound, a stuck render would be killed with its status still
	   reading "running", and the client would poll that forever — the
	   worst failure mode available, because it looks like progress.

	   Bounding it here means the code below always reaches the branch
	   that records "failed".
	*/
	ctx, cancel := context.WithTimeout(ctx, RenderTimeout)
	defer cancel()

	if err := r.status.Set(ctx, payload.UserID, payload.JobID, Status{State: StateRunning}); err != nil {
		// Not fatal to the render — the document is still worth making,
		// and the terminal Set below is the one the client needs.
		r.logger.WarnContext(ctx, "could not mark pdf job running",
			slog.String("job_id", payload.JobID.String()), slog.Any("err", err))
	}

	status, err := r.render(ctx, payload)
	if err != nil {
		r.logger.ErrorContext(ctx, "pdf render failed",
			slog.String("job_id", payload.JobID.String()),
			// IDs, never the profile. And never the page URL: it carries
			// a print token.
			slog.String("user_id", payload.UserID.String()),
			slog.Any("err", err))

		/*
		   Record the failure before returning it.

		   Written with a context that is NOT the one just cancelled: on
		   a timeout, ctx is already dead and this write would fail too,
		   leaving the status at "running" — the exact outcome the
		   deadline above exists to prevent.
		*/
		writeCtx, writeCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer writeCancel()

		if setErr := r.status.Set(writeCtx, payload.UserID, payload.JobID, Status{
			State:   StateFailed,
			Message: "We could not build your PDF. Please try again.",
		}); setErr != nil {
			r.logger.ErrorContext(writeCtx, "could not record pdf failure",
				slog.String("job_id", payload.JobID.String()), slog.Any("err", setErr))
		}
		return err
	}

	if err := r.status.Set(ctx, payload.UserID, payload.JobID, status); err != nil {
		// The document exists but nobody can find it. Returning the error
		// lets asynq retry, which re-renders — wasteful, but the
		// alternative is a job that silently never completes.
		return fmt.Errorf("pdf: record completed job: %w", err)
	}

	r.logger.InfoContext(ctx, "pdf rendered",
		slog.String("job_id", payload.JobID.String()))
	return nil
}

// render does the work and returns the terminal status.
func (r *Renderer) render(ctx context.Context, payload RenderPayload) (Status, error) {
	token, err := r.minter.Mint(ctx, charts.PrintScope{
		UserID:    payload.UserID,
		ProfileID: payload.ProfileID,
	})
	if err != nil {
		return Status{}, fmt.Errorf("pdf: mint print token: %w", err)
	}

	pageURL := r.printURL(token, payload.Locale)

	body, err := r.browser.PrintToPDF(ctx, pageURL)
	if err != nil {
		// Deliberately not wrapping pageURL into the message: it contains
		// the print token, and this error is logged.
		return Status{}, fmt.Errorf("pdf: render page: %w", err)
	}
	if len(body) == 0 {
		return Status{}, fmt.Errorf("pdf: render produced no bytes")
	}

	key := objectKey()
	if err := r.uploader.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "application/pdf"); err != nil {
		return Status{}, fmt.Errorf("pdf: upload: %w", err)
	}

	signed, err := r.uploader.SignedURL(ctx, key, SignedURLTTL)
	if err != nil {
		return Status{}, fmt.Errorf("pdf: sign: %w", err)
	}

	expires := r.now().Add(SignedURLTTL)
	return Status{State: StateDone, URL: signed, ExpiresAt: &expires}, nil
}

// printURL builds the page headless Chrome will load.
//
// The token goes in the query string because that is the only channel a
// browser navigation has — there is no way to attach a header to
// chromedp's Navigate. Which is exactly why the token is single use and
// expires in five minutes: it WILL end up in a log.
func (r *Renderer) printURL(token, locale string) string {
	query := url.Values{}
	query.Set("token", token)
	if locale != "" {
		query.Set("locale", locale)
	}
	return r.webURL + "/kundli/print?" + query.Encode()
}

/*
The object key carries no PII, and no identifier either.

`kundli-{random}.pdf`, not `kundli-{profile}.pdf` and not the user's
name. Two reasons, and the second is the one that bites:

 1. Object keys turn up in bucket listings, access logs and CDN
    metrics — none of which are places birth details belong.
 2. A key derived from an id the user has already seen is guessable.
    The object is private and reached by a signed URL, so guessing it
    is not immediately fatal, but "you need the signature AND the
    key" is a strictly better position than "you need the signature".
*/
func objectKey() string {
	return "kundli-" + uuid.NewString() + ".pdf"
}
