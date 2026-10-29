package domain

import (
	"context"
	"fmt"
	"time"
)

// PaymentFilter narrows payment lists. AppPublicID is a canonical app_ id.
type PaymentFilter struct {
	AppPublicID *string
	Status      *PaymentStatus
	From        *time.Time
	To          *time.Time
}

// Payment insert conflicts the service tells apart, so it can re-read the
// payment that won a race. Both match ErrConflict.
var (
	ErrDuplicateOrderID        = fmt.Errorf("%w: order_id already used by this app", ErrConflict)
	ErrDuplicateIdempotencyKey = fmt.Errorf("%w: idempotency key already used by this app", ErrConflict)
)

// PaymentRepository persists payments. Reads fill Payment.App.
type PaymentRepository interface {
	// Create inserts p. A duplicate order or idempotency key within the app
	// returns ErrDuplicateOrderID or ErrDuplicateIdempotencyKey.
	Create(ctx context.Context, p *Payment) error
	UpdateStatus(ctx context.Context, id int64, status PaymentStatus, paidAt, settledAt *time.Time, raw []byte) error
	// GetByReferenceForUpdate loads the payment whose external_id equals ref
	// and row-locks it until the surrounding transaction ends.
	GetByReferenceForUpdate(ctx context.Context, ref string) (*Payment, error)
	GetByPublicID(ctx context.Context, appID int64, publicID string) (*Payment, error)
	GetByID(ctx context.Context, appID, id int64) (*Payment, error)
	GetByOrderID(ctx context.Context, appID int64, orderID string) (*Payment, error)
	GetByIdempotencyKey(ctx context.Context, appID int64, key string) (*Payment, error)
	// List returns payments newest first (created_at, then public id).
	List(ctx context.Context, f PaymentFilter, p Page) (PageResult[Payment], error)
}

// WebhookEndpointRepository persists webhook endpoints. Every method joins
// the transaction carried by ctx; reads fill WebhookEndpoint.App. Lookups by
// public id skip deleted endpoints and return ErrNotFound for them.
type WebhookEndpointRepository interface {
	Create(ctx context.Context, e *WebhookEndpoint) error
	// Update writes every mutable column of e and refreshes UpdatedAt.
	Update(ctx context.Context, e *WebhookEndpoint) error
	// UpdateHealth writes the failure counter and status columns of e. It
	// refreshes UpdatedAt only when the status changes.
	UpdateHealth(ctx context.Context, e *WebhookEndpoint) error
	GetByPublicID(ctx context.Context, publicID string) (*WebhookEndpoint, error)
	// GetByPublicIDForUpdate also row-locks the endpoint until the transaction ends.
	GetByPublicIDForUpdate(ctx context.Context, publicID string) (*WebhookEndpoint, error)
	// GetByIDForUpdate loads an endpoint by internal id, deleted ones
	// included, and row-locks it until the transaction ends.
	GetByIDForUpdate(ctx context.Context, id int64) (*WebhookEndpoint, error)
	// List returns the app's endpoints newest first (created_at, then public id).
	List(ctx context.Context, appID int64, p Page) (PageResult[WebhookEndpoint], error)
	// CountByApp counts the app's endpoints that are not deleted.
	CountByApp(ctx context.Context, appID int64) (int, error)
	// ListActiveByApp returns the app's active endpoints that are not
	// deleted, oldest first.
	ListActiveByApp(ctx context.Context, appID int64) ([]WebhookEndpoint, error)
}

// EventRepository persists events.
type EventRepository interface {
	// Create inserts e and fills in its id.
	Create(ctx context.Context, e *Event) error
}

// DeliveryRepository persists outbound deliveries and their attempts. Reads
// by public id and List fill App, PaymentPublicID, Endpoint and Event.
type DeliveryRepository interface {
	// Create inserts d and fills in its id and timestamps.
	Create(ctx context.Context, d *WebhookDelivery) error
	GetByPublicID(ctx context.Context, publicID string) (*WebhookDelivery, error)
	// GetByPublicIDForUpdate also row-locks the delivery until the transaction ends.
	GetByPublicIDForUpdate(ctx context.Context, publicID string) (*WebhookDelivery, error)
	// List returns deliveries newest first (created_at, then public id),
	// without their payload.
	List(ctx context.Context, f DeliveryFilter, p Page) (PageResult[WebhookDelivery], error)
	// Attempts returns the delivery's attempts, oldest first.
	Attempts(ctx context.Context, deliveryID int64) ([]DeliveryAttempt, error)
	// LoadForSend loads a delivery by internal id with its event reference
	// and its endpoint, nil when it has none, in one query.
	LoadForSend(ctx context.Context, id int64) (*WebhookDelivery, *WebhookEndpoint, error)
	// Requeue writes the status, max_attempts and next_attempt_at of d and
	// refreshes UpdatedAt.
	Requeue(ctx context.Context, d *WebhookDelivery) error
	// Fail ends a PENDING delivery without an attempt. It reports false when
	// the delivery was no longer PENDING.
	Fail(ctx context.Context, id int64, lastErr string) (bool, error)
	// RecordAttempt stores a and the new state of d (status, attempts,
	// next_attempt_at, last_*), but only while the stored delivery is still
	// PENDING with prevAttempts attempts. It reports false when another
	// worker got there first, writing nothing.
	RecordAttempt(ctx context.Context, d *WebhookDelivery, prevAttempts int, a *DeliveryAttempt) (bool, error)
	// ListDue returns PENDING deliveries whose next attempt was due before
	// olderThan, oldest first.
	ListDue(ctx context.Context, olderThan time.Time, limit int) ([]WebhookDelivery, error)
}

// InboundWebhookRepository persists the log of gateway callbacks, which is
// also the durable callback dedupe.
type InboundWebhookRepository interface {
	// Claim inserts w unless a claimed row (applied, stale, mismatch or
	// unmapped) already exists for its connection, event key and provider
	// status. It reports false, writing nothing, when one does.
	Claim(ctx context.Context, w *InboundWebhook) (bool, error)
	// Create inserts w unconditionally.
	Create(ctx context.Context, w *InboundWebhook) error
	// UpdateOutcome writes the result, result detail, payment and event of w.
	UpdateOutcome(ctx context.Context, w *InboundWebhook) error
	// List returns callbacks newest first (received_at, then public id),
	// without their body.
	List(ctx context.Context, f InboundFilter, p Page) (PageResult[InboundWebhook], error)
	GetByPublicID(ctx context.Context, publicID string) (*InboundWebhook, error)
}

// TxManager runs fn in a database transaction. Repository calls made with the
// ctx passed to fn join that transaction; fn returning an error rolls it back.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}
