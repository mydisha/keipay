package service

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/logger"
)

// DeliveryDeps are the collaborators of a DeliveryService.
type DeliveryDeps struct {
	Deliveries domain.DeliveryRepository
	Endpoints  domain.WebhookEndpointRepository
	Tx         domain.TxManager
	Enqueuer   Enqueuer
	Audit      Auditor
	Log        *zap.Logger
}

// DeliveryService lists, shows and replays outbound webhook deliveries.
type DeliveryService struct {
	deliveries domain.DeliveryRepository
	endpoints  domain.WebhookEndpointRepository
	tx         domain.TxManager
	enqueuer   Enqueuer
	audit      Auditor
	log        *zap.Logger
	now        func() time.Time
}

// NewDeliveryService constructs a DeliveryService.
func NewDeliveryService(d DeliveryDeps) *DeliveryService {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &DeliveryService{deliveries: d.Deliveries, endpoints: d.Endpoints, tx: d.Tx, enqueuer: d.Enqueuer,
		audit: d.Audit, log: log, now: time.Now}
}

// deliverySnapshot is the audited state of a delivery when it was replayed.
type deliverySnapshot struct {
	Status   domain.WebhookDeliveryStatus `json:"status"`
	Attempts int                          `json:"attempts"`
}

// List returns one page of deliveries, newest first.
func (s *DeliveryService) List(ctx context.Context, f domain.DeliveryFilter, req domain.PageRequest) (domain.PageResult[domain.WebhookDelivery], error) {
	p, err := req.Page()
	if err != nil {
		return domain.PageResult[domain.WebhookDelivery]{}, err
	}
	res, err := s.deliveries.List(ctx, f, p)
	if err != nil {
		return domain.PageResult[domain.WebhookDelivery]{}, fmt.Errorf("list deliveries: %w", err)
	}
	return res, nil
}

// Get returns a delivery with its body and attempts, oldest first.
func (s *DeliveryService) Get(ctx context.Context, publicID string) (*domain.WebhookDelivery, []domain.DeliveryAttempt, error) {
	d, err := s.deliveries.GetByPublicID(ctx, publicID)
	if err != nil {
		return nil, nil, fmt.Errorf("get delivery: %w", err)
	}
	attempts, err := s.deliveries.Attempts(ctx, d.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("get delivery attempts: %w", err)
	}
	return d, attempts, nil
}

// Replay allows one more attempt of a DELIVERED or FAILED delivery, with the
// same event, body and endpoint, and queues it. The attempt is manual: it is
// not retried and never counts toward automatic disabling.
func (s *DeliveryService) Replay(ctx context.Context, publicID string) (*domain.WebhookDelivery, error) {
	var d *domain.WebhookDelivery
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		d, err = s.deliveries.GetByPublicIDForUpdate(ctx, publicID)
		if err != nil {
			return err
		}
		if err := s.replayable(ctx, d); err != nil {
			return err
		}
		before := deliverySnapshot{Status: d.Status, Attempts: d.Attempts}
		now := s.now().UTC().Truncate(time.Microsecond)
		d.Status, d.MaxAttempts, d.NextAttemptAt = domain.WebhookDeliveryPending, d.Attempts+1, &now
		if err := s.deliveries.Requeue(ctx, d); err != nil {
			return err
		}
		target := domain.AuditTarget{Type: TargetWebhookDelivery, ID: d.PublicID}
		return s.audit.Record(ctx, ActionWebhookDeliveryReplay, target, before, nil)
	})
	if err != nil {
		return nil, fmt.Errorf("replay delivery: %w", err)
	}
	if err := s.enqueuer.EnqueueDelivery(ctx, d.ID, d.Attempts+1, true, *d.NextAttemptAt); err != nil {
		// The delivery is PENDING again; the sweeper sends it.
		logger.Ctx(ctx, s.log).Warn("replay enqueue deferred",
			zap.String(logger.FieldLogType, logger.LogTypeWebhook),
			zap.String(logger.FieldEvent, "webhook.outbound.enqueue_deferred"),
			zap.String("delivery_id", d.PublicID),
			zap.Error(err),
		)
	}
	return d, nil
}

// replayable refuses a delivery that is still in progress or whose endpoint
// can no longer receive it. The endpoint is locked like the delivery, so it
// cannot be disabled or deleted before the replay commits.
func (s *DeliveryService) replayable(ctx context.Context, d *domain.WebhookDelivery) error {
	if d.Status == domain.WebhookDeliveryPending {
		return domain.DeliveryNotReplayable(domain.ReplayReasonInProgress)
	}
	if d.EndpointID == nil {
		return domain.DeliveryNotReplayable(domain.ReplayReasonNoEndpoint)
	}
	e, err := s.endpoints.GetByIDForUpdate(ctx, *d.EndpointID)
	if err != nil {
		return err
	}
	switch {
	case e.DeletedAt != nil:
		return domain.DeliveryNotReplayable(domain.ReplayReasonEndpointDeleted)
	case e.Status != domain.WebhookEndpointActive:
		return domain.DeliveryNotReplayable(domain.ReplayReasonDisabled)
	}
	return nil
}

// InboundService lists and shows inbound gateway webhooks.
type InboundService struct {
	inbound domain.InboundWebhookRepository
}

// NewInboundService constructs an InboundService.
func NewInboundService(inbound domain.InboundWebhookRepository) *InboundService {
	return &InboundService{inbound: inbound}
}

// List returns one page of inbound webhooks, newest first.
func (s *InboundService) List(ctx context.Context, f domain.InboundFilter, req domain.PageRequest) (domain.PageResult[domain.InboundWebhook], error) {
	p, err := req.Page()
	if err != nil {
		return domain.PageResult[domain.InboundWebhook]{}, err
	}
	res, err := s.inbound.List(ctx, f, p)
	if err != nil {
		return domain.PageResult[domain.InboundWebhook]{}, fmt.Errorf("list inbound webhooks: %w", err)
	}
	return res, nil
}

// Get returns an inbound webhook with its raw body.
func (s *InboundService) Get(ctx context.Context, publicID string) (*domain.InboundWebhook, error) {
	w, err := s.inbound.GetByPublicID(ctx, publicID)
	if err != nil {
		return nil, fmt.Errorf("get inbound webhook: %w", err)
	}
	return w, nil
}
