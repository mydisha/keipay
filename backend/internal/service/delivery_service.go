package service

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// Replayer re-enqueues an outbound webhook delivery on admin request.
type Replayer interface {
	ReplayWebhookForward(ctx context.Context, deliveryID int64) error
}

// DeliveryService lists and replays outbound webhook deliveries.
type DeliveryService struct {
	webhooks domain.WebhookRepository
	replayer Replayer
	audit    Auditor
}

// NewDeliveryService constructs a DeliveryService.
func NewDeliveryService(webhooks domain.WebhookRepository, replayer Replayer, audit Auditor) *DeliveryService {
	return &DeliveryService{webhooks: webhooks, replayer: replayer, audit: audit}
}

// deliverySnapshot is the audited state of a delivery when it was replayed.
type deliverySnapshot struct {
	Status   domain.WebhookDeliveryStatus `json:"status"`
	Attempts int                          `json:"attempts"`
}

// List returns deliveries filtered by app and/or status, newest first.
func (s *DeliveryService) List(ctx context.Context, f domain.DeliveryFilter) ([]domain.WebhookDelivery, error) {
	return s.webhooks.ListDeliveries(ctx, f)
}

// Replay re-enqueues a delivery and records it. The queue is not
// transactional, so the entry is written only once the enqueue succeeded.
func (s *DeliveryService) Replay(ctx context.Context, id int64) error {
	d, err := s.webhooks.GetDelivery(ctx, id)
	if err != nil {
		return fmt.Errorf("load delivery %d: %w", id, err)
	}
	if err := s.replayer.ReplayWebhookForward(ctx, id); err != nil {
		return fmt.Errorf("enqueue replay of delivery %d: %w", id, err)
	}
	target := domain.AuditTarget{Type: TargetWebhookDelivery, ID: strconv.FormatInt(id, 10)}
	if err := s.audit.Record(ctx, ActionWebhookDeliveryReplay, target,
		deliverySnapshot{Status: d.Status, Attempts: d.Attempts}, nil); err != nil {
		return fmt.Errorf("record replay of delivery %d: %w", id, err)
	}
	return nil
}
