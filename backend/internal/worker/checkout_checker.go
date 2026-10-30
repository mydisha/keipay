package worker

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/pkg/logger"
)

// OverdueChecker reads back one batch of expired checkouts;
// service.CheckoutCheckService implements it.
type OverdueChecker interface {
	CheckOverdue(ctx context.Context) (int, error)
}

// CheckoutChecker reads back expired checkouts every minute, so a payment
// whose callback never arrived still reaches its final status.
type CheckoutChecker struct {
	checks   OverdueChecker
	log      *zap.Logger
	interval time.Duration
}

// NewCheckoutChecker constructs a CheckoutChecker that runs every minute.
func NewCheckoutChecker(checks OverdueChecker, log *zap.Logger) *CheckoutChecker {
	return &CheckoutChecker{checks: checks, log: log, interval: time.Minute}
}

// Run checks on every tick until ctx is cancelled.
func (c *CheckoutChecker) Run(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := c.checks.CheckOverdue(ctx); err != nil && ctx.Err() == nil {
				c.log.Error("checkout check failed",
					zap.String(logger.FieldLogType, logger.LogTypeWebhook),
					zap.String(logger.FieldEvent, "checkout.check.list_failed"),
					zap.Error(err),
				)
			}
		}
	}
}
