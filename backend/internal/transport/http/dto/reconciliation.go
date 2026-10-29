package dto

import (
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// UserRef is a console user a record refers to.
type UserRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func userRefFromDomain(u *domain.UserRef) *UserRef {
	if u == nil {
		return nil
	}
	return &UserRef{ID: u.PublicID, Name: u.Name}
}

// ReconciliationResultCounts counts items per result; every result is
// present.
type ReconciliationResultCounts struct {
	Matched          int `json:"matched"`
	MissingInKeipay  int `json:"missing_in_keipay"`
	MissingInGateway int `json:"missing_in_gateway"`
	AmountMismatch   int `json:"amount_mismatch"`
	StatusMismatch   int `json:"status_mismatch"`
	FeeMismatch      int `json:"fee_mismatch"`
}

// ReconciliationRunSummary counts the items of a finished run.
type ReconciliationRunSummary struct {
	Total      int                        `json:"total"`
	Unresolved int                        `json:"unresolved"`
	Results    ReconciliationResultCounts `json:"results"`
}

// ReconciliationRunError is why a run failed.
type ReconciliationRunError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ReconciliationRun is a run as the admin API shows it.
type ReconciliationRun struct {
	ID         string                         `json:"id"`
	Connection ConnectionSummary              `json:"connection"`
	From       time.Time                      `json:"from"`
	To         time.Time                      `json:"to"`
	Status     domain.ReconciliationRunStatus `json:"status"`
	RunCount   int                            `json:"run_count"`
	Summary    *ReconciliationRunSummary      `json:"summary"`
	Error      *ReconciliationRunError        `json:"error"`
	CreatedBy  *UserRef                       `json:"created_by"`
	CreatedAt  time.Time                      `json:"created_at"`
	StartedAt  *time.Time                     `json:"started_at"`
	FinishedAt *time.Time                     `json:"finished_at"`
}

// ReconciliationRunFromDomain converts a run; summary is shown only once the
// run succeeded and error only when it failed.
func ReconciliationRunFromDomain(r *domain.ReconciliationRun) ReconciliationRun {
	out := ReconciliationRun{
		ID: r.PublicID, Connection: *ConnectionSummaryFromDomain(&r.Connection), From: r.From.UTC(), To: r.To.UTC(),
		Status: r.Status, RunCount: r.RunCount, CreatedBy: userRefFromDomain(r.CreatedBy), CreatedAt: r.CreatedAt.UTC(),
		StartedAt: utcPtr(r.StartedAt), FinishedAt: utcPtr(r.FinishedAt),
	}
	if r.Status == domain.RunSucceeded && r.Summary != nil {
		res := r.Summary.Results
		out.Summary = &ReconciliationRunSummary{Total: r.Summary.Total, Unresolved: r.UnresolvedCount,
			Results: ReconciliationResultCounts{
				Matched: res[domain.ResultMatched], MissingInKeipay: res[domain.ResultMissingInKeipay],
				MissingInGateway: res[domain.ResultMissingInGateway], AmountMismatch: res[domain.ResultAmountMismatch],
				StatusMismatch: res[domain.ResultStatusMismatch], FeeMismatch: res[domain.ResultFeeMismatch],
			}}
	}
	if r.Status == domain.RunFailed && r.Error != nil {
		out.Error = &ReconciliationRunError{Code: r.Error.Code, Message: r.Error.Message}
	}
	return out
}

// ReconciliationRunResponse wraps one run.
type ReconciliationRunResponse struct {
	Run ReconciliationRun `json:"run"`
}

// ReconciliationRunListResponse is the response for
// GET /admin/reconciliation-runs.
type ReconciliationRunListResponse struct {
	Data       []ReconciliationRun `json:"data"`
	NextCursor *string             `json:"next_cursor"`
}

// ReconciliationRunListFromDomain converts a page of runs.
func ReconciliationRunListFromDomain(page domain.PageResult[domain.ReconciliationRun]) ReconciliationRunListResponse {
	out := ReconciliationRunListResponse{Data: make([]ReconciliationRun, 0, len(page.Items)), NextCursor: nextCursor(page.NextCursor)}
	for i := range page.Items {
		out.Data = append(out.Data, ReconciliationRunFromDomain(&page.Items[i]))
	}
	return out
}

// CreateReconciliationRunRequest is the body of POST
// /admin/reconciliation-runs. Times stay strings so the service can report
// rule datetime.
type CreateReconciliationRunRequest struct {
	ConnectionID string `json:"connection_id"`
	From         string `json:"from"`
	To           string `json:"to"`
}

// ResolveReconciliationItemRequest is the body of POST
// /admin/reconciliation-items/{id}/resolve.
type ResolveReconciliationItemRequest struct {
	Note string `json:"note"`
}

// ReconciliationKeipaySide is KeiPay's record of the payment.
type ReconciliationKeipaySide struct {
	PaymentID string               `json:"payment_id"`
	App       AppSummary           `json:"app"`
	Status    domain.PaymentStatus `json:"status"`
	Amount    int64                `json:"amount"`
	Currency  string               `json:"currency"`
	Fee       *int64               `json:"fee"`
	PaidAt    *time.Time           `json:"paid_at"`
}

// ReconciliationGatewaySide is the gateway's record of the reference.
// Status is null when the gateway's status is one KeiPay does not know.
type ReconciliationGatewaySide struct {
	ProviderPaymentID string                `json:"provider_payment_id"`
	ProviderStatus    string                `json:"provider_status"`
	Status            *domain.PaymentStatus `json:"status"`
	Amount            int64                 `json:"amount"`
	Currency          string                `json:"currency"`
	Fee               *int64                `json:"fee"`
	OccurredAt        time.Time             `json:"occurred_at"`
	TransactionCount  int                   `json:"transaction_count"`
}

// ReconciliationResolution is how an item was resolved.
type ReconciliationResolution struct {
	Note       string    `json:"note"`
	ResolvedBy *UserRef  `json:"resolved_by"`
	ResolvedAt time.Time `json:"resolved_at"`
}

// ReconciliationItem is an item as the admin API shows it.
type ReconciliationItem struct {
	ID         string                      `json:"id"`
	RunID      string                      `json:"run_id"`
	Reference  string                      `json:"reference"`
	Result     domain.ReconciliationResult `json:"result"`
	Keipay     *ReconciliationKeipaySide   `json:"keipay"`
	Gateway    *ReconciliationGatewaySide  `json:"gateway"`
	Resolution *ReconciliationResolution   `json:"resolution"`
	CreatedAt  time.Time                   `json:"created_at"`
	UpdatedAt  time.Time                   `json:"updated_at"`
}

// ReconciliationItemFromDomain converts an item; a missing side is null.
func ReconciliationItemFromDomain(it *domain.ReconciliationItem) ReconciliationItem {
	out := ReconciliationItem{ID: it.PublicID, RunID: it.RunPublicID, Reference: it.Reference, Result: it.Result,
		CreatedAt: it.CreatedAt.UTC(), UpdatedAt: it.UpdatedAt.UTC()}
	if k := it.Keipay; k != nil {
		out.Keipay = &ReconciliationKeipaySide{PaymentID: k.PaymentPublicID, App: AppSummaryFromDomain(k.App), Status: k.Status,
			Amount: k.Amount, Currency: k.Currency, Fee: k.Fee, PaidAt: utcPtr(k.PaidAt)}
	}
	if g := it.Gateway; g != nil {
		out.Gateway = &ReconciliationGatewaySide{ProviderPaymentID: g.ProviderPaymentID, ProviderStatus: g.ProviderStatus,
			Amount: g.Amount, Currency: g.Currency, Fee: g.Fee, OccurredAt: g.OccurredAt.UTC(), TransactionCount: g.TransactionCount}
		if g.Status != "" {
			status := g.Status
			out.Gateway.Status = &status
		}
	}
	if r := it.Resolution; r != nil {
		out.Resolution = &ReconciliationResolution{Note: r.Note, ResolvedBy: userRefFromDomain(r.ResolvedBy), ResolvedAt: r.ResolvedAt.UTC()}
	}
	return out
}

// ReconciliationItemResponse wraps one item.
type ReconciliationItemResponse struct {
	Item ReconciliationItem `json:"item"`
}

// ReconciliationItemListResponse is the response for
// GET /admin/reconciliation-runs/{id}/items.
type ReconciliationItemListResponse struct {
	Data       []ReconciliationItem `json:"data"`
	NextCursor *string              `json:"next_cursor"`
}

// ReconciliationItemListFromDomain converts a page of items.
func ReconciliationItemListFromDomain(page domain.PageResult[domain.ReconciliationItem]) ReconciliationItemListResponse {
	out := ReconciliationItemListResponse{Data: make([]ReconciliationItem, 0, len(page.Items)), NextCursor: nextCursor(page.NextCursor)}
	for i := range page.Items {
		out.Data = append(out.Data, ReconciliationItemFromDomain(&page.Items[i]))
	}
	return out
}
