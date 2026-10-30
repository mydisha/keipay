package memstore

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// CheckoutChecks is an in-memory domain.CheckoutCheckRepository over a
// Payments store. It keeps status_checked_at itself.
type CheckoutChecks struct {
	payments *Payments
	mu       sync.Mutex
	checked  map[int64]time.Time
}

// NewCheckoutChecks returns a CheckoutChecks over payments.
func NewCheckoutChecks(payments *Payments) *CheckoutChecks {
	return &CheckoutChecks{payments: payments, checked: map[int64]time.Time{}}
}

// ListOverdue applies the query's rules to every stored payment.
func (c *CheckoutChecks) ListOverdue(_ context.Context, q domain.OverdueCheckoutQuery) ([]domain.OverdueCheckout, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.payments.mu.Lock()
	defer c.payments.mu.Unlock()
	var out []domain.OverdueCheckout
	for _, p := range c.payments.byID {
		if p.Status != domain.PaymentStatusPending || p.ExpiresAt == nil || p.ConnectionID == nil || p.XenditInvoiceID == "" ||
			p.ExpiresAt.After(q.ExpiredBefore) || !p.ExpiresAt.After(q.NotBefore) {
			continue
		}
		if at, ok := c.checked[p.ID]; ok && at.After(q.CheckedBefore) {
			continue
		}
		out = append(out, domain.OverdueCheckout{PaymentID: p.ID, PaymentPublicID: p.PublicID, ConnectionID: *p.ConnectionID,
			ProviderPaymentID: p.XenditInvoiceID, Reference: p.ExternalID, ExpiresAt: *p.ExpiresAt})
	}
	slices.SortFunc(out, func(a, b domain.OverdueCheckout) int {
		if d := a.ExpiresAt.Compare(b.ExpiresAt); d != 0 {
			return d
		}
		return cmp.Compare(a.PaymentID, b.PaymentID)
	})
	if len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

// MarkChecked records at for the payment.
func (c *CheckoutChecks) MarkChecked(_ context.Context, paymentID int64, at time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checked[paymentID] = at
	return nil
}

// CheckedAt returns when the payment was last checked.
func (c *CheckoutChecks) CheckedAt(paymentID int64) (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	at, ok := c.checked[paymentID]
	return at, ok
}
