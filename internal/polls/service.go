package polls

import (
	"context"
	"fmt"
	"time"

	"github.com/nikitkaralius/lineup/internal/jobs"
	"github.com/riverqueue/river"
)

type Service interface {
	SchedulePollFinish(ctx context.Context, args jobs.FinishPollArgs, runAt time.Time) error
}

type pollService[TTx any] struct {
	client *river.Client[TTx]
}

func NewPollsService[TTx any](client *river.Client[TTx]) Service {
	return &pollService[TTx]{client: client}
}

func (r *pollService[TTx]) SchedulePollFinish(ctx context.Context, args jobs.FinishPollArgs, runAt time.Time) error {
	opts := &river.InsertOpts{MaxAttempts: 1}
	if !runAt.IsZero() {
		return fmt.Errorf("runAt must be non zero")
	}
	opts.ScheduledAt = runAt
	_, err := r.client.Insert(ctx, args, opts)
	return err
}
