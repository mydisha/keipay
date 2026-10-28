package domain

import "time"

// LedgerEntryType classifies a ledger movement.
type LedgerEntryType string

const (
	LedgerEntryTypePayment LedgerEntryType = "PAYMENT"
	LedgerEntryTypeRefund  LedgerEntryType = "REFUND"
)

// LedgerEntry records per-tenant settled amounts for reconciliation against the
// single shared Xendit balance.
type LedgerEntry struct {
	ID        int64           `json:"id"`
	TenantID  int64           `json:"tenant_id"`
	PaymentID int64           `json:"payment_id"`
	Amount    int64           `json:"amount"`
	XenditFee int64           `json:"xendit_fee"`
	NetAmount int64           `json:"net_amount"`
	Type      LedgerEntryType `json:"type"`
	// SettledAt is when funds settled to the shared balance, when known.
	SettledAt *time.Time `json:"settled_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// LedgerSummary aggregates ledger totals for a tenant over a period.
type LedgerSummary struct {
	TenantID    int64 `json:"tenant_id"`
	Count       int64 `json:"count"`
	GrossAmount int64 `json:"gross_amount"`
	TotalFee    int64 `json:"total_fee"`
	NetAmount   int64 `json:"net_amount"`
}
