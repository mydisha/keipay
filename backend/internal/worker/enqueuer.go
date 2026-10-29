package worker

import (
	"context"
	"errors"
	"time"

	"github.com/hibiken/asynq"
)

// ErrTaskExists means the attempt's task id is still held by a queued,
// running, retrying or archived task.
var ErrTaskExists = asynq.ErrTaskIDConflict

// infraMaxRetry bounds asynq's own retries of one attempt. They cover only
// infrastructure failures (database, queue); failed HTTP attempts are
// rescheduled by the forwarder per the endpoint's retry policy.
const infraMaxRetry = 10

// Enqueuer wraps an asynq client to schedule delivery attempts.
type Enqueuer struct {
	client *asynq.Client
}

// NewEnqueuer constructs an Enqueuer backed by a Redis connection.
func NewEnqueuer(redisOpt asynq.RedisClientOpt) *Enqueuer {
	return &Enqueuer{client: asynq.NewClient(redisOpt)}
}

// Schedule queues one attempt of a delivery to run at at. An attempt whose
// task id is taken returns ErrTaskExists.
func (e *Enqueuer) Schedule(ctx context.Context, deliveryID int64, attempt int, manual bool, at time.Time) error {
	task, err := NewWebhookForwardTask(deliveryID, attempt, manual)
	if err != nil {
		return err
	}
	_, err = e.client.EnqueueContext(ctx, task, asynq.TaskID(ForwardTaskID(deliveryID, attempt)),
		asynq.Queue(QueueWebhooks), asynq.MaxRetry(infraMaxRetry), asynq.ProcessAt(at))
	return err
}

// EnqueueDelivery is Schedule where an attempt that is already queued is a
// no-op.
func (e *Enqueuer) EnqueueDelivery(ctx context.Context, deliveryID int64, attempt int, manual bool, at time.Time) error {
	err := e.Schedule(ctx, deliveryID, attempt, manual, at)
	if errors.Is(err, ErrTaskExists) {
		return nil
	}
	return err
}

// Close releases the underlying asynq client.
func (e *Enqueuer) Close() error { return e.client.Close() }
