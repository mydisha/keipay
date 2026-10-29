package domain

import (
	"context"
	"time"
)

// ReconciliationResult is the outcome of comparing one reference.
type ReconciliationResult string

// Reconciliation results, in display order. When several apply to one
// reference, the first of missing_in_keipay, missing_in_gateway,
// amount_mismatch, status_mismatch, fee_mismatch wins; matched is the rest.
const (
	ResultMatched          ReconciliationResult = "matched"
	ResultMissingInKeipay  ReconciliationResult = "missing_in_keipay"
	ResultMissingInGateway ReconciliationResult = "missing_in_gateway"
	ResultAmountMismatch   ReconciliationResult = "amount_mismatch"
	ResultStatusMismatch   ReconciliationResult = "status_mismatch"
	ResultFeeMismatch      ReconciliationResult = "fee_mismatch"
)

// ReconciliationResults returns every result in display order.
func ReconciliationResults() []ReconciliationResult {
	return []ReconciliationResult{ResultMatched, ResultMissingInKeipay, ResultMissingInGateway,
		ResultAmountMismatch, ResultStatusMismatch, ResultFeeMismatch}
}

// Valid reports whether r is a known result.
func (r ReconciliationResult) Valid() bool {
	for _, v := range ReconciliationResults() {
		if r == v {
			return true
		}
	}
	return false
}

// ReconciliationRunStatus is where a run is in its lifecycle.
type ReconciliationRunStatus string

// Run statuses.
const (
	RunQueued    ReconciliationRunStatus = "queued"
	RunRunning   ReconciliationRunStatus = "running"
	RunSucceeded ReconciliationRunStatus = "succeeded"
	RunFailed    ReconciliationRunStatus = "failed"
)

// Valid reports whether s is a known run status.
func (s ReconciliationRunStatus) Valid() bool {
	switch s {
	case RunQueued, RunRunning, RunSucceeded, RunFailed:
		return true
	default:
		return false
	}
}

// Active reports whether the worker has the run, or is about to.
func (s ReconciliationRunStatus) Active() bool { return s == RunQueued || s == RunRunning }

// Error codes of a failed run, stored on the run and shown in its error.
const (
	RunErrorProviderRejected    = "provider_rejected"
	RunErrorProviderUnavailable = "provider_unavailable"
	RunErrorTooManyTransactions = "too_many_transactions"
	RunErrorNotQueued           = "not_queued"
	RunErrorInternal            = "internal_error"
)

// ReconciliationSummary counts the items of a finished run per result.
type ReconciliationSummary struct {
	Total   int                          `json:"total"`
	Results map[ReconciliationResult]int `json:"results"`
}

// NewReconciliationSummary counts items, listing every result even when 0.
func NewReconciliationSummary(items []ReconciliationItem) *ReconciliationSummary {
	s := &ReconciliationSummary{Total: len(items), Results: make(map[ReconciliationResult]int, 6)}
	for _, r := range ReconciliationResults() {
		s.Results[r] = 0
	}
	for i := range items {
		s.Results[items[i].Result]++
	}
	return s
}

// RunError is why a run failed. Message is generic; gateway text is only
// logged.
type RunError struct {
	Code    string
	Message string
}

// UserRef names the console user a record refers to.
type UserRef struct {
	PublicID string
	Name     string
}

// ReconciliationRun compares one connection's gateway records with KeiPay's
// over [From, To). Re-running reuses the run and bumps RunCount, which also
// fences out workers still busy with an earlier count.
type ReconciliationRun struct {
	ID           int64
	PublicID     string
	ConnectionID int64
	// Connection is filled by reads.
	Connection ConnectionSummary
	From       time.Time
	To         time.Time
	Status     ReconciliationRunStatus
	RunCount   int
	// Summary is set once the run succeeded.
	Summary         *ReconciliationSummary
	UnresolvedCount int
	Error           *RunError
	CreatedByID     *int64
	// CreatedBy is filled by reads; nil when the run has no creator.
	CreatedBy  *UserRef
	CreatedAt  time.Time
	UpdatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// KeipaySide is KeiPay's record of a payment when an item was computed.
// App is not stored with it; reads join it from the payment.
type KeipaySide struct {
	PaymentPublicID string        `json:"payment_id"`
	App             AppSummary    `json:"-"`
	Status          PaymentStatus `json:"status"`
	Amount          int64         `json:"amount"`
	Currency        string        `json:"currency"`
	Fee             *int64        `json:"fee"`
	PaidAt          *time.Time    `json:"paid_at"`
}

// GatewaySide is the gateway's record of a reference: its transactions
// added up as the matcher does.
type GatewaySide struct {
	ProviderPaymentID string `json:"provider_payment_id"`
	ProviderStatus    string `json:"provider_status"`
	// Status is "" when the gateway's status is one KeiPay does not know.
	Status           PaymentStatus `json:"status"`
	Amount           int64         `json:"amount"`
	Currency         string        `json:"currency"`
	Fee              *int64        `json:"fee"`
	OccurredAt       time.Time     `json:"occurred_at"`
	TransactionCount int           `json:"transaction_count"`
}

// Resolution is how an admin settled an item.
type Resolution struct {
	Note         string
	ResolvedByID *int64
	// ResolvedBy is filled by reads.
	ResolvedBy *UserRef
	ResolvedAt time.Time
}

// ReconciliationItem is the result for one reference within a run.
type ReconciliationItem struct {
	ID          int64
	PublicID    string
	RunID       int64
	RunPublicID string
	Reference   string
	Result      ReconciliationResult
	PaymentID   *int64
	// Keipay is nil for missing_in_keipay, Gateway for missing_in_gateway.
	Keipay     *KeipaySide
	Gateway    *GatewaySide
	AnchorAt   time.Time
	Resolution *Resolution
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// GatewayTransaction is one transaction as a gateway reports it, normalized.
type GatewayTransaction struct {
	Reference         string
	ProviderPaymentID string
	// ProviderStatus is the gateway's own value.
	ProviderStatus string
	// Status is "" when ProviderStatus is unknown.
	Status   PaymentStatus
	Amount   int64
	Currency string
	// Fee is nil when unknown.
	Fee        *int64
	OccurredAt time.Time
}

// KeipayPayment is a payment as reconciliation compares it, with the fee
// from its report (nil without a report or when unknown).
type KeipayPayment struct {
	PaymentID       int64
	PaymentPublicID string
	App             AppSummary
	Reference       string
	Status          PaymentStatus
	Amount          int64
	Currency        string
	Fee             *int64
	PaidAt          *time.Time
}

// ReconciliationRunFilter narrows run lists; nil fields match everything.
type ReconciliationRunFilter struct {
	ConnectionPublicID *string
	Status             *ReconciliationRunStatus
	Environment        *Environment
}

// ReconciliationItemFilter narrows item lists; nil fields match everything.
// Resolved false matches every unresolved item, matched ones included.
type ReconciliationItemFilter struct {
	Result   *ReconciliationResult
	Resolved *bool
}

// ReconciliationRepository persists runs and their items. Every method joins
// the transaction carried by ctx. Run reads fill Connection and CreatedBy;
// item reads fill RunPublicID, Keipay.App and Resolution.ResolvedBy.
type ReconciliationRepository interface {
	CreateRun(ctx context.Context, r *ReconciliationRun) error
	GetRun(ctx context.Context, publicID string) (*ReconciliationRun, error)
	GetRunByID(ctx context.Context, id int64) (*ReconciliationRun, error)
	// GetRunForUpdate and GetRunByIDForUpdate also row-lock the run until the
	// transaction ends.
	GetRunForUpdate(ctx context.Context, publicID string) (*ReconciliationRun, error)
	GetRunByIDForUpdate(ctx context.Context, id int64) (*ReconciliationRun, error)
	// ListRuns returns runs newest first (created_at, then public id).
	ListRuns(ctx context.Context, f ReconciliationRunFilter, p Page) (PageResult[ReconciliationRun], error)
	// UpdateRun writes status, run count, summary, unresolved count, error,
	// started_at and finished_at of r and refreshes UpdatedAt.
	UpdateRun(ctx context.Context, r *ReconciliationRun) error
	// ClaimRun moves the run from queued to running, but only while its run
	// count is runCount. It reports false, writing nothing, otherwise.
	ClaimRun(ctx context.Context, id int64, runCount int) (bool, error)
	// TouchRun refreshes UpdatedAt of a running run, but only while its run
	// count is runCount, so a long run is not taken for a stalled one. It
	// reports false, writing nothing, when the run moved on.
	TouchRun(ctx context.Context, id int64, runCount int) (bool, error)
	// FailRun ends a queued or running run with runCount as failed. It
	// reports false, writing nothing, when the run moved on.
	FailRun(ctx context.Context, id int64, runCount int, e RunError) (bool, error)
	// SaveItems makes items the run's items: new references are inserted,
	// existing ones updated in place (keeping their resolution only when the
	// result is unchanged) and the run's other items deleted. It returns how
	// many items are unresolved differences afterwards.
	SaveItems(ctx context.Context, runID int64, items []ReconciliationItem) (int, error)
	// ListItems returns the run's items newest first (created_at, then
	// public id).
	ListItems(ctx context.Context, runID int64, f ReconciliationItemFilter, p Page) (PageResult[ReconciliationItem], error)
	// StreamItems calls fn with every matching item in ListItems' order, one
	// row at a time; an error from fn stops it and is returned.
	StreamItems(ctx context.Context, runID int64, f ReconciliationItemFilter, fn func(*ReconciliationItem) error) error
	GetItem(ctx context.Context, publicID string) (*ReconciliationItem, error)
	// GetItemForUpdate also row-locks the item until the transaction ends.
	GetItemForUpdate(ctx context.Context, publicID string) (*ReconciliationItem, error)
	// ResolveItem stores the resolution of it.
	ResolveItem(ctx context.Context, it *ReconciliationItem) error
	// KeipayPayments returns the payments a run of connectionID compares:
	// those with a report paid within [from, to), and those whose reference
	// is in refs, each once.
	KeipayPayments(ctx context.Context, connectionID int64, from, to time.Time, refs []string) ([]KeipayPayment, error)
}
