package worker

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/logger"
)

// AttemptScheduler queues one attempt of a delivery and returns
// ErrTaskExists when the attempt's task id is taken.
type AttemptScheduler interface {
	Schedule(ctx context.Context, deliveryID int64, attempt int, manual bool, at time.Time) error
}

// TaskDeleter removes a queued task; asynq.Inspector implements it. Deleting
// a running task fails.
type TaskDeleter interface {
	DeleteTask(queue, id string) error
}

// Sweeper re-enqueues deliveries whose next attempt is overdue: the enqueue
// after commit failed, or the task was lost or archived after asynq's own
// retries ran out.
type Sweeper struct {
	deliveries domain.DeliveryRepository
	scheduler  AttemptScheduler
	tasks      TaskDeleter
	log        *zap.Logger
	interval   time.Duration
	minAge     time.Duration
	batch      int
	now        func() time.Time
}

// NewSweeper constructs a Sweeper that runs every minute and picks up
// deliveries more than two minutes overdue, so it never races the normal
// enqueue of an attempt.
func NewSweeper(deliveries domain.DeliveryRepository, scheduler AttemptScheduler, tasks TaskDeleter, log *zap.Logger) *Sweeper {
	return &Sweeper{deliveries: deliveries, scheduler: scheduler, tasks: tasks, log: log,
		interval: time.Minute, minAge: 2 * time.Minute, batch: 100, now: time.Now}
}

// Run sweeps on every tick until ctx is cancelled.
func (s *Sweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.SweepOnce(ctx); err != nil && ctx.Err() == nil {
				s.log.Error("webhook delivery sweep failed",
					zap.String(logger.FieldLogType, logger.LogTypeWebhook),
					zap.String(logger.FieldEvent, "webhook.outbound.sweep_failed"),
					zap.Error(err),
				)
			}
		}
	}
}

// SweepOnce enqueues the next attempt of one batch of overdue deliveries and
// returns how many it enqueued. A task that still holds the attempt's id (an
// archived one, typically) is deleted and replaced; a running one is left
// alone.
func (s *Sweeper) SweepOnce(ctx context.Context) (int, error) {
	now := s.now()
	ds, err := s.deliveries.ListDue(ctx, now.Add(-s.minAge), s.batch)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, d := range ds {
		attempt := d.Attempts + 1
		err := s.scheduler.Schedule(ctx, d.ID, attempt, false, now)
		if errors.Is(err, ErrTaskExists) {
			if derr := s.tasks.DeleteTask(QueueWebhooks, ForwardTaskID(d.ID, attempt)); derr != nil {
				s.log.Debug("overdue delivery task kept",
					zap.String(logger.FieldLogType, logger.LogTypeWebhook),
					zap.String("delivery_id", d.PublicID), zap.Error(derr))
				continue
			}
			err = s.scheduler.Schedule(ctx, d.ID, attempt, false, now)
		}
		if err != nil {
			return n, err
		}
		n++
	}
	if n > 0 {
		s.log.Warn("webhook deliveries re-enqueued by sweeper",
			zap.String(logger.FieldLogType, logger.LogTypeWebhook),
			zap.String(logger.FieldEvent, "webhook.outbound.swept"),
			zap.Int("count", n),
		)
	}
	return n, nil
}
