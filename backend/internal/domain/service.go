package domain

import (
	"context"
	"time"
)

// ============================================================================
// Service Interfaces
// ============================================================================
//
// These interfaces define the contracts for business logic services.
// They are defined in the domain layer to:
// - Enable dependency inversion (handlers depend on interfaces, not implementations)
// - Facilitate mocking in tests
// - Allow alternative implementations (e.g., for different deployment scenarios)
//
// Implementation lives in internal/service/ package.

// PaymentServiceInput is the input for creating a payment.
type PaymentServiceInput struct {
	OrderID              string
	Amount               int64
	Currency             string
	Description          string
	PayerEmail           string
	InvoiceDuration      int
	Items                []OrderItem
	Customer             *Customer
	ExpectedSettlementAt *time.Time
}

// PaymentServicer defines payment business operations.
type PaymentServicer interface {
	// Create makes a Xendit invoice for the tenant and persists the payment.
	Create(ctx context.Context, tenant *Tenant, in PaymentServiceInput) (*Payment, error)
	// Get returns a payment scoped to a tenant.
	Get(ctx context.Context, tenantID, paymentID int64) (*Payment, error)
	// List returns payments matching the filter (admin use).
	List(ctx context.Context, f PaymentFilter) ([]Payment, int64, error)
}

// TenantServiceInput is the input for creating a tenant.
type TenantServiceInput struct {
	Name               string
	Slug               string
	WebhookURL         string
	RedirectSuccessURL string
	RedirectFailureURL string
}

// TenantUpdateInput holds mutable tenant fields.
type TenantUpdateInput struct {
	Name               string
	WebhookURL         string
	RedirectSuccessURL string
	RedirectFailureURL string
	Active             bool
}

// TenantCredentials is the one-time secret returned at create/rotate time.
type TenantCredentials struct {
	APIKey    string
	APISecret string
}

// TenantServicer defines tenant business operations.
type TenantServicer interface {
	// Create registers a tenant and returns it plus one-time plaintext credentials.
	Create(ctx context.Context, in TenantServiceInput) (*Tenant, *TenantCredentials, error)
	// Update persists mutable fields and invalidates cache.
	Update(ctx context.Context, id int64, in TenantUpdateInput) (*Tenant, error)
	// RotateCredentials issues a new API key + secret for a tenant.
	RotateCredentials(ctx context.Context, id int64) (*Tenant, *TenantCredentials, error)
	// Get returns a tenant by id.
	Get(ctx context.Context, id int64) (*Tenant, error)
	// List returns all tenants.
	List(ctx context.Context) ([]Tenant, error)
}

// LedgerServicer defines ledger business operations.
type LedgerServicer interface {
	// List returns ledger entries matching the filter.
	List(ctx context.Context, f LedgerFilter) ([]LedgerEntry, error)
	// Summary returns per-tenant ledger summaries.
	Summary(ctx context.Context, f LedgerFilter) ([]LedgerSummary, error)
}

// WebhookCallback is the subset of Xendit's invoice webhook we consume.
type WebhookCallback struct {
	ID             string
	ExternalID     string
	Status         string
	Amount         int64
	PaidAmount     int64
	FeesPaid       int64
	PaidAt         string
	SettlementDate string
	Updated        string
}

// WebhookServicer defines webhook processing business operations.
type WebhookServicer interface {
	// Process handles one inbound Xendit invoice callback.
	// It is idempotent: repeated (id,status) events are ignored.
	Process(ctx context.Context, cb WebhookCallback, raw []byte) error
}
