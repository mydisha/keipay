package worker

import (
	"context"
	"errors"
	"fmt"

	"github.com/hibiken/asynq"
)

// Enqueuer wraps an asynq client to schedule webhook forward tasks.
type Enqueuer struct {
	client   *asynq.Client
	maxRetry int
}

// NewEnqueuer constructs an Enqueuer backed by a Redis connection.
func NewEnqueuer(redisOpt asynq.RedisClientOpt, maxRetry int) *Enqueuer {
	if maxRetry <= 0 {
		maxRetry = 5
	}
	return &Enqueuer{client: asynq.NewClient(redisOpt), maxRetry: maxRetry}
}

// EnqueueWebhookForward schedules forwarding of a delivery with retry policy.
// The task ID is derived from the delivery, so enqueuing a delivery that is
// already queued or running (e.g. by the sweeper) is a no-op.
func (e *Enqueuer) EnqueueWebhookForward(ctx context.Context, deliveryID int64) error {
	err := e.enqueue(ctx, deliveryID, asynq.TaskID(fmt.Sprintf("%s:%d", TypeWebhookForward, deliveryID)))
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}

// ReplayWebhookForward schedules an admin-requested re-send. It skips the task ID
// dedup because an archived (exhausted) task keeps holding that ID.
func (e *Enqueuer) ReplayWebhookForward(ctx context.Context, deliveryID int64) error {
	return e.enqueue(ctx, deliveryID)
}

func (e *Enqueuer) enqueue(ctx context.Context, deliveryID int64, opts ...asynq.Option) error {
	task, err := NewWebhookForwardTask(deliveryID)
	if err != nil {
		return err
	}
	opts = append(opts, asynq.MaxRetry(e.maxRetry), asynq.Queue("webhooks"))
	_, err = e.client.EnqueueContext(ctx, task, opts...)
	return err
}

// Close releases the underlying asynq client.
func (e *Enqueuer) Close() error { return e.client.Close() }
