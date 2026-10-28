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

// LedgerFilter narrows ledger queries. AppPublicID is a canonical app_ id.
type LedgerFilter struct {
	AppPublicID *string
	From        *time.Time
	To          *time.Time
	Limit       int
	Offset      int
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

// LedgerRepository persists ledger entries. List and Summary rows carry the
// app summary and the payment's public id.
type LedgerRepository interface {
	Create(ctx context.Context, e *LedgerEntry) error
	// MarkSettled sets settled_at on a payment's PAYMENT entry if not already set.
	MarkSettled(ctx context.Context, paymentID int64, settledAt time.Time) error
	List(ctx context.Context, f LedgerFilter) ([]LedgerEntry, error)
	Summary(ctx context.Context, f LedgerFilter) ([]LedgerSummary, error)
}

// WebhookRepository persists inbound events and outbound deliveries.
type WebhookRepository interface {
	// CreateEventIfNew inserts an idempotency record; returns false if it already existed.
	CreateEventIfNew(ctx context.Context, xenditID, status string) (bool, error)
	CreateDelivery(ctx context.Context, d *WebhookDelivery) error
	GetDelivery(ctx context.Context, id int64) (*WebhookDelivery, error)
	MarkDelivered(ctx context.Context, id int64, attempts int) error
	MarkFailed(ctx context.Context, id int64, attempts int, lastErr string) error
	// ListDeliveries returns deliveries newest first with their app summary
	// and payment public id.
	ListDeliveries(ctx context.Context, f DeliveryFilter) ([]WebhookDelivery, error)
	// ListUnsentDeliveries returns PENDING deliveries never attempted and created
	// before olderThan, oldest first, so they can be re-enqueued.
	ListUnsentDeliveries(ctx context.Context, olderThan time.Time, limit int) ([]WebhookDelivery, error)
}

// TxManager runs fn in a database transaction. Repository calls made with the
// ctx passed to fn join that transaction; fn returning an error rolls it back.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}
