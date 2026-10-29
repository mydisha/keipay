package xendit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
)

// defaultBaseURL is Xendit's API server.
const defaultBaseURL = "https://api.xendit.co"

// maxTransactionsLimit is the largest page the Transactions API serves.
const maxTransactionsLimit = 50

// listTimeout bounds one page request.
const listTimeout = 30 * time.Second

// maxErrorBody is how much of an error answer is kept for the logs.
const maxErrorBody = 1024

// transactionsResponse is one page of GET /transactions. The SDK's model
// decodes amounts as float32, which cannot hold every rupiah amount above
// about 16.7 million, so amounts are read as json.Number here.
type transactionsResponse struct {
	HasMore bool          `json:"has_more"`
	Data    []transaction `json:"data"`
}

type transaction struct {
	ID               string          `json:"id"`
	ReferenceID      string          `json:"reference_id"`
	Status           string          `json:"status"`
	SettlementStatus *string         `json:"settlement_status"`
	Currency         string          `json:"currency"`
	Amount           json.Number     `json:"amount"`
	Fee              *transactionFee `json:"fee"`
	Created          time.Time       `json:"created"`
}

type transactionFee struct {
	XenditFee     json.Number `json:"xendit_fee"`
	ValueAddedTax json.Number `json:"value_added_tax"`
}

// ListTransactions reads one page of the account's payments from the
// Transactions API, newest first.
func (a *Adapter) ListTransactions(ctx context.Context, acc provider.Account, q provider.TransactionQuery) (*provider.TransactionPage, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.transactionsURL(q), nil)
	if err != nil {
		return nil, fmt.Errorf("xendit list transactions: %w", err)
	}
	req.SetBasicAuth(acc.Credentials[keySecretKey], "")
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{Transport: a.transport}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("xendit list transactions: %w: %v", provider.ErrRetryable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, fmt.Errorf("xendit list transactions: %w", statusError(resp.StatusCode, string(body)))
	}

	var page transactionsResponse
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err := dec.Decode(&page); err != nil {
		return nil, fmt.Errorf("xendit list transactions: %w",
			provider.NewStatusError(provider.ErrRetryable, resp.StatusCode, "unreadable response: "+err.Error()))
	}
	return transactionPage(page, q)
}

func (a *Adapter) transactionsURL(q provider.TransactionQuery) string {
	limit := q.Limit
	if limit < 1 || limit > maxTransactionsLimit {
		limit = maxTransactionsLimit
	}
	v := url.Values{}
	v.Set("types", "PAYMENT")
	v.Set("created[gte]", q.From.UTC().Format(time.RFC3339))
	v.Set("created[lte]", q.To.UTC().Format(time.RFC3339))
	v.Set("limit", strconv.Itoa(limit))
	if q.Cursor != "" {
		v.Set("after_id", q.Cursor)
	}
	base := a.baseURL
	if base == "" {
		base = defaultBaseURL
	}
	return base + "/transactions?" + v.Encode()
}

// statusError classifies a non-2xx answer of a read: nothing changed at
// Xendit, so everything but a refusal may be retried.
func statusError(status int, body string) error {
	switch {
	case status == http.StatusTooManyRequests, status >= 500:
		return provider.NewStatusError(provider.ErrRetryable, status, body)
	case status >= 400:
		return provider.NewStatusError(provider.ErrRejected, status, body)
	default:
		return provider.NewStatusError(provider.ErrRetryable, status, "unexpected status")
	}
}

// transactionPage normalizes a page. created[lte] is inclusive, so a row at
// exactly q.To is dropped to keep the range half-open.
func transactionPage(page transactionsResponse, q provider.TransactionQuery) (*provider.TransactionPage, error) {
	out := &provider.TransactionPage{Transactions: make([]domain.GatewayTransaction, 0, len(page.Data))}
	for _, t := range page.Data {
		tx, err := normalizeTransaction(t)
		if err != nil {
			return nil, fmt.Errorf("xendit list transactions: %w",
				provider.NewStatusError(provider.ErrRetryable, http.StatusOK, "unreadable transaction "+t.ID+": "+err.Error()))
		}
		if tx.OccurredAt.Before(q.From) || !tx.OccurredAt.Before(q.To) {
			continue
		}
		out.Transactions = append(out.Transactions, tx)
	}
	if page.HasMore && len(page.Data) > 0 {
		out.NextCursor = page.Data[len(page.Data)-1].ID
	}
	return out, nil
}

func normalizeTransaction(t transaction) (domain.GatewayTransaction, error) {
	amount, err := domain.MinorUnits(t.Amount.String(), t.Currency)
	if err != nil {
		return domain.GatewayTransaction{}, err
	}
	tx := domain.GatewayTransaction{
		Reference:         t.ReferenceID,
		ProviderPaymentID: t.ID,
		ProviderStatus:    t.Status,
		Status:            transactionStatus(t.Status, t.SettlementStatus),
		Amount:            amount,
		Currency:          t.Currency,
		OccurredAt:        t.Created.UTC(),
	}
	if t.Fee != nil {
		fee, err := feeMinorUnits(*t.Fee, t.Currency)
		if err != nil {
			return domain.GatewayTransaction{}, err
		}
		tx.Fee = &fee
	}
	return tx, nil
}

// feeMinorUnits is Xendit's fee plus its VAT, which KeiPay compares with
// the invoice callback's gross minus adjusted_received_amount.
func feeMinorUnits(f transactionFee, currency string) (int64, error) {
	var total int64
	for _, part := range []json.Number{f.XenditFee, f.ValueAddedTax} {
		if part == "" {
			continue
		}
		v, err := domain.MinorUnits(part.String(), currency)
		if err != nil {
			return 0, err
		}
		total += v
	}
	return total, nil
}

// transactionStatus normalizes a transaction status; one KeiPay does not
// know is "".
func transactionStatus(status string, settlement *string) domain.PaymentStatus {
	switch status {
	case "SUCCESS":
		if settlement != nil && *settlement == "SETTLED" {
			return domain.PaymentStatusSettled
		}
		return domain.PaymentStatusPaid
	case "PENDING":
		return domain.PaymentStatusPending
	case "FAILED", "VOIDED", "REVERSED":
		return domain.PaymentStatusFailed
	default:
		return ""
	}
}

var _ provider.TransactionLister = (*Adapter)(nil)
