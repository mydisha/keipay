package worker

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/logger"
)

// ForwardEnqueuer schedules a delivery for forwarding.
type ForwardEnqueuer interface {
	EnqueueWebhookForward(ctx context.Context, deliveryID int64) error
}

// Sweeper re-enqueues deliveries that were committed but never attempted, e.g.
// because Redis was unavailable right after the webhook was processed. With the
// per-delivery task ID in Enqueuer, a delivery still in the queue is not doubled.
type Sweeper struct {
	webhooks domain.WebhookRepository
	enqueuer ForwardEnqueuer
	log      *zap.Logger
	interval time.Duration
	minAge   time.Duration
	batch    int
}

// NewSweeper constructs a Sweeper that runs every minute and picks up deliveries
// older than a minute, so it never races the normal post-commit enqueue.
func NewSweeper(webhooks domain.WebhookRepository, enqueuer ForwardEnqueuer, log *zap.Logger) *Sweeper {
	return &Sweeper{webhooks: webhooks, enqueuer: enqueuer, log: log, interval: time.Minute, minAge: time.Minute, batch: 100}
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

// SweepOnce enqueues one batch of unsent deliveries and returns how many it enqueued.
func (s *Sweeper) SweepOnce(ctx context.Context) (int, error) {
	ds, err := s.webhooks.ListUnsentDeliveries(ctx, time.Now().Add(-s.minAge), s.batch)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, d := range ds {
		if err := s.enqueuer.EnqueueWebhookForward(ctx, d.ID); err != nil {
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
