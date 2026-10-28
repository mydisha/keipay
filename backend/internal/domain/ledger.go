package domain

import "time"

// LedgerEntryType classifies a ledger movement.
type LedgerEntryType string

const (
	LedgerEntryTypePayment LedgerEntryType = "PAYMENT"
	LedgerEntryTypeRefund  LedgerEntryType = "REFUND"
)

// LedgerEntry records per-app settled amounts for reconciliation against the
// single shared Xendit balance.
type LedgerEntry struct {
	ID        int64
	AppID     int64
	PaymentID int64
	// PaymentPublicID and App are filled by list queries only.
	PaymentPublicID string
	App             AppSummary
	Amount          int64
	XenditFee       int64
	NetAmount       int64
	Type            LedgerEntryType
	// SettledAt is when funds settled to the shared balance, when known.
	SettledAt *time.Time
	CreatedAt time.Time
}

// LedgerSummary aggregates ledger totals for an app over a period.
type LedgerSummary struct {
	App         AppSummary
	Count       int64
	GrossAmount int64
	TotalFee    int64
	NetAmount   int64
}
