package doku

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
)

// statusPath is the status API path of an order.
func statusPath(invoice string) string { return "/orders/v1/status/" + url.PathEscape(invoice) }

// GetCheckout reads an order with GET /orders/v1/status/{invoice_number}.
// The answer carries the fields of a notification, so it is read the same
// way and gives the event its notification would have.
func (a *Adapter) GetCheckout(ctx context.Context, acc provider.Account, invoice string) (*provider.InboundEvent, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	ans, err := a.call(ctx, acc, http.MethodGet, statusPath(invoice), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("doku order status: %w", transportError(ctx, false, err))
	}
	if ans.Status == http.StatusNotFound {
		return nil, nil, fmt.Errorf("doku order status %s: %w", invoice, provider.ErrNotFound)
	}
	if ans.Status < 200 || ans.Status > 299 {
		return nil, nil, fmt.Errorf("doku order status: %w", statusError(false, ans))
	}
	ev, err := a.ParseWebhook(ans.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("doku order status: %w",
			provider.NewStatusError(provider.ErrRetryable, ans.Status, "unreadable status: "+err.Error()))
	}
	ev.Source = domain.InboundSourcePoll
	return ev, ans.Body, nil
}
