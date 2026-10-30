package xendit

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
)

// maxInvoiceBody bounds the invoice answer KeiPay reads and stores.
const maxInvoiceBody = 1 << 20

// GetCheckout reads an invoice with GET /v2/invoices/{id}. The invoice
// object carries the fields of the invoice callback, so it is read the same
// way and gives the event its callback would have. It goes over plain HTTP
// for the same reason as ListTransactions: the SDK decodes amounts as
// float32.
func (a *Adapter) GetCheckout(ctx context.Context, acc provider.Account, invoiceID string) (*provider.InboundEvent, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	base := a.baseURL
	if base == "" {
		base = defaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v2/invoices/"+url.PathEscape(invoiceID), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("xendit get invoice: %w", err)
	}
	req.SetBasicAuth(acc.Credentials[keySecretKey], "")
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{Transport: a.transport}).Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("xendit get invoice: %w: %v", provider.ErrRetryable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil, fmt.Errorf("xendit get invoice %s: %w", invoiceID, provider.ErrNotFound)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, nil, fmt.Errorf("xendit get invoice: %w", statusError(resp.StatusCode, string(body)))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxInvoiceBody))
	if err != nil {
		return nil, nil, fmt.Errorf("xendit get invoice: %w: %v", provider.ErrRetryable, err)
	}
	ev, err := a.ParseWebhook(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("xendit get invoice: %w",
			provider.NewStatusError(provider.ErrRetryable, resp.StatusCode, "unreadable invoice: "+err.Error()))
	}
	ev.Source = domain.InboundSourcePoll
	return ev, raw, nil
}
