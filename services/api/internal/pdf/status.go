package pdf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

// State is where a render has got to.
type State string

const (
	StateQueued  State = "queued"
	StateRunning State = "running"
	StateDone    State = "done"
	StateFailed  State = "failed"
)

// ErrNoJob means there is no such job for this user.
//
// Deliberately the same answer for "never existed", "expired" and
// "belongs to somebody else". A caller must not be able to learn that
// another user's job id is real by polling it.
var ErrNoJob = errors.New("pdf: no such job")

// Status is what a polling client sees.
type Status struct {
	State State `json:"status"`

	// URL is the signed download link, present only when done.
	URL string `json:"url,omitempty"`

	// Message is a short, human, generic sentence for a failed job.
	// Never the underlying error: that names buckets, hosts and, on a
	// chromedp timeout, the URL — which carries a print token.
	Message string `json:"message,omitempty"`

	// ExpiresAt tells the UI when the link stops working, so it can say
	// so rather than letting the user discover it from a 403.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// StatusStore keeps job status in Redis.
//
// Redis rather than Postgres, and this is the one place in the service
// where that is not a violation of the single-writer rule: a render job
// is not durable state. It is a few minutes of progress on work that can
// simply be requested again. Nothing downstream reads it, no report is
// built from it, and losing it to a Redis restart costs one re-click.
type StatusStore struct {
	rdb goredis.UniversalClient
}

func NewStatusStore(rdb goredis.UniversalClient) *StatusStore {
	return &StatusStore{rdb: rdb}
}

/*
The key namespaces by USER as well as by job.

This is the authorisation, not a tidiness choice. The polling endpoint
composes the key from the authenticated caller plus the job id in the
URL, so a caller asking about somebody else's job looks up a key that
does not exist and gets ErrNoJob — the same answer as for a job id
they invented.

With the user left out, any job id would be readable by anyone
holding it, and the response carries a signed URL to a stranger's
birth chart. There would be no second check to catch it: the
ownership middleware guards the PROFILE in the path, and a job id is
not a profile.
*/
func statusKey(userID, jobID uuid.UUID) string {
	return "pdf:job:" + userID.String() + ":" + jobID.String()
}

// Set writes the status, replacing whatever was there.
func (s *StatusStore) Set(ctx context.Context, userID, jobID uuid.UUID, status Status) error {
	body, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("pdf: marshal status: %w", err)
	}

	if err := s.rdb.Set(ctx, statusKey(userID, jobID), body, StatusTTL).Err(); err != nil {
		return fmt.Errorf("pdf: write job status: %w", err)
	}
	return nil
}

// Get reads the status, or ErrNoJob.
func (s *StatusStore) Get(ctx context.Context, userID, jobID uuid.UUID) (Status, error) {
	raw, err := s.rdb.Get(ctx, statusKey(userID, jobID)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return Status{}, ErrNoJob
	}
	if err != nil {
		return Status{}, fmt.Errorf("pdf: read job status: %w", err)
	}

	var status Status
	if err := json.Unmarshal(raw, &status); err != nil {
		// A corrupt value is not a missing one, but the caller can do
		// nothing different about it, and it must not surface as a 200
		// with an empty state that the UI renders as "queued" forever.
		return Status{}, fmt.Errorf("pdf: decode job status: %w", err)
	}
	return status, nil
}
