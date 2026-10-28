package domain

import "context"

// LedgerServicer defines ledger business operations.
type LedgerServicer interface {
	// List returns ledger entries matching the filter.
	List(ctx context.Context, f LedgerFilter) ([]LedgerEntry, error)
	// Summary returns per-app ledger summaries.
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
