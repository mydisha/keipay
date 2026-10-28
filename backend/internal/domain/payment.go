package domain

import (
	"encoding/json"
	"time"
)

// PaymentStatus is the normalized lifecycle state of a payment.
type PaymentStatus string

const (
	PaymentStatusPending PaymentStatus = "PENDING"
	PaymentStatusPaid    PaymentStatus = "PAID"
	PaymentStatusSettled PaymentStatus = "SETTLED"
	PaymentStatusExpired PaymentStatus = "EXPIRED"
	PaymentStatusFailed  PaymentStatus = "FAILED"
)

// IsTerminal reports whether the status will not change further.
func (s PaymentStatus) IsTerminal() bool {
	switch s {
	case PaymentStatusSettled, PaymentStatusExpired, PaymentStatusFailed:
		return true
	default:
		return false
	}
}

// IsSuccessful reports whether funds have been captured (paid or settled).
func (s PaymentStatus) IsSuccessful() bool {
	return s == PaymentStatusPaid || s == PaymentStatusSettled
}

// CanTransitionTo reports whether a callback moving the payment from s to next
// should be applied. Callbacks can arrive out of order, so status only moves
// forward (PENDING → PAID → SETTLED). A late success after EXPIRED or FAILED is
// still applied, because the funds were captured.
func (s PaymentStatus) CanTransitionTo(next PaymentStatus) bool {
	if next == s {
		return false
	}
	switch s {
	case PaymentStatusPending:
		return true
	case PaymentStatusPaid:
		return next == PaymentStatusSettled
	case PaymentStatusExpired, PaymentStatusFailed:
		return next.IsSuccessful()
	default:
		return false
	}
}

// OrderItem is a single line in an order (product, package, etc.). It is sent to
// Xendit so the hosted invoice can show a breakdown, and stored for our records.
type OrderItem struct {
	Name        string `json:"name"`
	Quantity    int32  `json:"quantity"`
	Price       int64  `json:"price"`
	Category    string `json:"category,omitempty"`
	ReferenceID string `json:"reference_id,omitempty"`
	URL         string `json:"url,omitempty"`
}

// Customer carries optional payer details forwarded to Xendit's invoice.
type Customer struct {
	GivenNames   string `json:"given_names,omitempty"`
	Surname      string `json:"surname,omitempty"`
	Email        string `json:"email,omitempty"`
	MobileNumber string `json:"mobile_number,omitempty"`
}

// Payment is an invoice created on the shared Xendit account on behalf of a tenant.
type Payment struct {
	ID              int64         `json:"id"`
	TenantID        int64         `json:"tenant_id"`
	OrderID         string        `json:"order_id"`
	ExternalID      string        `json:"external_id"`
	XenditInvoiceID string        `json:"xendit_invoice_id"`
	Amount          int64         `json:"amount"`
	Currency        string        `json:"currency"`
	Status          PaymentStatus `json:"status"`
	Description     string        `json:"description"`
	InvoiceURL      string        `json:"invoice_url"`
	PayerEmail      string        `json:"payer_email"`
	Items           []OrderItem   `json:"items,omitempty"`
	ExpiresAt       *time.Time    `json:"expires_at,omitempty"`
	PaidAt          *time.Time    `json:"paid_at,omitempty"`
	// SettledAt is the actual time funds settled to the shared balance, sourced
	// from Xendit's SETTLED callback. Nil until settlement is confirmed.
	SettledAt *time.Time `json:"settled_at,omitempty"`
	// ExpectedSettlementAt is the estimated settlement date the tenant can show
	// to the user before funds actually settle. Optional, set at creation time.
	ExpectedSettlementAt *time.Time      `json:"expected_settlement_at,omitempty"`
	Raw                  json.RawMessage `json:"-"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}
