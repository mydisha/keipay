package domain

import (
	"context"
	"time"
)

// OverdueCheckout is a pending payment whose checkout has expired without a
// callback that moved it on.
type OverdueCheckout struct {
	PaymentID         int64
	PaymentPublicID   string
	ConnectionID      int64
	ProviderPaymentID string
	Reference         string
	ExpiresAt         time.Time
}

// OverdueCheckoutQuery selects the checkouts one check reads back.
type OverdueCheckoutQuery struct {
	// ExpiredBefore and NotBefore bound expires_at: ExpiredBefore leaves
	// the gateway time to send its own callback, NotBefore stops checking
	// checkouts that stay pending for good.
	ExpiredBefore, NotBefore time.Time
	// CheckedBefore skips payments checked more recently than this.
	CheckedBefore time.Time
	Limit         int
}

// CheckoutCheckRepository finds the checkouts to read back from their
// gateway and records each read.
type CheckoutCheckRepository interface {
	// ListOverdue returns up to q.Limit PENDING payments that have a
	// connection and a provider payment id, with NotBefore < expires_at <=
	// ExpiredBefore, never checked or last checked at or before
	// CheckedBefore, earliest expiry first.
	ListOverdue(ctx context.Context, q OverdueCheckoutQuery) ([]OverdueCheckout, error)
	// MarkChecked records that the payment's checkout was read at at.
	MarkChecked(ctx context.Context, paymentID int64, at time.Time) error
}
