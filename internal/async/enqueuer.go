package async

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"github.com/nikitkaralius/lineup/internal/jobs"
)

// Enqueuer abstracts async job enqueueing for the service.
// Implementations should be safe for concurrent use.
type Enqueuer interface {
	// EnqueueFinishPoll schedules a job to finalize a Telegram poll at the given time.
	EnqueueFinishPoll(ctx context.Context, args jobs.FinishPollArgs, runAt time.Time) error
	Close()
}

type RiverEnqueuer[TTx any] struct {
	client *river.Client[TTx]
}

// NewRiverEnqueuer wraps an existing River client (initialized by the service) for enqueueing jobs.
func NewRiverEnqueuer[TTx any](client *river.Client[TTx]) *RiverEnqueuer[TTx] {
	return &RiverEnqueuer[TTx]{client: client}
}

// Close is a no-op because the lifecycle of the underlying River client and DB pool
// is managed by the service.
func (e *RiverEnqueuer[TTx]) Close() {}

func (e *RiverEnqueuer[TTx]) EnqueueFinishPoll(ctx context.Context, args jobs.FinishPollArgs, runAt time.Time) error {
	opts := &river.InsertOpts{MaxAttempts: 1}
	if !runAt.IsZero() {
		// schedule if provided
		opts.ScheduledAt = runAt
	}
	_, err := e.client.Insert(ctx, args, opts)
	return err
}
