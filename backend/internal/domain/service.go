package domain

import "context"

// LedgerServicer defines ledger business operations.
type LedgerServicer interface {
	// List returns ledger entries matching the filter.
	List(ctx context.Context, f LedgerFilter) ([]LedgerEntry, error)
	// Summary returns per-app ledger summaries.
	Summary(ctx context.Context, f LedgerFilter) ([]LedgerSummary, error)
}
