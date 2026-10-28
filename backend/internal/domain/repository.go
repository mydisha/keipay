package domain

import (
	"context"
	"time"
)

// PaymentFilter narrows payment list queries.
type PaymentFilter struct {
	TenantID *int64
	Status   *PaymentStatus
	From     *time.Time
	To       *time.Time
	Limit    int
	Offset   int
}

// LedgerFilter narrows ledger queries.
type LedgerFilter struct {
	TenantID *int64
	From     *time.Time
	To       *time.Time
	Limit    int
	Offset   int
}

// TenantRepository persists tenants.
type TenantRepository interface {
	Create(ctx context.Context, t *Tenant) error
	Update(ctx context.Context, t *Tenant) error
	GetByID(ctx context.Context, id int64) (*Tenant, error)
	// GetByIDForUpdate is GetByID that also row-locks the tenant until the
	// surrounding transaction ends. Call it inside TxManager.WithinTx.
	GetByIDForUpdate(ctx context.Context, id int64) (*Tenant, error)
	GetByAPIKey(ctx context.Context, apiKey string) (*Tenant, error)
	GetBySlug(ctx context.Context, slug string) (*Tenant, error)
	List(ctx context.Context) ([]Tenant, error)
}

// PaymentRepository persists payments.
type PaymentRepository interface {
	Create(ctx context.Context, p *Payment) error
	UpdateStatus(ctx context.Context, id int64, status PaymentStatus, paidAt, settledAt *time.Time, raw []byte) error
	GetByID(ctx context.Context, id int64) (*Payment, error)
	GetByExternalID(ctx context.Context, externalID string) (*Payment, error)
	// GetByExternalIDForUpdate is GetByExternalID that also row-locks the payment
	// until the surrounding transaction ends. Call it inside TxManager.WithinTx.
	GetByExternalIDForUpdate(ctx context.Context, externalID string) (*Payment, error)
	List(ctx context.Context, f PaymentFilter) ([]Payment, int64, error)
}

// LedgerRepository persists ledger entries.
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
	ListDeliveries(ctx context.Context, tenantID *int64, status *WebhookDeliveryStatus, limit, offset int) ([]WebhookDelivery, error)
	// ListUnsentDeliveries returns PENDING deliveries never attempted and created
	// before olderThan, oldest first, so they can be re-enqueued.
	ListUnsentDeliveries(ctx context.Context, olderThan time.Time, limit int) ([]WebhookDelivery, error)
}

// TxManager runs fn in a database transaction. Repository calls made with the
// ctx passed to fn join that transaction; fn returning an error rolls it back.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}
